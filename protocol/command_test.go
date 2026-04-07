package protocol

import (
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
				SeqID:     123,
				CommandID: "cmd-place",
				UserID:    789,
				MarketID:  "BTC-USDT",
				Timestamp: 1678901234,
			},
			OrderID:   "order-1",
			Side:      SideBuy,
			OrderType: OrderTypeLimit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		},
	},
	{
		name: "CancelOrderRequest",
		req: &CancelOrderRequest{
			BaseCommand: BaseCommand{
				SeqID:     124,
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
				SeqID:     125,
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
		name: "CreateMarketRequest",
		req: &CreateMarketRequest{
			BaseCommand: BaseCommand{
				SeqID:     126,
				CommandID: "cmd-create",
				UserID:    1,
				MarketID:  "ETH-USDT",
				Timestamp: 1678901237,
			},
			MinLotSize: udecimal.MustFromInt64(1, 2),
		},
	},
	{
		name: "SuspendMarketRequest",
		req: &SuspendMarketRequest{
			BaseCommand: BaseCommand{
				SeqID:     127,
				CommandID: "cmd-suspend",
				UserID:    1,
				MarketID:  "ETH-USDT",
				Timestamp: 1678901238,
			},
			Reason: "maintenance",
		},
	},
	{
		name: "ResumeMarketRequest",
		req: &ResumeMarketRequest{
			BaseCommand: BaseCommand{
				SeqID:     128,
				CommandID: "cmd-resume",
				UserID:    1,
				MarketID:  "ETH-USDT",
				Timestamp: 1678901239,
			},
		},
	},
	{
		name: "UpdateConfigRequest",
		req: &UpdateConfigRequest{
			BaseCommand: BaseCommand{
				SeqID:     129,
				CommandID: "cmd-update",
				UserID:    1,
				MarketID:  "ETH-USDT",
				Timestamp: 1678901240,
			},
			MinLotSize: udecimal.MustFromInt64(1, 3),
		},
	},
	{
		name: "UserEventRequest",
		req: &UserEventRequest{
			BaseCommand: BaseCommand{
				SeqID:     130,
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
			name: "CreateMarketRequest",
			req: &CreateMarketRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-3",
					UserID:    1,
					MarketID:  "ETH-USDT",
					Timestamp: 1,
				},
			},
		},
		{
			name: "SuspendMarketRequest",
			req: &SuspendMarketRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-4",
					UserID:    1,
					MarketID:  "ETH-USDT",
					Timestamp: 1,
				},
				Reason: "maintenance",
			},
		},
		{
			name: "ResumeMarketRequest",
			req: &ResumeMarketRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-5",
					UserID:    1,
					MarketID:  "ETH-USDT",
					Timestamp: 1,
				},
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
			case *CreateMarketRequest:
				require.IsType(t, &CreateMarketRequest{}, decoded)
			case *SuspendMarketRequest:
				require.IsType(t, &SuspendMarketRequest{}, decoded)
			case *ResumeMarketRequest:
				require.IsType(t, &ResumeMarketRequest{}, decoded)
			case *UserEventRequest:
				require.IsType(t, &UserEventRequest{}, decoded)
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
			name: "SuspendMarketRequest oversized Reason",
			req: &SuspendMarketRequest{
				BaseCommand: BaseCommand{
					CommandID: "cmd-1",
					UserID:    1,
					MarketID:  "BTC-USDT",
					Timestamp: 1,
				},
				Reason: oversized,
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
	// version + userID + type + seqID + timestamp, plus partial strings).
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
	// Build a valid ResumeMarket frame (smallest payload), then overwrite the
	// command-type byte with an unrecognized value.
	validReq := &ResumeMarketRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-unk",
			UserID:    1,
			MarketID:  "X",
			Timestamp: 1,
		},
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
	validReq := &ResumeMarketRequest{
		BaseCommand: BaseCommand{
			CommandID: "cmd-ts",
			UserID:    1,
			MarketID:  "X",
			Timestamp: 1,
		},
	}

	data, err := MarshalRequest(validReq)
	require.NoError(t, err)

	// Timestamp bytes are at offset 18 (1 version + 8 userID + 1 type + 8 seqID).
	const timestampOffset = 18
	// Set the most-significant bit to make the value exceed maxInt64Value.
	data[timestampOffset] = 0x80

	_, err = UnmarshalRequest(data)
	require.ErrorIs(t, err, errInvalidRequest)
}
