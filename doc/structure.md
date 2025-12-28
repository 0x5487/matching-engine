# 撮合引擎數據結構設計決策 (Design Decision: Price Level Data Structure)

**狀態**: 已確認 (Approved)  
**日期**: 2025-12-28  
**評審者**: 架構師 (Architect Review Incorporated)

---

## 1. 背景與需求 (Context)

本專案的核心目標是構建一個極高性能、低延遲的撮合引擎。針對訂單簿 (Order Book) 的 **價格層級 (Price Level)** 管理，我們面臨以下嚴苛的生產環境需求：

1.  **深度規模**: 生產環境的買賣檔位 (Depth) 經常超過 **1000 層**。
2.  **Zero Allocation (零動態分配)**: 在熱路徑上嚴禁使用 `new` 或 `make`，以避免 GC 延遲抖動。
3.  **高性能 (High Throughput)**: 目標吞吐量為百萬級 TPS。
4.  **1:1 Insert/Cancel 比例**: 實際生產中，機構用戶的下單與撤單比例接近 **1:1**，Delete 性能至關重要。

---

## 2. 方案選型分析 (Options Analysis)

| 方案 | Insert | Delete | 熱路徑分配 | 結論 |
|:---|:---|:---|:---|:---|
| **huando/skiplist** | 407 µs | 55 µs | 3000+ allocs | ❌ 分配過多 |
| **LLRB Tree** | 215 µs | 223 µs | 0 allocs | ❌ Delete 太慢 |
| **Pooled Skiplist** | 282 µs | **51 µs** | **0 allocs** | ✅ **最優選擇** |

> [!IMPORTANT]
> **最終決策**: 採用 **Pooled Fixed-Level Skiplist**。在 1:1 Insert/Cancel 場景下，Delete 性能是關鍵，Pooled Skiplist 完勝。

---

## 3. Benchmark 對比結果 (1000 價格層級)

| 操作 | LLRB | Pooled Skiplist | Original Skiplist |
|:---|:---|:---|:---|
| **Insert (1000)** | **215 µs, 0 allocs** | 323 µs, 4 allocs | 407 µs, 3004 allocs |
| **Search** | **121 ns, 0 allocs** | 237 ns, 0 allocs | 458 ns, 1 alloc |
| **Delete (500)** | 202 µs, 0 allocs | **55 µs, 0 allocs** ✅ | 55 µs, 531 allocs |
| **DeleteMin (drain)** | 194 µs, 0 allocs | **11 µs, 0 allocs** ✅ | 26 µs, 62 allocs |
| **Mixed Workload** | 502 µs, 0 allocs | **536 µs, 4 allocs** | 660 µs, 3641 allocs |

> [!NOTE]
> Pooled Skiplist 的 4 allocs 是初始化時的 Slice 分配，**熱路徑上無分配**。

---

## 4. API 參考 (API Reference)

### 4.1 創建

```go
import "github.com/0x5487/matching-engine/structure"

// 基本用法：初始容量 3000
sl := structure.NewPooledSkiplist(3000, time.Now().UnixNano())

// 帶選項：設置上限和擴容回調
sl := structure.NewPooledSkiplistWithOptions(3000, seed, structure.SkiplistOptions{
    MaxCapacity: 100000,  // 0 = 無限制
    OnGrow: func(old, new int32) {
        log.Warnf("Skiplist expanded: %d -> %d", old, new)
    },
})
```

### 4.2 核心操作

| 方法 | 說明 | 時間複雜度 |
|:---|:---|:---|
| `Insert(price)` | 插入價格，返回 (inserted, error) | O(log N) |
| `MustInsert(price)` | 插入價格，失敗則 panic | O(log N) |
| `Delete(price)` | 刪除價格，返回是否成功 | O(log N) |
| `Contains(price)` | 檢查價格是否存在 | O(log N) |
| `Min()` | 獲取最小價格 | O(1) |
| `DeleteMin()` | 刪除並返回最小價格 | O(log N) |
| `Count()` | 獲取元素數量 | O(1) |
| `Capacity()` | 獲取當前容量 | O(1) |

### 4.3 迭代器

```go
iter := sl.Iterator()
for iter.Valid() {
    price := iter.Price()
    // 處理每個價格...
    iter.Next()
}
```

