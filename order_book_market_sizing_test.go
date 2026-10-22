package match

import (
	"encoding/json"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// marketSizing is how a market request carries its size: a base quantity, a quote
// amount, both, or neither. An empty field is not sent.
type marketSizing struct {
	name     string
	size     string
	quote    string
	accepted bool
}

var marketSizings = []marketSizing{
	{name: "base only", size: "1", accepted: true},
	{name: "quote only", quote: "100", accepted: true},
	{name: "both", size: "1", quote: "100"},
	{name: "neither"},
}

// marketRequest builds a market order that carries exactly the sizes of s. The step
// is 0.01, so a quote of 100 at price 100 buys one whole unit.
func marketRequest(id string, side Side, price string, s marketSizing) *protocol.PlaceOrderRequest {
	req := &protocol.PlaceOrderRequest{
		OrderID:   id,
		OrderType: Market,
		Side:      side,
		Price:     udecimal.MustParse(price),
		StepSize:  udecimal.MustParse("0.01"),
	}
	if s.size != "" {
		req.Size = udecimal.MustParse(s.size)
	}
	if s.quote != "" {
		req.QuoteSize = udecimal.MustParse(s.quote)
	}
	return req
}

// TestMarketOrderTakesExactlyOneSizing: a market order carries a base quantity or a
// quote amount, never both and never neither. A valid order walks the book; an
// invalid one is refused with one reject, and the book and its version do not move.
func TestMarketOrderTakesExactlyOneSizing(t *testing.T) {
	t.Parallel()

	sides := []struct {
		name   string
		taker  Side
		rest   Side
		restID string
	}{
		{name: "buy", taker: Buy, rest: Sell, restID: "ask-100"},
		{name: "sell", taker: Sell, rest: Buy, restID: "bid-100"},
	}
	for _, sd := range sides {
		for _, s := range marketSizings {
			t.Run(sd.name+" "+s.name, func(t *testing.T) {
				t.Parallel()

				book := newWalkBook()
				walkRest(t, book, sd.restID, sd.rest, "100", "10", "0")
				version := book.SeqID()

				batch := walkPlace(t, book, marketRequest("taker", sd.taker, "0", s))
				rest, ok := book.Remaining(sd.restID)
				require.True(t, ok, "the resting order is still on the book")

				if s.accepted {
					require.NotEmpty(t, batch.Logs)
					assert.Equal(t, protocol.LogTypeMatch, batch.Logs[0].Type, "a valid order walks the book")
					assert.Equal(t, "9", rest.String(), "the maker gives one unit")
					assert.Greater(t, book.SeqID(), version, "a fill moves the depth version")
					return
				}

				require.Len(t, batch.Logs, 1, "an invalid market order produces one reject and nothing else")
				assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type)
				assert.Equal(t, protocol.RejectReasonInvalidPayload, batch.Logs[0].RejectReason)
				assert.Equal(t, "10", rest.String(), "the book is untouched")
				assert.Equal(t, 1, book.OrderCount(), "no order was added or removed")
				assert.Equal(t, version, book.SeqID(), "the depth version does not move")
			})
		}
	}
}

// restingOrder is a limit order a case rests on the book before the taker arrives.
type restingOrder struct {
	id    string
	side  Side
	price string
	size  string
}

// marketRejectCase is a market order that the walk refuses after whatever it could
// fill. Each reject must carry the unfilled amount in the unit the order was placed
// in, and in no other field.
type marketRejectCase struct {
	name               string
	rest               []restingOrder
	s                  marketSizing
	price              string
	reason             protocol.RejectReason
	wantRemaining      string // empty when the reject must not carry remaining
	wantQuoteRemaining string // empty when the reject must not carry quote_remaining
}

