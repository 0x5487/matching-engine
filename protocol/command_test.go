package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"
)

// happyPathRequests contains one valid instance of every supported request type.
var happyPathRequests = []struct {
	name string
	req  any
}{
	{
		name: "PlaceOrderRequest",
		req: &PlaceOrderRequest{
			BaseCommand: BaseCommand{
				CommandID: "cmd-place",
				UserID:    789,
				MarketID:  "BTC-USDT",
				Timestamp: 1678901234,
			},
			OrderID:     "order-1",
			Side:        SideBuy,
			OrderType:   OrderTypeLimit,
			Price:       udecimal.MustFromInt64(100, 0),
			Size:        udecimal.MustFromInt64(1, 0),
			VisibleSize: udecimal.MustParse("0.5"),
			QuoteSize:   udecimal.MustParse("50"),
			// Every field is deliberately non-zero: a field the wire drops
			// round-trips as zero, and a zero fixture cannot tell the
			// difference. StepSize was dropped this way until this test
			// carried a non-zero one.
			StepSize: udecimal.MustParse("0.0001"),
			// The routing travels too: a field the codec drops would round-trip as zero, as StepSize once did.
			OmsShard:     7,
			MakerFeeRate: udecimal.MustParse("0.0005"),
			TakerFeeRate: udecimal.MustParse("0.0012"),
			BaseAsset:    "BTC",
			QuoteAsset:   "USDT",
		},
	},
	{
		name: "CancelOrderRequest",
		req: &CancelOrderRequest{
			BaseCommand: BaseCommand{
				CommandID: "cmd-cancel",
				UserID:    789,
				MarketID:  "BTC-USDT",
				Timestamp: 1678901235,
			},
			OrderID: "order-1",
		},
	},
	{
		name: "AmendOrderRequest",
		req: &AmendOrderRequest{
			BaseCommand: BaseCommand{
				CommandID: "cmd-amend",
				UserID:    789,
				MarketID:  "BTC-USDT",
				Timestamp: 1678901236,
			},
			OrderID:  "order-1",
			NewPrice: udecimal.MustFromInt64(101, 0),
			NewSize:  udecimal.MustFromInt64(2, 0),
		},
	},
	{
		name: "UserEventRequest",
		req: &UserEventRequest{
			BaseCommand: BaseCommand{
				CommandID: "cmd-user-event",
				UserID:    99,
				MarketID:  "ETH-USDT",
				Timestamp: 1678901241,
			},
			EventType: "EndOfBlock",
			Key:       "block-123",
			Data:      []byte("some-data"),
		},
	},
}

// TestMarshalUnmarshalRequest verifies happy-path round trips for every request type.
func TestMarshalUnmarshalRequest(t *testing.T) {
	for _, tt := range happyPathRequests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalRequest(tt.req)
			require.NoError(t, err)

			decoded, err := UnmarshalRequest(data)
			require.NoError(t, err)
			require.Equal(t, tt.req, decoded, "round-trip failed for "+tt.name)
		})
	}
}

// TestMarshalRequest_DerivedWireType verifies that MarshalRequest always writes the
// correct CommandType on the wire, and that UnmarshalRequest reconstructs the
// correct concrete Go type. This guards against the MQ-dispatch bug described
// in code-review issue #2.
func TestMarshalRequest_DerivedWireType(t *testing.T) {
	tests := []struct {
		name         string
		req          any
		wantWireType CommandType
	}{
		{
			name: "PlaceOrderRequest",
			req: &PlaceOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID:   "o1",
				Side:      SideBuy,
				OrderType: OrderTypeLimit,
			},
		},
		{
			name: "CancelOrderRequest",
			req: &CancelOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-2",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID: "o1",
			},
		},
		{
			name: "UserEventRequest",
			req: &UserEventRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-6",
					UserID:    1,
					Timestamp: 1,
				},
				EventType: "EndOfBlock",
				Key:       "block-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalRequest(tt.req)
			require.NoError(t, err, "MarshalRequest must not fail for valid request")

			decoded, err := UnmarshalRequest(data)
			require.NoError(t, err, "UnmarshalRequest must not fail on well-formed bytes")

			// Verify the decoded type matches the original type.
			switch tt.req.(type) {
			case *PlaceOrderRequest:
				require.IsType(t, &PlaceOrderRequest{}, decoded)
			case *CancelOrderRequest:
				require.IsType(t, &CancelOrderRequest{}, decoded)
			case *UserEventRequest:
				require.IsType(t, &UserEventRequest{}, decoded)
			default:
				t.Fatalf("unexpected request type: %T", tt.req)
			}
		})
	}
}

