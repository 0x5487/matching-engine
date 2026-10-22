package match

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// The book is the one owner of a resting order: its routing (shard, fee rates, assets), its user and its filled size
// are kept on the Order, and every match log carries the maker as the trade left it. Nothing outside the book keeps a
// copy of any of it (AGENTS.md 1.8 of the consumer).

// routedPlace is a limit order that carries routing. The fee rates and assets are fixed, so a test can tell them apart
// from a zero value.
func routedPlace(id string, userID uint64, side Side, price, size float64, shard uint32) *protocol.PlaceOrderRequest {
	req := newPlaceCmd(id, Limit, side, price, size)
	req.UserID = userID
	req.OmsShard = shard
	req.MakerFeeRate = udecimal.MustParse("0.001")
	req.TakerFeeRate = udecimal.MustParse("0.002")
	req.BaseAsset = "BTC"
	req.QuoteAsset = "USDT"
	return req
}

// placeCopy places req on book and returns a copy of each log of the batch: the batch belongs to the book and is
// reclaimed by the next call.
func placeCopy(t *testing.T, book *OrderBook, req *protocol.PlaceOrderRequest) []OrderBookLog {
	t.Helper()
	req.MarketID = book.marketID
	req.CommandID = "cmd-" + req.OrderID
	if req.Timestamp == 0 {
		req.Timestamp = 1
	}
	batch, err := book.PlaceOrder(req)
	require.NoError(t, err)
	out := make([]OrderBookLog, 0, len(batch.Logs))
	for _, l := range batch.Logs {
		out = append(out, *l)
	}
	return out
}

// matchStep is what one match log of a maker's book must show: the trade size, and the maker's visible, hidden and
// filled size after the trade, as strings.
type matchStep struct{ size, visible, hidden, filled string }

func TestRestingOrderCarriesItsRouting(t *testing.T) {
	t.Parallel()
	book := NewOrderBook("BTC-USDT")
	placeCopy(t, book, routedPlace("maker", 201, Sell, 100, 5, 7))

	got, ok := book.Order("maker")
	require.True(t, ok)
	assert.Equal(t, uint64(201), got.UserID)
	assert.Equal(t, Sell, got.Side)
	assert.Equal(t, uint32(7), got.OmsShard)
	assert.True(t, got.MakerFeeRate.Equal(udecimal.MustParse("0.001")), "maker rate %s", got.MakerFeeRate)
	assert.True(t, got.TakerFeeRate.Equal(udecimal.MustParse("0.002")), "taker rate %s", got.TakerFeeRate)
	assert.Equal(t, "BTC", got.BaseAsset)
	assert.Equal(t, "USDT", got.QuoteAsset)
	assert.True(t, got.FilledSize.IsZero(), "a new order has traded nothing")

	_, ok = book.Order("nope")
	assert.False(t, ok, "an order the book does not hold is not found")
}

func TestOrderIsNotFoundOnceItIsGone(t *testing.T) {
	t.Parallel()
	book := NewOrderBook("BTC-USDT")
	placeCopy(t, book, routedPlace("maker", 201, Sell, 100, 5, 7))
	assert.Equal(t, 1, book.OrderCount())
	placeCopy(t, book, routedPlace("taker", 101, Buy, 100, 5, 9))
	assert.Equal(t, 0, book.OrderCount(), "a filled maker and a filled taker leave nothing resting")

	_, ok := book.Order("maker")
	assert.False(t, ok, "a maker filled whole is no longer on the book")
	_, ok = book.Order("taker")
	assert.False(t, ok, "a taker filled whole never rests")
}

