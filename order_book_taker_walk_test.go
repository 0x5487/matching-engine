package match

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// IOC and Market orders both walk the opposite side of the book taking what they
// can. These tests pin that walk: the logs it produces, field by field, for the
// same book and request sent down each entry point. They were recorded against the
// two separate loops before those were merged into one, and they must hold
// unchanged after it.

const (
	walkTakerTS = int64(2)
	// takerMark and blockedMark stand in for the two things that legitimately
	// differ between the entry points: the taker's order type and the code of a
	// reject caused by the price bound.
	takerMark   = "TAKER"
	blockedMark = "BLOCKED"
)

// renderLog flattens every field of a log into one line, so a comparison cannot
// miss a field. Fields at their zero value are left out to keep a line short; a
// field that becomes non-zero shows up as a changed line. Keys: seq, c command,
// tr trade, s side, px price, sz size, amt amount, o order, u user, ot order type,
// opx and osz old price and size, mk maker order, mku maker user, why reject
// reason, ts timestamp.
func renderLog(l *OrderBookLog) string {
	out := string(l.Type)
	str := func(key, val string) {
		if val != "" && val != "0" {
			out += " " + key + "=" + val
		}
	}
	str("seq", strconv.FormatUint(l.SeqID, 10))
	str("c", l.CommandID)
	str("tr", strconv.FormatUint(l.TradeID, 10))
	str("s", fmt.Sprint(l.Side))
	str("px", l.Price.String())
	str("sz", l.Size.String())
	str("amt", l.Amount.String())
	str("o", l.OrderID)
	str("u", strconv.FormatUint(l.UserID, 10))
	str("ot", string(l.OrderType))
	str("opx", l.OldPrice.String())
	str("osz", l.OldSize.String())
	str("mk", l.MakerOrderID)
	str("mku", strconv.FormatUint(l.MakerUserID, 10))
	str("why", string(l.RejectReason))
	str("ts", strconv.FormatInt(l.Timestamp, 10))
	return out
}

func renderBatch(batch *LogBatch) []string {
	out := make([]string, 0, len(batch.Logs))
	for _, l := range batch.Logs {
		out = append(out, renderLog(l))
	}
	return out
}

// renderNormalized renders a batch with the taker's order type and the price-bound
// reject code masked, which is everything allowed to differ between IOC and Market.
func renderNormalized(batch *LogBatch, takerType OrderType) []string {
	out := make([]string, 0, len(batch.Logs))
	for _, l := range batch.Logs {
		c := *l
		if c.OrderType == takerType {
			c.OrderType = takerMark
		}
		if c.RejectReason == protocol.RejectReasonPriceMismatch ||
			c.RejectReason == protocol.RejectReasonPriceProtection {
			c.RejectReason = blockedMark
		}
		out = append(out, renderLog(&c))
	}
	return out
}

func newWalkBook() *OrderBook {
	return newOrderBookWithPublisher("test-engine", "BTC-USDT", NewMemoryPublishLog())
}

// walkRest rests a limit order; an iceberg when visible is not "0".
func walkRest(t *testing.T, book *OrderBook, id string, side Side, price, size, visible string) {
	t.Helper()
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "rest-" + id, MarketID: book.marketID, Timestamp: 1},
		OrderID:     id,
		OrderType:   Limit,
		Side:        side,
		Price:       udecimal.MustParse(price),
		Size:        udecimal.MustParse(size),
		VisibleSize: udecimal.MustParse(visible),
	})
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Len(t, batch.Logs, 1, "a resting limit order produces exactly its open log")
}

// walkPlace sends one taker order through PlaceOrder.
func walkPlace(t *testing.T, book *OrderBook, req *protocol.PlaceOrderRequest) *LogBatch {
	t.Helper()
	req.BaseCommand = protocol.BaseCommand{
		UserID: 2, CommandID: "c-" + req.OrderID, MarketID: book.marketID, Timestamp: walkTakerTS,
	}
	batch, err := book.PlaceOrder(req)
	require.NoError(t, err)
	require.NotNil(t, batch)
	return batch
}

// Book layouts. Each rests the side the taker will walk.
func walkAsksLadder(t *testing.T, book *OrderBook) {
	t.Helper()
	walkRest(t, book, "ask-99", Sell, "99", "1", "0")
	walkRest(t, book, "ask-100", Sell, "100", "1", "0")
	walkRest(t, book, "ask-101", Sell, "101", "1", "0")
}

