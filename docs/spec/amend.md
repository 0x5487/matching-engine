# Feature Specification: Amend Order (修改訂單)

**狀態**: 已實作 (Implemented)
**日期**: 2025-12-29  
**評審者**: Bob 架構師

---

## 1. 背景與專有名詞說明 (Background & Terminology)

### 什麼是修改訂單 (Amend Order)?
修改訂單（Amend Order / Replace Order）允許交易者在不取消原訂單的情況下，直接修改已掛在訂單簿（Order Book）上之委託單的參數，主要包括**價格 (Price)** 與 **數量 (Size)**。

### 為什麼需要這個功能?
1.  **減少操作延遲 (Latency Reduction)**：相比於「先取消再下新單 (Cancel-Replace)」的兩步操作，單一指令的 Amend 操作能減少網絡往返次數與撮合引擎的處理開銷。
2.  **維持排隊優勢 (Priority Retention)**：在某些特定場景下（如僅減少數量），修改訂單允許交易者保留其在價格優先/時間優先（Price-Time Priority）隊列中的位置，而不必重新排隊。

### 專有名詞
*   **Priority Loss (優先級丟失)**：訂單被移出隊列並重新插入，視為新訂單，排在同價格檔位的末尾。
*   **Priority Retention (優先級保留)**：訂單在隊列中的位置保持不變。
*   **In-Flight Check**：確保在修改指令到達時，原訂單尚未被完全成交或取消。

---

## 2. 目標 (Goals)
明確定義 SDK 中 `AmendOrder` 的行為邏輯，特別是針對優先級（Priority）的處理規則，確保行為符合大多數交易所的標準慣例。

1.  **支持參數**：支持修改 `Price` 和 `Size`。
2.  **優先級規則**：
    *   **減量 (Size Decrease)**：保留優先級。
    *   **增量 (Size Increase)**：失去優先級（重新排隊）。
    *   **改價 (Price Change)**：失去優先級（視為新單搓合）。
3.  **併發安全性**：確保在發送修改期間，若訂單已被成交，修改操作應優雅失敗或僅作用於剩餘部分。

---

## 3. 技術方案 (Technical Solution)

### 3.1 模型說明 (Model)

**`AmendOrderCommand` (`models.go`)**
```go
type AmendOrderCommand struct {
    SeqID    uint64           `json:"seq_id"`
    OrderID  string           `json:"order_id"`
    UserID   int64            `json:"user_id"`
    NewPrice udecimal.Decimal `json:"new_price"`
    NewSize  udecimal.Decimal `json:"new_size"`
    Timestamp int64           `json:"timestamp"`
}
```

### 3.2 核心邏輯流程 (`order_book.go`)

撮合引擎收到 `CmdAmendOrder` 後的處理邏輯 (`amendOrder` 函數)：

#### A. 前置驗證
1.  **查找訂單**：根據 `OrderID` 在 Bid 或 Ask 隊列中查找。
2.  **存在性檢查**：若訂單不存在（已完全成交或已取消）或 `UserID` 不匹配，回傳/記錄 `RejectReasonOrderNotFound`。

#### B. 邏輯分支

目前實作根據修改內容分為兩條路徑：

**路徑 1: 失去優先級 (Priority Loss / Re-matching)**
*   **觸發條件**：
    *   `NewPrice != OldPrice` (改價)
    *   **或者** `NewSize > OldSize` (增量)
*   **執行步驟**：
    1.  **移除原單**：從當前隊列中移除原訂單 (`removeOrder`)。
    2.  **更新屬性**：更新內存中 `Order` 對象的 `Price` 和 `Size`。
    3.  **記錄日誌**：發送 `LogTypeAmend`，記錄變更前後的狀態。
    4.  **重新撮合 (Re-match)**：
        *   將該訂單視為一個**全新**的 Limit Order，調用 `handleLimitOrder`。
        *   這意味著如果修改後的價格能與對手方成交，它會立即成交（Taker）。
        *   若無法成交，則**插入到新價格隊列的末尾**（Maker）。

**路徑 2: 保留優先級 (Priority Retention)**
*   **觸發條件**：
    *   `NewPrice == OldPrice` (同價)
    *   **且** `NewSize < OldSize` (減量)
*   **執行步驟**：
    1.  **原地更新**：直接修改隊列中訂單的 `Size` (`updateOrderSize`)。
    2.  **保持位置**：訂單對象的指針未變，前後指針未變，因此在 SkipList/LinkedList 中的位置不變。
    3.  **記錄日誌**：發送 `LogTypeAmend`。

### 3.3 邊界情況 (Edge Cases)

*   **無效參數**：若 `NewSize <= 0` 或 `NewPrice <= 0`，直接返回 `ErrInvalidParam`（實際上由 command 驗證攔截）。
*   **併發成交**：此設計基於單線程/Sequencer 架構。當處理 Amend Event 時，訂單狀態是確定的。若在 Amend 到達前，訂單已被 Match 了一部分：
    *   `OldSize` 指的是 Amend 執行當下剩餘的 Size。
    *   若剩餘 Size 已小於目標 `NewSize`（針對減量場景），邏輯上應以當前剩餘為準或拒絕。目前的代碼邏輯是直接覆蓋 `Size = NewSize`。如果 `NewSize` 比當前實際剩餘的還大，會變成增量邏輯（走路徑 1）。
    *   *注意*：若用戶原本想將 10 改成 5，但在途中成交了 6 剩 4。用戶指令 "Set Size to 5" 到達。此時 `NewSize(5) > OldSize(4)`，會觸發增量邏輯（路徑 1），變成新的 5 單位訂單排隊。這通常是可接受的，雖然用戶可能本意是 "減量"。

---

## 4. 驗收測試 (Verification & Acceptance)

### 4.1 優先級驗證
需在 `order_book_test.go` 中補充或確認以下場景：

1.  **減量不丟失位置**：
    *   Setup: Price 100 隊列 [A(10), B(10)]。
    *   Action: Amend A Size to 5.
    *   Expected: 隊列 [A'(5), B(10)]。
    *   Verify: 下一個賣單 Size 5 進來，應與 A' 成交，而非 B。

2.  **增量丟失位置**：
    *   Setup: Price 100 隊列 [A(10), B(10)]。
    *   Action: Amend A Size to 15.
    *   Expected: 隊列 [B(10), A'(15)]。
    *   Verify: 下一個賣單 Size 5 進來，應與 B 成交。

3.  **改價重新搓合**：
    *   Setup: Ask Book [105, 110]. Bid Order A @ 100.
    *   Action: Amend A Price to 106.
    *   Expected: A 應立即與 Ask(105) 成交。

### 4.2 狀態驗證
*   驗證 `LogTypeAmend` 的輸出字段是否正確反映了 `OldPrice`, `OldSize` 與 `NewPrice`, `NewSize`。
*   驗證修改不存在的訂單會正確產生 `RejectLog`。

---

## 5. 未來擴展 (Future Work)
*   **Cancel-Replace 原子性**：目前的 Amend 實作在失敗時（如 Validation）不會影響原單。但在路徑 1（Re-match）中，是先 Remove 再 Add。如果 Re-matching 過程中發生極端錯誤（理論上 `handleLimitOrder` 不會失敗，只會成交或 Rest），原單已不在簿子上。這符合 Cancel-Replace 語義。
*   **Iceberg 支持**：(參見 `iceberg.md`) 冰山訂單的修改邏輯較複雜，目前 Amend 對普通訂單的行為標準，但若涉及冰山，建議遵循冰山規格書的定義（通常涉及全單重排）。