// TestMarshalRequest_InvalidInputs verifies that MarshalRequest returns errors
// (not panics) for oversized body string fields, and for other invalid inputs.
// This guards against the panic-on-large-string bug described in code-review issue #1.
func TestMarshalRequest_InvalidInputs(t *testing.T) {
	oversized := strings.Repeat("x", maxUint16Value+1)

	tests := []struct {
		name    string
		req     any
		wantErr error
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: errUnknownRequest,
		},
		{
			name:    "unknown request type",
			req:     struct{ Foo string }{"bar"},
			wantErr: errUnknownRequest,
		},
		{
			name: "negative timestamp",
			req: &PlaceOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: -1,
				},
				OrderID:   "o1",
				Side:      SideBuy,
				OrderType: OrderTypeLimit,
			},
			wantErr: errInvalidRequest,
		},
		{
			name: "oversized MarketID in header",
			req: &PlaceOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  oversized,
					Timestamp: 1,
				},
				OrderID:   "o1",
				Side:      SideBuy,
				OrderType: OrderTypeLimit,
			},
			wantErr: errStringTooLong,
		},
		{
			name: "oversized CommandID in header",
			req: &PlaceOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: oversized,
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID:   "o1",
				Side:      SideBuy,
				OrderType: OrderTypeLimit,
			},
			wantErr: errStringTooLong,
		},
		// --- Body string field oversize tests (issue #1 fixes) ---
		{
			name: "PlaceOrderRequest oversized OrderID",
			req: &PlaceOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID:   oversized,
				Side:      SideBuy,
				OrderType: OrderTypeLimit,
			},
			wantErr: errStringTooLong,
		},
		{
			name: "CancelOrderRequest oversized OrderID",
			req: &CancelOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID: oversized,
			},
			wantErr: errStringTooLong,
		},
		{
			name: "AmendOrderRequest oversized OrderID",
			req: &AmendOrderRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				OrderID: oversized,
			},
			wantErr: errStringTooLong,
		},
		{
			name: "UserEventRequest oversized EventType",
			req: &UserEventRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					Timestamp: 1,
				},
				EventType: oversized,
				Key:       "k",
			},
			wantErr: errStringTooLong,
		},
		{
			name: "UserEventRequest oversized Key",
			req: &UserEventRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					Timestamp: 1,
				},
				EventType: "EndOfBlock",
				Key:       oversized,
			},
			wantErr: errStringTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalRequest(tt.req)
			require.ErrorIs(t, err, tt.wantErr,
				"MarshalRequest must return %v, got %v", tt.wantErr, err)
		})
	}
}

// TestUnmarshalRequest_TruncatedPayloads verifies that UnmarshalRequest returns
// io.ErrUnexpectedEOF (not a panic or nil error) when the binary data is truncated.
// This guards against the lost negative-path coverage described in issue #3.
func TestUnmarshalRequest_TruncatedPayloads(t *testing.T) {
	// Build a valid wire frame first, then truncate it at various offsets.
	validReq := &PlaceOrderRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-trunc",
			UserID:    1,
			MarketID:  "BTC-USDT",
			Timestamp: 1,
		},
		OrderID:   "order-1",
		Side:      SideBuy,
		OrderType: OrderTypeLimit,
		Price:     udecimal.MustFromInt64(100, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}

	full, err := MarshalRequest(validReq)
	require.NoError(t, err)

	// Test every prefix truncation in the header region (first 32 bytes covers
	// version + userID + type + timestamp, plus partial strings).
	for i := range len(full) - 1 {
		truncated := full[:i]
		_, err := UnmarshalRequest(truncated)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF,
			"expected ErrUnexpectedEOF for %d-byte truncation, got: %v", i, err)
	}
}

// TestUnmarshalRequest_UnknownCommandType verifies that UnmarshalRequest returns
// errUnknownRequest when the wire command type is not recognized.
func TestUnmarshalRequest_UnknownCommandType(t *testing.T) {
	// Build a valid CancelOrder frame (smallest payload), then overwrite the
	// command-type byte with an unrecognized value.
	validReq := &CancelOrderRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-unk",
			UserID:    1,
			MarketID:  "X",
			Timestamp: 1,
		},
		OrderID: "o",
	}

	data, err := MarshalRequest(validReq)
	require.NoError(t, err)

	// The command-type byte sits at offset 9 (1 version + 8 userID).
	const cmdTypeOffset = 9
	data[cmdTypeOffset] = 0xFF // unrecognized type

	_, err = UnmarshalRequest(data)
	require.ErrorIs(t, err, errUnknownRequest)
}