func walkBidsLadder(t *testing.T, book *OrderBook) {
	t.Helper()
	walkRest(t, book, "bid-101", Buy, "101", "1", "0")
	walkRest(t, book, "bid-100", Buy, "100", "1", "0")
	walkRest(t, book, "bid-99", Buy, "99", "1", "0")
}

// The iceberg shows 2 of 5 at 100; a plain order queues behind it at the same price,
// so each reload of the iceberg goes behind that plain order.
func walkAsksIceberg(t *testing.T, book *OrderBook) {
	t.Helper()
	walkRest(t, book, "ask-ice", Sell, "100", "5", "2")
	walkRest(t, book, "ask-100", Sell, "100", "1", "0")
	walkRest(t, book, "ask-101", Sell, "101", "3", "0")
}

func walkBidsIceberg(t *testing.T, book *OrderBook) {
	t.Helper()
	walkRest(t, book, "bid-ice", Buy, "100", "5", "2")
	walkRest(t, book, "bid-100", Buy, "100", "1", "0")
	walkRest(t, book, "bid-99", Buy, "99", "3", "0")
}

func walkEmpty(*testing.T, *OrderBook) {}

// TestIOCAndMarketWithProtectionPriceWalkIdentically sends the same request to two
// identical books, once as IOC at price P and once as a Market order with protection
// price P sized in base. The logs must match entry by entry; only the taker's order
// type and the code of the reject caused by the bound may differ. The IOC logs are
// also pinned literally.
func TestIOCAndMarketWithProtectionPriceWalkIdentically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rest  func(*testing.T, *OrderBook)
		side  Side
		price string
		size  string
		// blocked reports whether the walk ends on a reject caused by the bound.
		blocked bool
		wantIOC []string
	}{
		{
			name: "buy partial level inside the bound", rest: walkAsksLadder, side: Buy, price: "100", size: "1.5",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=ioc mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=0.5 amt=50 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
			},
		},
		{
			name: "buy stops at the first level beyond the bound", rest: walkAsksLadder, side: Buy,
			price: "100", size: "3", blocked: true,
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=ioc mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
				"reject c=c-taker s=1 px=100 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
		{
			name: "buy bound is inclusive and the remainder finds no liquidity", rest: walkAsksLadder, side: Buy,
			price: "101", size: "4",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=ioc mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=1 px=101 sz=1 amt=101 o=taker u=2 ot=ioc mk=ask-101 mku=1 ts=2",
				"reject c=c-taker s=1 px=101 sz=1 o=taker u=2 ot=ioc why=no_liquidity ts=2",
			},
		},
		{
			name: "buy blocked before any fill", rest: walkAsksLadder, side: Buy, price: "98", size: "1", blocked: true,
			wantIOC: []string{
				"reject c=c-taker s=1 px=98 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
		{
			name: "buy exact fill of one level", rest: walkAsksLadder, side: Buy, price: "99", size: "1",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=ioc mk=ask-99 mku=1 ts=2",
			},
		},
		{
			name: "buy on an empty book", rest: walkEmpty, side: Buy, price: "100", size: "1",
			wantIOC: []string{
				"reject c=c-taker s=1 px=100 sz=1 o=taker u=2 ot=ioc why=no_liquidity ts=2",
			},
		},
		{
			name: "buy through an iceberg, partial", rest: walkAsksIceberg, side: Buy, price: "100", size: "4",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=2 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
			},
		},
		{
			name: "buy through an iceberg and past it", rest: walkAsksIceberg, side: Buy, price: "101", size: "20",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=2 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=1 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=4 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"match seq=4 c=c-taker tr=5 s=1 px=101 sz=3 amt=303 o=taker u=2 ot=ioc mk=ask-101 mku=1 ts=2",
				"reject c=c-taker s=1 px=101 sz=11 o=taker u=2 ot=ioc why=no_liquidity ts=2",
			},
		},
		{
			name: "buy through an iceberg, blocked above it", rest: walkAsksIceberg, side: Buy,
			price: "100", size: "7", blocked: true,
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=2 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=1 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=4 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=ask-ice mku=1 ts=2",
				"reject c=c-taker s=1 px=100 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
		{
			name: "sell partial level inside the bound", rest: walkBidsLadder, side: Sell, price: "100", size: "1.5",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=ioc mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=0.5 amt=50 o=taker u=2 ot=ioc mk=bid-100 mku=1 ts=2",
			},
		},
		{
			name: "sell stops at the first level beyond the bound", rest: walkBidsLadder, side: Sell,
			price: "100", size: "3", blocked: true,
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=ioc mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-100 mku=1 ts=2",
				"reject c=c-taker s=2 px=100 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
		{
			name: "sell bound is inclusive and the remainder finds no liquidity", rest: walkBidsLadder, side: Sell,
			price: "99", size: "4",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=ioc mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=2 px=99 sz=1 amt=99 o=taker u=2 ot=ioc mk=bid-99 mku=1 ts=2",
				"reject c=c-taker s=2 px=99 sz=1 o=taker u=2 ot=ioc why=no_liquidity ts=2",
			},
		},
		{
			name: "sell blocked before any fill", rest: walkBidsLadder, side: Sell,
			price: "102", size: "1", blocked: true,
			wantIOC: []string{
				"reject c=c-taker s=2 px=102 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
		{
			name: "sell on an empty book", rest: walkEmpty, side: Sell, price: "100", size: "1",
			wantIOC: []string{
				"reject c=c-taker s=2 px=100 sz=1 o=taker u=2 ot=ioc why=no_liquidity ts=2",
			},
		},
		{
			name: "sell through an iceberg, partial", rest: walkBidsIceberg, side: Sell, price: "100", size: "4",
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=bid-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=1 px=100 sz=2 o=bid-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-ice mku=1 ts=2",
			},
		},
		{
			name: "sell through an iceberg, blocked below it", rest: walkBidsIceberg, side: Sell,
			price: "100", size: "7", blocked: true,
			wantIOC: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=bid-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=1 px=100 sz=2 o=bid-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=2 px=100 sz=2 amt=200 o=taker u=2 ot=ioc mk=bid-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=1 px=100 sz=1 o=bid-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=4 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=ioc mk=bid-ice mku=1 ts=2",
				"reject c=c-taker s=2 px=100 sz=1 o=taker u=2 ot=ioc why=price_mismatch ts=2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			iocBook, marketBook := newWalkBook(), newWalkBook()
			tt.rest(t, iocBook)
			tt.rest(t, marketBook)

			price := udecimal.MustParse(tt.price)
			size := udecimal.MustParse(tt.size)
			ioc := walkPlace(t, iocBook, &protocol.PlaceOrderRequest{
				OrderID: "taker", OrderType: IOC, Side: tt.side, Price: price, Size: size,
			})
			market := walkPlace(t, marketBook, &protocol.PlaceOrderRequest{
				OrderID: "taker", OrderType: Market, Side: tt.side, Price: price, Size: size,
			})

			assert.Equal(t, tt.wantIOC, renderBatch(ioc), "the IOC logs, pinned")
			assert.Equal(t, renderNormalized(ioc, IOC), renderNormalized(market, Market),
				"IOC at P and Market with protection P walk the same book identically")

			// The two codes themselves, on the last log of each walk.
			last := func(b *LogBatch) *OrderBookLog { return b.Logs[len(b.Logs)-1] }
			require.NotEmpty(t, ioc.Logs)
			require.NotEmpty(t, market.Logs)
			if tt.blocked {
				assert.Equal(t, protocol.RejectReasonPriceMismatch, last(ioc).RejectReason)
				assert.Equal(t, protocol.RejectReasonPriceProtection, last(market).RejectReason)
			} else if last(ioc).Type == protocol.LogTypeReject {
				assert.Equal(t, protocol.RejectReasonNoLiquidity, last(ioc).RejectReason)
				assert.Equal(t, protocol.RejectReasonNoLiquidity, last(market).RejectReason)
			}

			// The book each walk leaves behind is the same too.
			assert.Equal(t, iocBook.Snapshot().Bids, marketBook.Snapshot().Bids)
			assert.Equal(t, iocBook.Snapshot().Asks, marketBook.Snapshot().Asks)
			assert.Equal(t, iocBook.SeqID(), marketBook.SeqID())
			assert.Equal(t, iocBook.TradeID(), marketBook.TradeID())
		})
	}
}

