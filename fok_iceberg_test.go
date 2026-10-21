package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// fokRun sends a FOK buy or sell and returns the reject reason, if any, and the
// matched total. A FOK that fills returns no reject.
func fokRun(t *testing.T, h *dlHarness, id string, price, size int64) (protocol.RejectReason, string) {
	t.Helper()
	logs := h.apply(func(b *OrderBook) (*LogBatch, error) {
		return b.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: h.base(), OrderID: id, OrderType: FOK, Side: Buy,
			Price: udecimal.MustFromInt64(price, 0), Size: udecimal.MustFromInt64(size, 0),
			StepSize: udecimal.MustFromInt64(1, 0),
		})
	})
	filled := udecimal.Zero
	var reason protocol.RejectReason
	for _, l := range logs {
		switch l.Type {
		case protocol.LogTypeMatch:
			filled = filled.Add(l.Size)
		case protocol.LogTypeReject:
			reason = l.RejectReason
		default:
			// Opens and replenishes of the iceberg do not change the fill or the reason.
		}
	}
	return reason, filled.String()
}

// TestFOKCountsIcebergHiddenSize is the scenario from ticket 22: a sell iceberg at
// 100 with total 10 and visible 2. A buy FOK of 5 at 100 fills, because the reload
// supplies the hidden size. The same buy as a limit order fills 2+2+1.
func TestFOKCountsIcebergHiddenSize(t *testing.T) {
	t.Parallel()

	t.Run("fok of 5 fills through the iceberg hidden size", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2)
		reason, filled := fokRun(t, h, "fok", 100, 5)
		require.Empty(t, reason, "a fok the hidden size can fill must not be refused")
		require.Equal(t, "5", filled)
		ice, ok := h.book.findOrder("ice")
		require.True(t, ok)
		require.Equal(t, "5", ice.Size.Add(ice.HiddenSize).String())
	})

	t.Run("fok larger than visible plus hidden is refused with insufficient size", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2)
		reason, filled := fokRun(t, h, "fok", 100, 11)
		require.Equal(t, protocol.RejectReasonInsufficientSize, reason)
		require.Equal(t, "0", filled)
		require.Equal(t, "2", h.book.LevelSize(Sell, udecimal.MustParse("100")).String())
	})

	t.Run("fok spanning two levels counts both levels' hidden size", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2) // 10 at 100
		h.place("plain", Sell, 101, 4, 0)
		reason, filled := fokRun(t, h, "fok", 101, 14)
		require.Empty(t, reason)
		require.Equal(t, "14", filled)
		require.True(t, h.book.LevelSize(Sell, udecimal.MustParse("100")).IsZero())
	})

	t.Run("fok after the iceberg is canceled sees only what is left", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2)
		h.cancel("ice")
		reason, _ := fokRun(t, h, "fok", 100, 1)
		require.Equal(t, protocol.RejectReasonNoLiquidity, reason)
	})

	t.Run("fok after the iceberg is amended down into its hidden part", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2)
		h.amend("ice", 0, 4) // hidden 8 -> 2; visible stays 2 and the total is 4
		reason, _ := fokRun(t, h, "fok-5", 100, 5)
		require.Equal(t, protocol.RejectReasonInsufficientSize, reason,
			"the total is now 4, so 5 cannot fill")
		reason, filled := fokRun(t, h, "fok-4", 100, 4)
		require.Empty(t, reason)
		require.Equal(t, "4", filled)
	})

	t.Run("fok after restore from snapshot sees the hidden size", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 100, 10, 2)
		restored := NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog()))
		restored.Restore(h.book.Snapshot())
		h.book = restored
		reason, filled := fokRun(t, h, "fok", 100, 5)
		require.Empty(t, reason)
		require.Equal(t, "5", filled)
	})
}