| 方法 | 說明 |
|:---|:---|
| `Iterator()` | 返回迭代器，指向第一個（最小）元素 |
| `iter.Valid()` | 是否指向有效元素 |
| `iter.Next()` | 移動到下一個元素 |
| `iter.Price()` | 獲取當前元素的價格 |

### 4.4 參數說明

| 參數 | 說明 |
|:---|:---|
| `capacity` | 初始容量，建議設為預估最大檔位數（如 3000）|
| `seed` | 隨機數種子，用於生成節點層高。建議使用 `time.Now().UnixNano()` |
| `MaxCapacity` | 可選，最大容量硬上限。0 = 不限制，自動 2x 擴容 |
| `OnGrow` | 可選，擴容時的回調函數 |

---

## 5. Queue.go 整合方案

### 5.1 整合架構

```
當前設計:                          新設計:
+-----------------------+          +-----------------------+
| depthList (skiplist)  |   ==>    | orderedPrices (Pooled)|
| priceList (map->Elem) |   ==>    | priceList (map->Unit) |
+-----------------------+          +-----------------------+
```

### 5.2 代碼變更對照

| 行號 | 當前代碼 | 新代碼 |
|:---|:---|:---|
| 30 | `depthList *skiplist.SkipList` | `orderedPrices *structure.PooledSkiplist` |
| 31 | `priceList map[...]Element` | `priceList map[...]*priceUnit` |
| 130 | `depthList.Set(price, unit)` | `orderedPrices.MustInsert(price)` |
| 175 | `depthList.RemoveElement(el)` | `orderedPrices.Delete(price)` |
| 200 | `depthList.Front()` | `orderedPrices.Min()` |
| 236 | `elem := depthList.Front()` | `iter := orderedPrices.Iterator()` |
| 255 | `elem = elem.Next()` | `iter.Next()` |

### 5.3 FIFO 順序保證

訂單的先進先出 (FIFO) 順序由 `priceUnit` 內部的雙向鏈表維護，**與 Skiplist 無關**：
- `priceUnit.head` → 最早的訂單（優先成交）
- `priceUnit.tail` → 最新的訂單

---

## 6. 測試規範 (Testing Requirements)

### 6.1 測試覆蓋率
- **目標**: ≥ 90%
- **當前**: **93.1%** ✅

### 6.2 測試清單 (18 tests)

| 測試 | 說明 |
|:---|:---|
| `TestPooledSkiplist_BasicOperations` | 基本 Insert/Delete/Contains |
| `TestPooledSkiplist_Delete` | 刪除邊界情況 |
| `TestPooledSkiplist_DeleteMin` | 連續 DeleteMin |
| `TestPooledSkiplist_OracleTest` | 10k 隨機操作對照 map |
| `TestPooledSkiplist_DynamicGrow` | 動態擴容驗證 |
| `TestPooledSkiplist_MaxCapacity` | 容量限制驗證 |
| `TestPooledSkiplist_Iterator` | 迭代器排序驗證 |
| `FuzzPooledSkiplist` | 200k+ 隨機操作 |

### 6.3 Fuzz Testing 命令

```bash
go test -v -fuzz=FuzzPooledSkiplist -fuzztime=10s ./structure/...
```

---

## 7. 評審檢查清單 (Review Checklist)

- [x] **刪除邏輯正確性**: 單元測試通過
- [x] **內存洩漏**: 刪除節點正確歸還到空閒鏈表
- [x] **動態擴容**: 容量不足時自動擴容，不 Panic
- [x] **MaxCapacity 限制**: 超限時返回 `ErrMaxCapacityReached`
- [x] **Oracle Testing**: 與 Go map 對照結果一致
- [x] **Fuzz Testing**: 200k+ 操作無失敗
- [x] **Iterator 排序**: 迭代順序嚴格升序
- [x] **Benchmark**: 熱路徑 0 allocs/op

---

## 8. 後續計劃

- [ ] 整合 Pooled Skiplist 到 `queue.go` 替換現有 `huando/skiplist`
- [ ] 更新 Snapshot/Restore 邏輯以支持新結構
- [ ] 運行完整 E2E 測試驗證撮合正確性
- [ ] 更新 CHANGELOG.md