// TestMarketByQuoteWalk pins a market order sized in quote: its fills, the quote it
// has left, iceberg reloads along the way, and a remainder smaller than one step.
func TestMarketByQuoteWalk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rest  func(*testing.T, *OrderBook)
		side  Side
		quote string
		price string
		step  string
		want  []string
	}{
		{
			name: "buy through an iceberg, the rest runs out of liquidity", rest: walkAsksIceberg, side: Buy,
			quote: "1000", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=market mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=2 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=1 px=100 sz=2 amt=200 o=taker u=2 ot=market mk=ask-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=2 px=100 sz=1 o=ask-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=4 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-ice mku=1 ts=2",
				"match seq=4 c=c-taker tr=5 s=1 px=101 sz=3 amt=303 o=taker u=2 ot=market mk=ask-101 mku=1 ts=2",
				"reject c=c-taker s=1 sz=97 o=taker u=2 ot=market why=no_liquidity ts=2",
			},
		},
		{
			name: "buy leaves less than one step", rest: walkAsksLadder, side: Buy,
			quote: "150", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=market mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=0.5 amt=50 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
				"reject c=c-taker s=1 sz=1 o=taker u=2 ot=market why=no_liquidity ts=2",
			},
		},
		{
			name: "buy spends the whole quote", rest: walkAsksLadder, side: Buy,
			quote: "199", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=market mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
			},
		},
		{
			name: "buy with a protection price", rest: walkAsksLadder, side: Buy,
			quote: "1000", price: "100", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=market mk=ask-99 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
				"reject c=c-taker s=1 px=100 sz=801 o=taker u=2 ot=market why=price_protection ts=2",
			},
		},
		{
			name: "sell through an iceberg, the rest runs out of liquidity", rest: walkBidsIceberg, side: Sell,
			quote: "1000", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=100 sz=2 amt=200 o=taker u=2 ot=market mk=bid-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=1 px=100 sz=2 o=bid-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=bid-100 mku=1 ts=2",
				"match seq=4 c=c-taker tr=3 s=2 px=100 sz=2 amt=200 o=taker u=2 ot=market mk=bid-ice mku=1 ts=2",
				"open seq=4 c=c-taker s=1 px=100 sz=1 o=bid-ice u=1 ot=limit ts=2",
				"match seq=4 c=c-taker tr=4 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=bid-ice mku=1 ts=2",
				"match seq=4 c=c-taker tr=5 s=2 px=99 sz=3 amt=297 o=taker u=2 ot=market mk=bid-99 mku=1 ts=2",
				"reject c=c-taker s=2 sz=103 o=taker u=2 ot=market why=no_liquidity ts=2",
			},
		},
		{
			name: "sell leaves less than one step", rest: walkBidsLadder, side: Sell,
			quote: "150", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=market mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=0.4 amt=40 o=taker u=2 ot=market mk=bid-100 mku=1 ts=2",
				"reject c=c-taker s=2 sz=9 o=taker u=2 ot=market why=no_liquidity ts=2",
			},
		},
		{
			name: "sell spends the whole quote", rest: walkBidsLadder, side: Sell,
			quote: "201", price: "0", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=market mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=bid-100 mku=1 ts=2",
			},
		},
		{
			name: "sell with a protection price", rest: walkBidsLadder, side: Sell,
			quote: "1000", price: "100", step: "0.1", want: []string{
				"match seq=4 c=c-taker tr=1 s=2 px=101 sz=1 amt=101 o=taker u=2 ot=market mk=bid-101 mku=1 ts=2",
				"match seq=4 c=c-taker tr=2 s=2 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=bid-100 mku=1 ts=2",
				"reject c=c-taker s=2 px=100 sz=799 o=taker u=2 ot=market why=price_protection ts=2",
			},
		},
		{
			name: "quote order on an empty book", rest: walkEmpty, side: Buy,
			quote: "1000", price: "0", step: "0.1", want: []string{
				"reject c=c-taker s=1 sz=1000 o=taker u=2 ot=market why=no_liquidity ts=2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			book := newWalkBook()
			tt.rest(t, book)
			batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
				OrderID: "taker", OrderType: Market, Side: tt.side, Price: udecimal.MustParse(tt.price),
				QuoteSize: udecimal.MustParse(tt.quote), StepSize: udecimal.MustParse(tt.step),
			})
			assert.Equal(t, tt.want, renderBatch(batch))
		})
	}
}

