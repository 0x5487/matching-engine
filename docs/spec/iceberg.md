# Feature Specification: Iceberg Orders (冰山訂單)

**狀態**: 已確認 (Confirmed)
**日期**: 2025-12-29  
**評審者**: Bob 架構師 (已通過 2025-12-29)

---

## 1. 背景與專有名詞說明 (Background & Terminology)

### 什麼是冰山訂單 (Iceberg Orders)?
冰山訂單（Iceberg Order），又稱為隱藏單（Hidden Order），是一種允許交易者提交大額訂單，但只向市場公開展示其中一小部分數量的訂單類型。就像冰山一樣，水面上只能看到一小角（公開可見數量），而水面下隱藏著巨大的體積（剩餘隱藏數量）。

### 為什麼需要這個功能?
1.  **減少市場衝擊 (Market Impact)**：如果機構投資者直接在訂單簿（Order Book）上掛出巨額買單或賣單，會驚嚇市場，導致價格向不利方向移動（例如巨額買單會導致其他賣家提高價格）。
2.  **隱藏交易意圖 (Information Leakage)**：防止高頻交易者（HFT）或掠奪性算法偵測到大戶的存在而在其面前搶跑（Front-running）。
3.  **流動性提供**：鼓勵大戶提供流動性，因為他們不必擔心暴露全部持倉。

### 專有名詞
*   **Total Size (總數量)**：訂單原本計畫交易的總量。
*   **Visible Size / Display Qty (可見數量)**：在訂單簿上公開顯示的數量，也是每次撮合的上限。
*   **Hidden Size (隱藏數量)**：剩餘未公開的數量，保存在系統內部，外部不可見。
*   **Replenishment (補單/重載)**：當 Visible Size 被完全吃掉（Filled）後，系統自動從 Hidden Size 移動一部分數量變成新的 Visible Size 的過程。
*   **Sequencer (定序器)**：確保所有命令有全域唯一且遞增的 ID，這在我們的架構中對應 `SeqID`。

---

## 2. 目標 (Goals)
在現有的搓合引擎 SDK 中引入冰山訂單支持，具體目標如下：

1.  **SDK 接口擴充**：`PlaceOrder` 接口需支持傳入 `visible_size` 參數。
2.  **核心邏輯實作**：
    *   **主動單階段 (Aggressive)**：新下的冰山單視為普通單與現有訂單搓合。
    *   **被動單階段 (Passive)**：未成交部分進入訂單簿時，僅展示 `visible_size`。
    *   **自動補單**：當可見部分被成交後，自動從隱藏部分補單，並**重新排隊**（時間優先級降低）。
3.  **數據透明度**：
    *   對外行情（Market Data / Depth）只包含可見數量。
    *   成交回報（Trade Log）需正確反映實際成交。
4.  **確定性 (Determinism)**：確保在 Event Sourcing / Replay 架構下，補單的行為是確定性的，不依賴本機時間。
5.  **相容性**：不破壞現有 `models.go` 的快照與恢復機制。

---

## 3. 技術方案 (Technical Solution)

### 3.1 模型變更 (Model Changes)

**修改 `PlaceOrderCommand` (`models.go`)**
新增 `VisibleSize` 欄位。**強制要求**所有 Command 攜帶 `CreatedTime`，以確保確定性時間戳。

```go
type PlaceOrderCommand struct {
    // ... 原有欄位 ...
    Size        udecimal.Decimal `json:"size"`         // 總下單數量
    VisibleSize udecimal.Decimal `json:"visible_size"` // 最大可見數量 (0 代表普通訂單)
    
    // 必須由上游 Sequencer 或 Gateway 打上時間戳 (Unix Nano)
    // 用於訂單創建時間及 Rephelishment 時更新邏輯時間
    Timestamp   int64            `json:"timestamp"` 
}
```

**修改內部 `Command` 結構 (`order_book.go`)**
為了保持 Unified Command 架構的一致性，內部扁平化的 `Command` 結構也需同步更新。

```go
type Command struct {
    // ... existing fields ...
    Size        udecimal.Decimal
    QuoteSize   udecimal.Decimal
    UserID      uint64
    VisibleSize udecimal.Decimal  // [NEW] For Iceberg orders
    Timestamp   int64             // [NEW] Command timestamp
    // ...
}
```

**修改 `Order` 結構 (`models.go`)**
確保 JSON 序列化時使用 `omitempty` 以兼容舊版 SnapshotSchema。

```go
type Order struct {
    // ... 原有欄位 ...
    
    // 配置的每次最大顯示數量 (靜態配置，下單後不變)
    // 如果為 0，則視為普通限價單
    VisibleLimit udecimal.Decimal `json:"visible_limit,omitempty"` 
    
    // 當前剩餘的隱藏數量
    HiddenSize udecimal.Decimal `json:"hidden_size,omitempty"`
}
```

