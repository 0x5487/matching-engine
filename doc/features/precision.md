# Feature Specification: Market Precision Configuration (市場精度配置)

**狀態**: 規劃中 (Planning)  
**日期**: 2025-12-29  
**評審者**: 待定

---

## 1. 背景 (Background)

### 問題描述

目前 `handleMarketOrder` 使用 `matchSize.IsZero()` 判斷是否能繼續成交。當 Market Order 使用 `quoteSize` 時：

```go
matchSize = quoteSize / tOrd.Price
```

**問題場景：除不盡導致無限迴圈**

當 `quoteSize / price` 無法整除時，會產生極小的剩餘值，導致迴圈無法終止：

```
用戶請求: Market Buy，quoteSize = 1.00 USDT
訂單簿: [Sell-A: 0.001 BTC @ 33333 USDT]

第一輪迴圈:
  matchSize = 1.00 / 33333 = 0.00003000030000... BTC (無限循環小數)
  成交: 0.00003 BTC @ 33333 = 0.99999 USDT
  quoteSize = 1.00 - 0.99999 = 0.00001 USDT  ← 還有剩餘

第二輪迴圈:
  matchSize = 0.00001 / 33333 = 0.000000000300003... BTC (極小值)
  matchSize.IsZero() = false  ← 不是零，繼續迴圈！
  成交: 極小量...
  quoteSize = 0.00000000001 USDT

第三輪、第四輪... (無限循環)
  matchSize = 3e-16 BTC
  matchSize.IsZero() = false  ← 仍然不是零！
```

**根本原因**：`udecimal` 精度高達 19 位小數，`matchSize` 幾乎永遠不會剛好等於 0。

### 解決方案

在 `NewOrderBook` 時傳入 `LotSize`（最小成交單位），使用 `matchSize < LotSize` 判斷終止條件。

---

## 2. 設計決策

### 2.1 哪些配置需要保留在搓合引擎？

| 配置 | 搓合引擎需要？ | 說明 |
|------|---------------|------|
| Tick Size | ❌ | 價格驗證由上游處理 |
| **Lot Size** | ✅ | Market Order 終止判斷 |
| Min Notional | ❌ | 訂單金額驗證由上游處理 |
| Min Qty | ❌ | 訂單數量驗證由上游處理 |

**結論**：搓合引擎只需要 `LotSize`。

### 2.2 接口風格

採用 **Go Functional Options** 模式：

```go
book := NewOrderBook("BTC-USDT", publisher,
    WithLotSize(udecimal.MustParse("0.0001")),
)
```

---

## 3. 技術方案

### 3.1 新增 Option 類型 (`order_book.go`)

```go
// OrderBookOption configures an OrderBook.
type OrderBookOption func(*OrderBook)

// DefaultLotSize is the fallback minimum trade unit (1e-8).
// This prevents infinite loops when quoteSize/price produces very small values.
var DefaultLotSize = udecimal.MustFromInt64(1, 8)  // 0.00000001

// WithLotSize sets the minimum trade unit for the order book.
// When a Market order's calculated match size is less than this value,
// the order will be rejected with remaining funds returned.
// Default: 1e-8 (0.00000001) as a safety fallback.
func WithLotSize(size udecimal.Decimal) OrderBookOption {
    return func(book *OrderBook) {
        book.lotSize = size
    }
}
```

### 3.2 修改 OrderBook 結構

```go
type OrderBook struct {
    marketID         string
    lotSize          udecimal.Decimal  // [NEW] Minimum trade unit
    seqID            atomic.Uint64
    // ... 其他欄位 ...
}
```

### 3.3 修改 NewOrderBook

```go
// NewOrderBook creates a new order book instance.
func NewOrderBook(marketID string, publishTrader PublishLog, opts ...OrderBookOption) *OrderBook {
    book := &OrderBook{
        marketID:         marketID,
        lotSize:          DefaultLotSize,  // Default: 1e-8 (safety fallback)
        bidQueue:         NewBuyerQueue(),
        askQueue:         NewSellerQueue(),
        done:             make(chan struct{}),
        shutdownComplete: make(chan struct{}),
        publishTrader:    publishTrader,
    }
    
    for _, opt := range opts {
        opt(book)
    }
    
    book.cmdBuffer = NewRingBuffer(32768, book)
    return book
}
```

### 3.4 修改 handleMarketOrder 終止條件

```go
// 簡化邏輯：直接使用 LotSize 比較，不再需要 IsZero 判斷
if matchSize.LessThan(book.lotSize) {
    // 成交量小於最小單位，產生 Reject Log
    log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity, timestamp)
    log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
    log.Size = quoteSize  // 退還剩餘金額
    *logsPtr = append(*logsPtr, log)
    releaseOrder(order)
    break
}
```

---

## 4. 影響範圍

### 需要修改的文件

| 文件 | 變更 |
|------|------|
| `order_book.go` | 新增 `lotSize` 欄位、`OrderBookOption`、`WithLotSize()`、修改 `NewOrderBook()`、修改 `handleMarketOrder()` |
| `snapshot.go` | 新增 `LotSize` 到快照結構（可選） |
| `*_test.go` | 更新測試，大部分無需修改（使用預設值） |

### 不需要修改的文件

- `models.go`：不需要新增結構，Option 定義在 `order_book.go`
- `engine.go`：不需要修改

---

## 5. 驗證計畫

### 5.1 新增測試

```go
func TestMarketOrder_LotSize(t *testing.T) {
    // Setup: OrderBook with LotSize = 0.001
    config := WithLotSize(udecimal.MustParse("0.001"))
    book := NewOrderBook("BTC-USDT", publisher, config)
    
    // Place: Sell 0.1 BTC @ 50000
    // Action: Market buy with quoteSize = 0.04 USDT
    //         matchSize = 0.04 / 50000 = 0.0000008 < 0.001
    // Expected: Reject log with remaining quote size
}
```

### 5.2 向後兼容測試

確保不傳 opts 時行為不變：
```go
book := NewOrderBook("BTC-USDT", publisher)  // 無 opts，使用預設
```

---

## 6. 使用範例

```go
// 建立 BTC/USDT 訂單簿，最小成交單位 0.0001 BTC
btcBook := NewOrderBook("BTC-USDT", publisher,
    WithLotSize(udecimal.MustParse("0.0001")),
)

// 建立 ETH/USDT 訂單簿，最小成交單位 0.001 ETH
ethBook := NewOrderBook("ETH-USDT", publisher,
    WithLotSize(udecimal.MustParse("0.001")),
)

// 不指定時使用預設（向後兼容）
legacyBook := NewOrderBook("LEGACY", publisher)
```