// TestMarketWithBothSizesKeepsQuoteInTheEmptyBookReject documents an existing
// quirk rather than endorsing it: a market order carrying both a Size and a
// QuoteSize walks by Size, but the reject for an exhausted book reports QuoteSize,
// while every other reject reports the unfilled Size.
func TestMarketWithBothSizesKeepsQuoteInTheEmptyBookReject(t *testing.T) {
	t.Parallel()

	t.Run("exhausted book reports the quote", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "ask-100", Sell, "100", "1", "0")
		batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
			OrderID: "taker", OrderType: Market, Side: Buy,
			Size: udecimal.MustParse("2"), QuoteSize: udecimal.MustParse("50"),
		})
		assert.Equal(t, []string{
			"match seq=2 c=c-taker tr=1 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
			"reject c=c-taker s=1 sz=50 o=taker u=2 ot=market why=no_liquidity ts=2",
		}, renderBatch(batch))
	})

	t.Run("protection price reports the unfilled size", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "ask-100", Sell, "100", "1", "0")
		walkRest(t, book, "ask-101", Sell, "101", "1", "0")
		batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
			OrderID: "taker", OrderType: Market, Side: Buy, Price: udecimal.MustParse("100"),
			Size: udecimal.MustParse("2"), QuoteSize: udecimal.MustParse("50"),
		})
		assert.Equal(t, []string{
			"match seq=3 c=c-taker tr=1 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
			"reject c=c-taker s=1 px=100 sz=1 o=taker u=2 ot=market why=price_protection ts=2",
		}, renderBatch(batch))
	})
}

