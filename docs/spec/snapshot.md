# Feature Specification: Snapshot & Restore (快照與還原)

**狀態**: 進行中 (In Progress)
**日期**: 2025-12-29  
**評審者**: Bob 架構師

---

## 1. 背景與專有名詞說明 (Background & Terminology)

### 什麼是快照 (Snapshot)?
快照是系統在特定時間點的完整狀態備份。對於基於內存（In-Memory）的撮合引擎而言，快照機制至關重要，它能讓系統在當機重啟後，快速恢復到最近一次的穩定狀態，再配合消息隊列（MQ）的重播（Replay）機制追趕至最新狀態。

### 為什麼需要這個功能?
1.  **快速復原 (Fast Recovery)**：避免從頭重播所有歷史交易紀錄（可能數億筆），將恢復時間從數小時縮短至數秒。
2.  **資料一致性 (Consistency)**：確保全域（多個市場）在同一邏輯時間點的狀態一致性，支援精確的斷點續傳。
3.  **非阻塞 (Non-blocking)**：在高頻交易場景下，製作快照不應暫停服務（Stop-the-World），以維持低延遲特性。

### 專有名詞
*   **Chandy-Lamport Algorithm**：一種分散式系統快照算法，利用 Marker 消息在 Channel 中流動來劃分快照邊界。
*   **Marker (標記)**：一個特殊的命令 (`CmdSnapshot`)，注入到命令流中。Marker 之前的命令屬於快照前，之後的屬於快照後。
*   **Global Sequence ID (全域序號)**：系統接收到的全域命令索引，用於標記快照版本水位（Watermark）。
*   **OrderBook Snapshot**: 單個市場的狀態快照，包含所有買賣單及內部計數器。

---

## 2. 目標 (Goals)
實作一個高效、非阻塞且一致的快照系統。

1.  **一致性**：保證快照包含且僅包含 `GlobalLastCmdSeqID` 之前的所有狀態變更。
2.  **非阻塞性**：利用 Go Channel 的 FIFO 特性，在不停止接收新訂單的情況下完成快照。
3.  **儲存效率**：採用緊湊的結構化二進位格式儲存，並分離 Metada 與 Payload。
4.  **可擴展性**：支持數千個市場的同時快照。

---

## 3. 技術方案 (Technical Solution)

### 3.1 核心機制 (Core Mechanism)

採用 **Marker 機制** 實現非阻塞全域快照：
1.  **發起**：Engine 決定快照，記錄當前 `lastDispatchedSeqID` 為 `GlobalMarkerSeq`。
2.  **廣播**：Engine 向所有 OrderBook 的輸入 Channel 發送 `CmdSnapshot` (Marker)。
3.  **流動**：Marker 在 Channel 中與普通訂單一同排隊，保證了時序性。
4.  **執行**：OrderBook 消費到 Marker 時，暫停處理後續消息，將當前內存狀態序列化，然後恢復處理。
5.  **彙整**：Engine 收集所有 OrderBook 的序列化數據，統一寫入文件。

### 3.2 檔案結構 (File Structure)

快照由兩部分組成，建議打包為 ZIP 或分開儲存：

#### A. Metadata (`metadata.json`) - 輕量全域資訊
```json
{
  "schema_version": 1,
  "timestamp": 1672531200000000000,
  "global_last_cmd_seq_id": 100500,
  "engine_version": "v1.0.0",
  "snapshot_checksum": 3294823,
  "markets": [
    { "market_id": "BTC-USDT", "offset": 0, "length": 4096, "checksum": 12345 },
    { "market_id": "ETH-USDT", "offset": 4096, "length": 2048, "checksum": 67890 }
  ]
}
```

#### B. Binary Data (`snapshot.bin`) - 自描述二進位檔
*   內容為所有市場的 `OrderBookSnapshot` 二進位數據緊密排列。
*   具體位置由 Metadata 中的 `offset` 和 `length` 索引。

### 3.3 資料模型 (Data Models)

**OrderBook Snapshot 物件 (`order_book.go`)**
```go
type OrderBookSnapshot struct {
    MarketID     string
    SeqID        uint64     // 該市場內部的 BookLog 序號
    LastCmdSeqID uint64     // 該市場最後處理的 Command SeqID
    TradeID      uint64     // 該市場當前的 Trade ID
    Bids         []*Order   // 買單列表 (按優先級)
    Asks         []*Order   // 賣單列表 (按優先級)
}
```

**Order 物件**
```go
type Order struct {
    ID        string
    Side      Side
    Price     decimal.Decimal
    Size      decimal.Decimal
    Type      OrderType
    UserID    int64
    Timestamp int64           // 用於重建優先級
    // 未來擴充: VisibleLimit, HiddenSize (for Iceberg)
}
```

### 3.4 流程詳解

#### 快照流程 (TakeSnapshot)
1.  Engine 鎖定水位 `GlobalMarkerSeq`。
2.  並發向所有 OrderBook 發送 `CmdSnapshot`。
3.  OrderBook 收到指令後，回傳自身的 `OrderBookSnapshot`。
4.  Engine 收到所有回傳後，依序寫入 `snapshot.bin`，計算 Checksum。
5.  生成並寫入 `metadata.json`。
6.  標記快照完成（如寫入 `DONE` 標記檔或原子改名）。

#### 還原流程 (Restore)
1.  讀取 `metadata.json` 獲取全域水位 `GlobalLastCmdSeqID`。
2.  根據 Metadata 索引，從 `snapshot.bin` 讀取各市場數據。
3.  並行重建每個市場的 `OrderBook` 實例（重建 SkipList, 恢復序列號）。
4.  啟動所有 OrderBook。
5.  通知 MQ Consumer 從 `GlobalLastCmdSeqID + 1` 開始重播後續消息。

---

## 4. 驗收測試 (Verification & Acceptance)

### 4.1 一致性驗證
*   **SeqID 連續性**：還原後，第一條重播消息的 SeqID 必須等於 `GlobalLastCmdSeqID + 1`。
*   **狀態比對**：
    1.  啟動 Engine，打入 1000 條訂單。
    2.  執行快照。
    3.  繼續打入 500 條訂單。
    4.  Kill Engine。
    5.  Restore Engine（載入快照 + 重播後 500 條）。
    6.  比對最終 OrderBook 狀態（Hash值），必須與完全運行無重啟的狀態一致。

### 4.2 效能驗證
*   **快照耗時**：測量 1000 個市場、百萬級訂單下的快照生成時間（目標 < 500ms，視 IO 而定）。
*   **阻塞影響**：測量快照期間，對正常訂單處理延遲的影響（應極小）。

---

## 5. 未來擴展 (Future Work)

*   **ZIP 打包**: 直接生成 `.zip` 檔，便於歸檔與傳輸。
*   **增量快照 (Incremental)**: 針對訂單量極大的市場，僅記錄變動部分（Delta），配合定期全量快照使用。
