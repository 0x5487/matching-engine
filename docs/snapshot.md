# Matching Engine Snapshot Design

本文件詳細說明撮合引擎的快照 (Snapshot) 與還原 (Restore) 機制設計。本設計採用非阻塞式全域快照策略，確保在高併發環境下資料的一致性與高效能。

## 1. 核心設計理念

### 1.1 非阻塞式快照 (Non-blocking Global Snapshot)
為了避免在快照期間暫停引擎運作 (Stop-the-World)，我們採用 **Chandy-Lamport 分散式快照演算法** 的變體，利用 Go Channels 的 FIFO (先進先出) 特性作為順序保證。

*   **Marker 機制**: Engine 在決定快照時，會向所有 OrderBook 的 `cmdChan` 發送一個特殊的 `CmdSnapshot` (Marker)。
*   **一致性保證**: 由於 Channel 的順序性，`CmdSnapshot` 之前的命令保證已被處理，之後的命令保證未被該快照包含。
*   **全域水位**: Engine 記錄發送 Marker 瞬間的 `GlobalLastCmdSeqID`，作為整份快照的資料版本號，用於恢復時的 MQ 重播起點。

### 1.2 儲存結構
快照資料將分為兩部分儲存：

1.  **Metadata (`metadata.json`) - 輕量全域資訊**: 
    *   只儲存 `GlobalLastCmdSeqID` (MQ 重播 Offset)、版本號、時間戳。
    *   包含 `snapshot.bin` 的全檔 Checksum。
    *   **保持乾淨，不包含具體市場列表**。

2.  **Binary Data (`snapshot.bin`) - 自描述二進位檔**:
    *   **Body**: 緊湊排列的各市場二進位資料。
    *   **Footer**: 包含市場索引 (`Markets` Index)，紀錄每個市場資料的 Offset 與 Length。
    *   格式: `[MarketData1][MarketData2]...[FooterJSON][FooterLength(4B)]`

## 2. 資料結構定義

### 2.1 Metadata結構 (JSON)

```go
// 存於 metadata.json
type SnapshotMetadata struct {
    SchemaVersion      int    `json:"schema_version"`        // 快照結構版本號，用於向後相容
    Timestamp          int64  `json:"timestamp"`              // 快照生成時間
    GlobalLastCmdSeqID uint64 `json:"global_last_cmd_seq_id"` // 全域 MQ 重播起點 (取各市場 LastCmdSeqID 的最大值)
    EngineVersion      string `json:"engine_version"`         // 引擎版本
    SnapshotChecksum   uint32 `json:"snapshot_checksum"`      // snapshot.bin 的全檔 CRC32
}

// 存於 snapshot.bin 的末尾 (Footer)
type SnapshotFileFooter struct {
    Markets []MarketSegment `json:"markets"` // 市場資料索引
}

type MarketSegment struct {
    MarketID string `json:"market_id"`
    Offset   int64  `json:"offset"`   // 在 snapshot.bin 中的絕對起始位置
    Length   int64  `json:"length"`   // 資料長度
    Checksum uint32 `json:"checksum"` // 該段資料的 CRC32
}
```

> **Note**: Footer 目前使用 JSON 格式序列化，對於大量市場 (1000+) 可能產生較大的解析開銷。未來可考慮改用 Protocol Buffers 或 FlatBuffers 優化。

### 2.2 OrderBook Snapshot (Binary)

```go
// Order 代表訂單簿中的訂單狀態，同時用於快照序列化
type Order struct {
    ID        string          // Order ID
    Side      Side            // Buy/Sell
    Price     decimal.Decimal
    Size      decimal.Decimal // 剩餘數量
    Type      OrderType       // Limit/Market/...
    UserID    uint64
    Timestamp int64           // Unix nano, 用於優先級重建
}

type OrderBookSnapshot struct {
    MarketID     string
    SeqID        uint64     // 該市場內部的 BookLog 序號
    LastCmdSeqID uint64     // 該市場最後處理的 Command SeqID (輔助驗證)
    TradeID      uint64     // 該市場當前的 Trade ID
    Bids         []*Order   // 按優先級排序的買單
    Asks         []*Order   // 按優先級排序的賣單
}
```