### 3.2 核心邏輯流程

#### A. 下單 (Aggressive Phase)
1.  **驗證**：若 `VisibleSize > 0`，必須小於總 `Size`。
2.  **搓合**：直接使用總 `Size` 與對手方進行搓合。
    *   *設計意圖*：在 Taker 階段，冰山單的「隱藏」特性無需生效，盡可能多地成交是首要目標。
3.  **殘餘處理 (Resting)**：若搓合後仍有剩餘，轉為被動單。
    *   **時間戳處理**：`Order.Timestamp` 使用 `PlaceOrderCommand.Timestamp`。**絕對禁止**使用 `time.Now()`。

#### B. 被動成交與補單 (Passive Execution & Replenishment)
當 Taker 吃掉 Iceberg 的可見部分 (`Order.Size == 0` && `Order.HiddenSize > 0`)：
1.  **檢查條件**：確認 Visible 部分確實已耗盡。若僅部分成交（Visible > 0），不觸發補單。
2.  **補單動作**：
    *   計算補單量 `ReloadQty = min(Order.HiddenSize, Order.OrderVisibleLimit)`.
    *   `Order.Size = ReloadQty`
    *   `Order.HiddenSize -= ReloadQty`
3.  **優先級重置 (Priority Reset)**：
    *   **邏輯操作**：直接將該 `Order` 從當前價格隊列的頭部移除，並**重新插入到同價格隊列的尾部**。
    *   **時間戳更新**：將 `Order.Timestamp` 更新為當前觸發補單的 Taker Command 的 `Timestamp`。這不僅反映了邏輯時間的更新，也便於日誌追蹤。

**關於 Depth (市場深度) 的影響**：
*   現有的 `queue.depth()` 邏輯不需大幅修改，因為它累加的是 `Order.Size`。
*   由於我們將 `Order.Size` 定義為「當前可見數量」，因此市場深度數據自然只會包含可見部分，隱藏部分不會被廣播，符合設計預期。

### 3.3 針對其他訂單類型的影響分析 (Production Ready)

#### Cancel Order (取消訂單)
*   **處理邏輯**：
    *   查找訂單。
    *   計算總退款量 = `Order.Size` (當前可見) + `Order.HiddenSize` (剩餘隱藏)。
    *   從訂單簿移除訂單。
    *   發送 `LogTypeCancel`，`Size` 欄位填寫總退款量。

#### Amend Order (修改訂單)
遵循 `doc/features/amend.md` 定義的規則：
1.  **改價**：Cancel + Place (優先級丟失)。
2.  **增量**：加到 Hidden，並移至隊尾 (優先級丟失)。
3.  **減量**：優先扣 Hidden，不移位 (優先級保留)。

---

## 4.驗收測試 (Verification & Acceptance)

### 4.1 確定性驗證
*   **Time Travel 測試**：構造一組包含 Taker 吃單導致多次 Replenish 的 Command 序列。驗證在不同的物理時間運行該測試，產生的 Trade Log 和最終 OrderBook 狀態完全一致。

### 4.2 冰山邏輯驗證
*   **Priority Loss (連續補單)**：
    *   Setup: Iceberg (Vis:10, Hid:100)。
    *   Action: 連續 5 個 Taker 各吃 10。
    *   Verify: 驗證每次補單後，該 Iceberg 訂單都排在當時價格隊列的最後。
*   **隱藏保護**：驗證 Market Data 只廣播 Visible Size。
*   **部分成交不補單**：
    *   Setup: Iceberg (Vis:10, Hid:50)。
    *   Action: Taker 吃 5。
    *   Verify: Visible 剩 5，不觸發補單，Hidden 仍為 50。

### 4.3 Snapshot/Restore 驗證
*   **相容性**：驗證舊版 Snapshot 能被成功 Restore (HiddenSize 默認為 0)。
*   **完整性**：驗證含 Iceberg 的 Snapshot Restore 後，HiddenSize 與 VisibleLimit 數值正確，且後續補單邏輯依然有效。

---

## 5. 手續費與結算 (Fees & Settlement)
*   **架構確認**：手續費計算邏輯位於下游結算服務 (Settlement Service)。
*   **撮合職責**：撮合引擎僅負責輸出標準化的 `MatchLog`。

---

## 6. Code Review (2025-12-29)

**評審者**: Bob 架構師  
**評審結論**: ✅ **通過 - 代碼品質合格，可合併**

### 6.1 審查範圍

| 文件 | 變更類型 | 評估 |
|------|----------|------|
| `models.go` | 新增欄位 | ✅ 正確 |
| `queue.go` | Snapshot 支持 | ✅ 正確 |
| `order_book.go` | 核心邏輯 | ✅ 正確 |
| `order_book_log.go` | Log 時間戳 | ✅ 正確 |
| `order_book_iceberg_test.go` | 新增測試 | ✅ 覆蓋充分 |