// A market order with Price 0 has no bound; an IOC with Price 0 is refused.
func TestZeroPriceMarketIsUnboundedAndZeroPriceIOCIsRefused(t *testing.T) {
	t.Parallel()

	t.Run("market without a price takes every level", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkAsksLadder(t, book)
		batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
			OrderID: "taker", OrderType: Market, Side: Buy, Size: udecimal.MustParse("3"),
		})
		assert.Equal(t, []string{
			"match seq=4 c=c-taker tr=1 s=1 px=99 sz=1 amt=99 o=taker u=2 ot=market mk=ask-99 mku=1 ts=2",
			"match seq=4 c=c-taker tr=2 s=1 px=100 sz=1 amt=100 o=taker u=2 ot=market mk=ask-100 mku=1 ts=2",
			"match seq=4 c=c-taker tr=3 s=1 px=101 sz=1 amt=101 o=taker u=2 ot=market mk=ask-101 mku=1 ts=2",
		}, renderBatch(batch))
		assert.Equal(t, int64(0), book.askQueue.depthCount())
	})

	for _, side := range []Side{Buy, Sell} {
		t.Run(fmt.Sprintf("ioc without a price is refused, side %d", side), func(t *testing.T) {
			t.Parallel()
			book := newWalkBook()
			walkAsksLadder(t, book)
			walkRest(t, book, "bid-90", Buy, "90", "1", "0")
			batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
				OrderID: "taker", OrderType: IOC, Side: side, Size: udecimal.MustParse("1"),
			})
			require.Len(t, batch.Logs, 1)
			assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type)
			assert.Equal(t, protocol.RejectReasonInvalidPayload, batch.Logs[0].RejectReason)
			assert.Equal(t, int64(3), book.askQueue.depthCount(), "a refused order does not touch the book")
			assert.Equal(t, int64(1), book.bidQueue.depthCount(), "a refused order does not touch the book")
		})
	}

	// PlaceOrder refuses a zero-price IOC before the handler runs; this pins what
	// the handler itself does if one ever reaches it: the price bound is required,
	// so a buy at 0 is below every ask and is turned away by the bound.
	t.Run("ioc handler treats price 0 as a bound", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkAsksLadder(t, book)
		order := acquireOrder()
		order.ID, order.Side, order.Type, order.UserID = "taker", Buy, IOC, 2
		order.Price, order.Size, order.Timestamp = udecimal.Zero, udecimal.MustParse("1"), walkTakerTS
		// The public calls reclaim the batch when they start; this direct handler call does the same.
		book.batch.reclaim()
		book.handleIOCOrder("taker-cmd", order, walkTakerTS)
		batch := &book.batch
		require.Len(t, batch.Logs, 1)
		assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type)
		assert.Equal(t, protocol.RejectReasonPriceMismatch, batch.Logs[0].RejectReason)
		assert.Equal(t, int64(3), book.askQueue.depthCount())
	})
}