## 3. 流程詳解

### 3.1 TakeSnapshot (生成快照)

1.  **Engine 啟動**:
    *   Engine 鎖定當前 `lastDispatchedSeqID`，記為 `GlobalMarkerSeq`。
    *   建立當次快照的臨時目錄。

2.  **廣播 Marker**:
    *   Engine 透過 `sync.Map` 遍歷所有 OrderBook。
    *   向每個 OrderBook 的 `cmdChan` 發送 `CmdSnapshot` 命令。
    *   **注意**: 無需暫停 Engine 的 Command Dispatcher，新命令 (Seq > Marker) 會排在 Marker 之後。

3.  **OrderBook 執行**:
    *   OrderBook 消費到 `CmdSnapshot` 時，暫停處理後續命令。
    *   將當前記憶體狀態 (`bids`, `asks`, `seqID` 等) 序列化為 `OrderBookSnapshot` 物件。
    *   回傳 Snapshot 物件給 Engine (或寫入 Result Channel)。

4.  **彙整與寫入**:
    *   Engine 收集所有市場的 Snapshot。
    *   依序將每個 `OrderBookSnapshot` 寫入同一個 `snapshot.bin` 檔案。
    *   記錄每個市場寫入的 Offset, Length，並計算 CRC32。
    *   生成 `metadata.json`，寫入 `GlobalMarkerSeq` 和各市場的 Segment 資訊。

5.  **完成**:
    *   原子性地將臨時目錄移動/標記為完成 (如寫入 `DONE` 檔案)。

### 3.2 Restore (還原快照)

1.  **驗證 Metadata**:
    *   讀取 `metadata.json`。
    *   開啟 `snapshot.bin`。

2.  **重建 OrderBooks**:
    *   遍歷 Metadata 中的市場列表 (`Markets`)。
    *   根據 Offset/Length 從 `snapshot.bin` 讀取對應區段。
    *   校驗 CRC32。
    *   反序列化為 `OrderBookSnapshot`。
    *   為每個市場建立新的 `OrderBook` 實例，重建 `SkipList` 與 Atomic Counters。

3.  **註冊與啟動**:
    *   將重建好的 OrderBook 註冊回 `MatchingEngine`。
    *   啟動每個 OrderBook 的 `Start()` Goroutine。

4.  **狀態驗證**:
    *   比對各市場 Checksum 確保資料完整。
    *   驗證買賣簿總訂單數與預期一致。

5.  **MQ 重播**:
    *   Engine 告知 MQ Consumer 從 `GlobalLastCmdSeqID + 1` 開始消費。
    *   系統恢復服務。

## 4. 錯誤處理與邊界情況

*   **快照期間崩潰**: 由於寫入是先寫臨時目錄，若中途崩潰，重啟時會看到未完成的目錄 (無 `DONE` 標記)，直接捨棄並使用上一個成功快照。
*   **熱更新/擴容**: 透過 Snapshot 機制，可以輕鬆將 Engine 狀態複製到新機器，實現快速擴容或藍綠部署。

## 5. 未來擴展

*   **ZIP 打包壓縮**: 將 `metadata.json` 與 `snapshot.bin` 打包成單一 `.zip` 檔案，簡化 API 與檔案管理：
    ```
    snapshot_2024_12_27_120000.zip
    ├── metadata.json
    └── snapshot.bin
    ```
    *   **API 變更**: `TakeSnapshot(filePath)` / `RestoreFromSnapshot(filePath)` 傳入檔案路徑而非目錄
    *   **優點**: 單檔傳輸、備份更方便；壓縮節省儲存空間；標準格式可用命令行工具解壓
    *   **考量**: 壓縮增加少量 CPU 開銷；無法直接 seek 到特定市場資料（但目前全量讀取無影響）

*   **增量快照 (Incremental)**: 對於極大規模市場，可考慮只 Snapshot 變動部分 (與 WAL 結合)，但目前全量快照方案簡單可靠，優先實作。