### 6.2 實作亮點 👍

1.  **確定性時間戳**：所有 Command 和 Log 現在都攜帶 `Timestamp`，完全移除對 `time.Now()` 的依賴。
2.  **`checkReplenish` 封裝**：補單邏輯被抽取為獨立函數，代碼簡潔。
3.  **`prepareIcebergForResting` 函數**：確保 Iceberg Taker 使用全量搓合，Size 拆分僅在進入訂單簿時發生。
4.  **Amend Iceberg 處理正確**：減量優先扣 Hidden、增量重新排隊。
5.  **Cancel 總量計算**：正確返回 `Size + HiddenSize`。
6.  **Snapshot 序列化**：正確包含 `VisibleLimit` 和 `HiddenSize`，使用 `omitempty` 確保向後兼容。

### 6.3 測試覆蓋

| 測試案例 | 覆蓋場景 |
|----------|----------|
| `TestIceberg_Placement` | 下單後 Depth 只顯示 Visible |
| `TestIceberg_Replenishment` | 補單觸發 + Open Log 產生 |
| `TestIceberg_ReplenishmentPriority` | 補單後優先級降低 |
| `TestIceberg_Amend` | 減量保留優先級、增量丟失優先級 |
| `TestIceberg_PartialFillNoReplenish` | ✅ 部分成交不觸發補單 |
| `TestIceberg_TakerAggressiveMatch` | ✅ Taker 使用全量搓合 |
| `TestIceberg_SnapshotRestore` | ✅ Snapshot/Restore 完整性 |

### 6.4 Bug 修復記錄 🐛

#### A. Iceberg Taker 全量搓合 (已修復)

**問題**：原實作在 `addOrder` 中提前將 Iceberg 的 Size 拆分為 VisibleSize，導致 Taker 階段僅用 VisibleSize 搓合，違反規格書 3.2.A。

**修復**：
- 新增 `prepareIcebergForResting()` 函數
- Size 拆分延遲到訂單進入訂單簿時才執行
- 測試 `TestIceberg_TakerAggressiveMatch` 驗證此行為

#### B. 移除 1e-8 精度限制 (已修復)

**問題**：`handleMarketOrder` 使用 `quoteSize.LessThan(1e-8)` 作為終止條件，但精度限制應由上游處理。

**修復**：改為 `quoteSize.IsZero()`。當 quoteSize 太小導致 matchSize 為 0 時，L703 的 `matchSize.IsZero()` 檢查已會安全終止循環。

#### C. `RejectReasonWouldCrossSpread` 更名

```diff
- RejectReasonWouldCrossSpread RejectReason = "would_cross_spread"
+ RejectReasonPostOnlyMatch    RejectReason = "post_only_match"
```

**確認**：下游服務不依賴此值，無需額外處理。

#### D. `matchSize.IsZero()` 缺少 Reject Log (已修復)

**問題**：當 Market Order 使用 `quoteSize` 時，若剩餘金額太小導致 `matchSize` 計算為 0，原代碼直接跳出循環而未產生 Reject Log。這導致 OMS 不知道要解凍剩餘資金。

**修復**：在 `matchSize.IsZero()` 分支中添加 Reject Log，確保 OMS 能正確處理資金解凍。

### 6.5 額外發現問題 (TODO)

以下問題在本次 Code Review 過程中發現，但不在 Iceberg 功能範圍內，記錄為未來 TODO：

#### TODO 1: 最小成交單位配置

**現狀**：目前使用 `matchSize.IsZero()` 判斷是否能繼續成交，但這依賴運算精度。

**建議**：
- 在 `NewOrderBook()` 時允許傳入 `minTradeUnit` 參數
- 不同市場（如 BTC/USDT vs ETH/USDT）可能有不同的最小成交單位
- 使用 `matchSize.LessThan(minTradeUnit)` 替代 `matchSize.IsZero()`

**代碼位置**：`order_book.go` L717 已添加 TODO 備註

#### TODO 2: Market Order + FOK 支持

**現狀**：目前 `OrderType` 設計為 `market`, `limit`, `ioc`, `fok` 等互斥類型，無法組合。Market Order 隱式為 IOC 語義。

**建議**：
- 新增 `TimeInForce` 欄位，允許用戶對 Market Order 指定 `IOC` 或 `FOK`
- 符合主流交易所（Binance, OKX, Bybit）的 API 設計

**方案 A**：新增欄位
```go
type PlaceOrderCommand struct {
    Type        OrderType   `json:"type"`          // market, limit
    TimeInForce TimeInForce `json:"time_in_force"` // ioc, fok, gtc (新增)
}
```

**方案 B**：新增組合類型
```go
const (
    MarketIOC OrderType = "market_ioc"
    MarketFOK OrderType = "market_fok"
)
```

### 6.6 結論

**審核通過 ✅ 所有問題已修復，7 個測試全部通過，可合併至主分支。**