// TestUnmarshalRequest_InvalidTimestamp verifies that UnmarshalRequest rejects
// timestamps that would overflow int64 (bit 63 set in the uint64 wire value).
func TestUnmarshalRequest_InvalidTimestamp(t *testing.T) {
	validReq := &CancelOrderRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-ts",
			UserID:    1,
			MarketID:  "X",
			Timestamp: 1,
		},
		OrderID: "o",
	}

	data, err := MarshalRequest(validReq)
	require.NoError(t, err)

	// Timestamp bytes are at offset 10 (1 version + 8 userID + 1 type).
	const timestampOffset = 10
	// Set the most-significant bit to make the value exceed maxInt64Value.
	data[timestampOffset] = 0x80

	_, err = UnmarshalRequest(data)
	require.ErrorIs(t, err, errInvalidRequest)
}

// TestPlaceOrderRequest_MarketProtectionPriceRoundTrip pins that a market order's
// protection price survives both encodings. The binary check is made on the raw
// bytes: the price must appear as its length-prefixed string, so a dropped or
// reordered field fails here instead of decoding to a plausible zero.
func TestPlaceOrderRequest_MarketProtectionPriceRoundTrip(t *testing.T) {
	req := &PlaceOrderRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-market-protected",
			UserID:    789,
			MarketID:  "BTC-USDT",
			Timestamp: 1678901234,
		},
		OrderID:   "order-market",
		Side:      SideBuy,
		OrderType: OrderTypeMarket,
		Price:     udecimal.MustFromInt64(100, 0),
		QuoteSize: udecimal.MustParse("1000"),
		StepSize:  udecimal.MustParse("0.01"),
	}

	t.Run("binary", func(t *testing.T) {
		data, err := MarshalRequest(req)
		require.NoError(t, err)

		wantPrice := append([]byte{0, 3}, []byte("100")...)
		require.True(t, bytes.Contains(data, wantPrice),
			"the protection price must be on the wire as a length-prefixed string: % x", data)

		decoded, err := UnmarshalRequest(data)
		require.NoError(t, err)
		got, ok := decoded.(*PlaceOrderRequest)
		require.True(t, ok)
		require.Equal(t, "100", got.Price.String())
		require.Equal(t, req, got)
	})

	t.Run("json", func(t *testing.T) {
		// BaseCommand carries no json tags, which is the existing wire shape for every
		// request; this test only asserts on the Price field.
		data, err := json.Marshal(req) //nolint:musttag // see above
		require.NoError(t, err)

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &raw))
		require.Equal(t, "100", strings.Trim(string(raw["price"]), `"`), "the price field must be on the wire")

		var decoded PlaceOrderRequest
		require.NoError(t, json.Unmarshal(data, &decoded)) //nolint:musttag // see above
		require.Equal(t, "100", decoded.Price.String())
	})
}

// TestAmendOrderStepSizeRoundTrip pins the binary encoding of the amend step: the
// StepSize that goes in comes back out, zero included.
func TestAmendOrderStepSizeRoundTrip(t *testing.T) {
	t.Parallel()

	for _, step := range []string{"0", "0.0001", "1"} {
		in := &AmendOrderRequest{
			BaseCommand: BaseCommand{CommandID: "cmd-amend", UserID: 3, MarketID: "BTC-USDT", Timestamp: 99},
			OrderID:     "order-9",
			NewPrice:    udecimal.MustParse("101.5"),
			NewSize:     udecimal.MustParse("2.5"),
			StepSize:    udecimal.MustParse(step),
		}
		raw, err := MarshalRequest(in)
		require.NoError(t, err)
		out, err := UnmarshalRequest(raw)
		require.NoError(t, err)
		got, ok := out.(*AmendOrderRequest)
		require.True(t, ok)
		require.Equal(t, in.OrderID, got.OrderID)
		require.Equal(t, in.NewPrice.String(), got.NewPrice.String())
		require.Equal(t, in.NewSize.String(), got.NewSize.String())
		require.Equal(t, in.StepSize.String(), got.StepSize.String(), "step %s", step)
	}
}