// TestMatchLogCarriesTheMakerAsTheTradeLeftIt pins the maker copy of each match log: its routing, its size left on the
// book after the trade and before any replenish, its filled size. An iceberg maker is hit more than once in one call.
func TestMatchLogCarriesTheMakerAsTheTradeLeftIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		maker   *protocol.PlaceOrderRequest
		taker   *protocol.PlaceOrderRequest
		matches []matchStep
	}{
		{
			name:  "maker consumed whole",
			maker: routedPlace("maker", 201, Sell, 100, 1, 7),
			taker: routedPlace("taker", 101, Buy, 100, 1, 9),
			matches: []matchStep{
				{size: "1", visible: "0", hidden: "0", filled: "1"},
			},
		},
		{
			name:  "maker partly consumed",
			maker: routedPlace("maker", 201, Sell, 100, 3, 7),
			taker: routedPlace("taker", 101, Buy, 100, 1, 9),
			matches: []matchStep{
				{size: "1", visible: "2", hidden: "0", filled: "1"},
			},
		},
		{
			name: "iceberg maker replenished and hit again in one call",
			maker: func() *protocol.PlaceOrderRequest {
				m := routedPlace("maker", 201, Sell, 100, 10, 7)
				m.VisibleSize = udecimal.MustParse("4")
				return m
			}(),
			taker: routedPlace("taker", 101, Buy, 100, 10, 9),
			matches: []matchStep{
				{size: "4", visible: "0", hidden: "6", filled: "4"},
				{size: "4", visible: "0", hidden: "2", filled: "8"},
				{size: "2", visible: "0", hidden: "0", filled: "10"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			book := NewOrderBook("BTC-USDT")
			placeCopy(t, book, tc.maker)
			logs := placeCopy(t, book, tc.taker)

			var matches []OrderBookLog
			for _, l := range logs {
				if l.Type == protocol.LogTypeMatch {
					matches = append(matches, l)
				}
			}
			require.Len(t, matches, len(tc.matches))
			for i, want := range tc.matches {
				m := matches[i]
				assert.Equal(t, "maker", m.MakerOrder.ID, "match %d: maker id", i)
				assert.Equal(t, uint64(201), m.MakerOrder.UserID, "match %d: maker user", i)
				assert.Equal(t, uint32(7), m.MakerOrder.OmsShard, "match %d: maker shard", i)
				assert.True(t, m.MakerOrder.MakerFeeRate.Equal(udecimal.MustParse("0.001")), "match %d: maker rate", i)
				assert.Equal(t, "BTC", m.MakerOrder.BaseAsset, "match %d", i)
				assert.Equal(t, "USDT", m.MakerOrder.QuoteAsset, "match %d", i)
				assert.Equal(t, Sell, m.MakerOrder.Side, "match %d", i)
				assert.True(t, m.MatchSize.Equal(udecimal.MustParse(want.size)), "match %d: size %s", i, m.MatchSize)
				assert.True(t, m.MakerOrder.Size.Equal(udecimal.MustParse(want.visible)),
					"match %d: maker visible after the trade %s", i, m.MakerOrder.Size)
				assert.True(t, m.MakerOrder.HiddenSize.Equal(udecimal.MustParse(want.hidden)),
					"match %d: maker hidden after the trade %s", i, m.MakerOrder.HiddenSize)
				assert.True(t, m.MakerOrder.FilledSize.Equal(udecimal.MustParse(want.filled)),
					"match %d: maker filled after the trade %s", i, m.MakerOrder.FilledSize)
			}
		})
	}
}

// TestMatchLogNamesTheMakerOnce: a match log names its maker in one place, MakerOrder. A second field for the maker's
// ID or user could disagree with the copy, and a reader would have to choose between them. The check reads the type and
// the encoded bytes, so a field that is kept under another name or tag is caught too.
func TestMatchLogNamesTheMakerOnce(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[OrderBookLog]()
	for _, name := range []string{"MakerOrderID", "MakerUserID"} {
		_, ok := typ.FieldByName(name)
		assert.False(t, ok, "OrderBookLog has the field %s: the maker is named once, in MakerOrder", name)
	}

	book := NewOrderBook("BTC-USDT")
	placeCopy(t, book, routedPlace("maker", 201, Sell, 100, 1, 7))
	logs := placeCopy(t, book, routedPlace("taker", 101, Buy, 100, 1, 9))
	var match *OrderBookLog
	for i := range logs {
		if logs[i].Type == protocol.LogTypeMatch {
			match = &logs[i]
		}
	}
	require.NotNil(t, match)

	raw, err := json.Marshal(match)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	for _, key := range []string{"maker_order_id", "maker_user_id"} {
		_, ok := fields[key]
		assert.False(t, ok, "a match log is encoded with the key %q: the maker is named once, in maker_order", key)
	}
	var maker struct {
		ID     string `json:"id"`
		UserID uint64 `json:"user_id"`
	}
	require.NoError(t, json.Unmarshal(fields["maker_order"], &maker))
	assert.Equal(t, "maker", maker.ID)
	assert.Equal(t, uint64(201), maker.UserID)
}