var marketRejectCases = []marketRejectCase{
	// Placed in base quantity.
	{
		name: "base, exhausted book", s: marketSizing{size: "3"},
		price: "0", reason: protocol.RejectReasonNoLiquidity, wantRemaining: "3",
	},
	{
		name: "base, protection bound before any fill", s: marketSizing{size: "3"},
		rest:  []restingOrder{{id: "ask-101", side: Sell, price: "101", size: "5"}},
		price: "100", reason: protocol.RejectReasonPriceProtection, wantRemaining: "3",
	},
	{
		name: "base, protection bound after a partial fill", s: marketSizing{size: "3"},
		rest: []restingOrder{
			{id: "ask-100", side: Sell, price: "100", size: "1"},
			{id: "ask-101", side: Sell, price: "101", size: "5"},
		},
		price: "100", reason: protocol.RejectReasonPriceProtection, wantRemaining: "2",
	},
	{
		name: "base, exhausted book after a partial fill", s: marketSizing{size: "3"},
		rest:  []restingOrder{{id: "ask-100", side: Sell, price: "100", size: "1"}},
		price: "0", reason: protocol.RejectReasonNoLiquidity, wantRemaining: "2",
	},
	// Placed in quote amount.
	{
		name: "quote, exhausted book", s: marketSizing{quote: "100"},
		price: "0", reason: protocol.RejectReasonNoLiquidity, wantQuoteRemaining: "100",
	},
	{
		name: "quote, protection bound before any fill", s: marketSizing{quote: "100"},
		rest:  []restingOrder{{id: "ask-101", side: Sell, price: "101", size: "5"}},
		price: "100", reason: protocol.RejectReasonPriceProtection, wantQuoteRemaining: "100",
	},
	{
		name: "quote, protection bound after a partial fill", s: marketSizing{quote: "100"},
		rest: []restingOrder{
			{id: "ask-100", side: Sell, price: "100", size: "0.5"},
			{id: "ask-101", side: Sell, price: "101", size: "5"},
		},
		price: "100", reason: protocol.RejectReasonPriceProtection, wantQuoteRemaining: "50",
	},
	{
		name: "quote, too small to buy one step", s: marketSizing{quote: "0.5"},
		rest:  []restingOrder{{id: "ask-100", side: Sell, price: "100", size: "10"}},
		price: "0", reason: protocol.RejectReasonNoLiquidity, wantQuoteRemaining: "0.5",
	},
}

// TestMarketRejectReportsOnlyTheUnitItWasPlacedIn: a base-quantity reject carries
// remaining and a quote-amount reject carries quote_remaining, and the other field is
// absent. The keys are read from the raw JSON, not from the struct.
func TestMarketRejectReportsOnlyTheUnitItWasPlacedIn(t *testing.T) {
	t.Parallel()

	for _, tt := range marketRejectCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			book := newWalkBook()
			for _, r := range tt.rest {
				walkRest(t, book, r.id, r.side, r.price, r.size, "0")
			}
			batch := walkPlace(t, book, marketRequest("taker", Buy, tt.price, tt.s))

			require.NotEmpty(t, batch.Logs)
			reject := batch.Logs[len(batch.Logs)-1]
			require.Equal(t, protocol.LogTypeReject, reject.Type)
			assert.Equal(t, tt.reason, reject.RejectReason)

			raw, err := json.Marshal(reject)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &fields))

			if tt.wantRemaining == "" {
				assert.NotContains(t, fields, "remaining", "a quote-placed reject carries no base remaining")
			} else {
				assert.Equal(t, `"`+tt.wantRemaining+`"`, string(fields["remaining"]))
			}
			if tt.wantQuoteRemaining == "" {
				assert.NotContains(t, fields, "quote_remaining", "a base-placed reject carries no quote remaining")
			} else {
				assert.Equal(t, `"`+tt.wantQuoteRemaining+`"`, string(fields["quote_remaining"]))
			}
		})
	}
}

// TestMarketWalkPicksTheRejectUnitByPlacement: the walk is given both a quantity and a
// quote, which the entry check never lets through. The reject still follows the
// order's placement, so a quantity-placed order reports Remaining and nothing in
// QuoteRemaining. The unit is decided once, in the walk, not by which field is set.
func TestMarketWalkPicksTheRejectUnitByPlacement(t *testing.T) {
	t.Parallel()

	book := newWalkBook()
	book.beginCall()
	defer book.endCall()
	book.batch.reclaim()

	order := acquireOrder()
	order.ID = "taker"
	order.UserID = 2
	order.Side = Buy
	order.Type = Market
	order.Size = udecimal.MustParse("2")
	book.handleMarketOrder(
		"c-taker", order,
		udecimal.MustParse("100"), udecimal.MustParse("0.01"), walkTakerTS,
	)

	require.Len(t, book.batch.Logs, 1)
	reject := book.batch.Logs[0]
	assert.Equal(t, protocol.RejectReasonNoLiquidity, reject.RejectReason)
	assert.Equal(t, "2", reject.Remaining.String(), "a quantity-placed order reports its quantity")
	assert.True(t, reject.QuoteRemaining.IsZero(), "and never the quote")
}