// TestFilledSizeFollowsEveryMatchAndAmend: the book's filled size of a resting order counts every match it takes or
// gives, through an amend that re-matches it as well.
func TestFilledSizeFollowsEveryMatchAndAmend(t *testing.T) {
	t.Parallel()
	book := NewOrderBook("BTC-USDT")
	placeCopy(t, book, routedPlace("maker", 201, Sell, 100, 5, 7))
	placeCopy(t, book, routedPlace("buy-rests", 101, Buy, 99, 3, 9))

	// The buy rests below the ask and is then amended up to the ask: a priority-losing amend re-matches it.
	req := &protocol.AmendOrderRequest{
		OrderID:  "buy-rests",
		NewPrice: udecimal.MustParse("100"),
		NewSize:  udecimal.MustParse("3"),
	}
	req.CommandID, req.Timestamp, req.MarketID, req.UserID = "amend-buy", 2, book.marketID, 101
	_, err := book.AmendOrder(req)
	require.NoError(t, err)

	_, ok := book.Order("buy-rests")
	assert.False(t, ok, "the amended buy filled and is gone")
	maker, ok := book.Order("maker")
	require.True(t, ok)
	assert.True(t, maker.FilledSize.Equal(udecimal.MustParse("3")), "maker filled %s", maker.FilledSize)
	assert.True(t, maker.Size.Equal(udecimal.MustParse("2")), "maker left %s", maker.Size)
	assert.Equal(t, uint32(7), maker.OmsShard)

	// A taker that rests after a partial fill carries what it has traded: the 2 left of maker at 100 and the 2 of
	// sell-part at 105 fill it to 4, and the rest rests at 105.
	placeCopy(t, book, routedPlace("sell-part", 202, Sell, 105, 2, 4))
	placeCopy(t, book, routedPlace("buy-part", 203, Buy, 105, 5, 6))
	part, ok := book.Order("buy-part")
	require.True(t, ok)
	assert.True(t, part.FilledSize.Equal(udecimal.MustParse("4")), "buy-part filled %s", part.FilledSize)
	assert.True(t, part.Size.Equal(udecimal.MustParse("1")), "buy-part left %s", part.Size)
	assert.Equal(t, uint32(6), part.OmsShard)
}

// TestAmendThatLosesPriorityKeepsRouting: an amend that only raises the size keeps the order's routing and its
// filled size.
func TestAmendThatLosesPriorityKeepsRouting(t *testing.T) {
	t.Parallel()
	book := NewOrderBook("BTC-USDT")
	placeCopy(t, book, routedPlace("buy", 101, Buy, 90, 2, 9))
	req := &protocol.AmendOrderRequest{
		OrderID:  "buy",
		NewPrice: udecimal.MustParse("90"),
		NewSize:  udecimal.MustParse("4"),
	}
	req.CommandID, req.Timestamp, req.MarketID, req.UserID = "amend", 2, book.marketID, 101
	_, err := book.AmendOrder(req)
	require.NoError(t, err)

	got, ok := book.Order("buy")
	require.True(t, ok)
	assert.Equal(t, uint32(9), got.OmsShard)
	assert.True(t, got.MakerFeeRate.Equal(udecimal.MustParse("0.001")))
	assert.Equal(t, "USDT", got.QuoteAsset)
	assert.True(t, got.Size.Equal(udecimal.MustParse("4")))
	assert.True(t, got.FilledSize.IsZero())
}

// TestSnapshotBetweenTwoFillsRestoresTheSameLogs: a book restored between two fills of one resting order gives the
// same logs, maker copies included, as the book that was never snapshotted.
func TestSnapshotBetweenTwoFillsRestoresTheSameLogs(t *testing.T) {
	t.Parallel()
	live := NewOrderBook("BTC-USDT")
	placeCopy(t, live, routedPlace("maker", 201, Sell, 100, 10, 7))
	placeCopy(t, live, routedPlace("taker-1", 101, Buy, 100, 3, 9))

	restored := NewOrderBook("BTC-USDT")
	restored.Restore(live.Snapshot())

	want := placeCopy(t, live, routedPlace("taker-2", 102, Buy, 100, 4, 8))
	got := placeCopy(t, restored, routedPlace("taker-2", 102, Buy, 100, 4, 8))
	require.Equal(t, want, got)

	a, ok := live.Order("maker")
	require.True(t, ok)
	b, ok := restored.Order("maker")
	require.True(t, ok)
	assert.Equal(t, a, b)
	assert.True(t, b.FilledSize.Equal(udecimal.MustParse("7")))
}
