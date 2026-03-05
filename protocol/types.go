package protocol

import (
	"github.com/bytedance/sonic"
)

// Serializer defines the contract for serializing and deserializing command payloads.
type Serializer interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

// DefaultJSONSerializer is a high-performance JSON serializer using the sonic library.
type DefaultJSONSerializer struct{}

func (s *DefaultJSONSerializer) Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

func (s *DefaultJSONSerializer) Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

// FastBinarySerializer implements the Serializer interface, prioritizing
// manual binary serialization for supported types with a JSON fallback.
type FastBinarySerializer struct {
	json DefaultJSONSerializer
}

func (s *FastBinarySerializer) Marshal(v any) ([]byte, error) {
	if marshaler, ok := v.(FastBinaryMarshaler); ok {
		size := marshaler.BinarySize()
		buf := make([]byte, size)
		n, err := marshaler.MarshalBinary(buf)
		if err != nil {
			return nil, err
		}
		return buf[:n], nil
	}
	return s.json.Marshal(v)
}

func (s *FastBinarySerializer) Unmarshal(data []byte, v any) error {
	if unmarshaler, ok := v.(FastBinaryUnmarshaler); ok {
		_, err := unmarshaler.UnmarshalBinary(data)
		if err == nil {
			return nil
		}
	}
	// Fallback to JSON if binary fails (e.g. for legacy JSON payloads in tests)
	return s.json.Unmarshal(data, v)
}

// FastBinaryMarshaler is implemented by types that can serialize themselves
// into a binary format efficiently.
type FastBinaryMarshaler interface {
	MarshalBinary(buf []byte) (int, error)
	BinarySize() int
}

// FastBinaryUnmarshaler is implemented by types that can deserialize themselves
// from a binary format efficiently.
type FastBinaryUnmarshaler interface {
	UnmarshalBinary(data []byte) (int, error)
}

type DepthItem struct {
	Price string `json:"price"`
	Size  string `json:"size"`
	Count int64  `json:"count"`
}

// GetDepthResponse represents the state of the order book depth.
type GetDepthResponse struct {
	UpdateID uint64       `json:"update_id"`
	Asks     []*DepthItem `json:"asks"`
	Bids     []*DepthItem `json:"bids"`
}

// GetStatsResponse contains statistics about the order book queues.
type GetStatsResponse struct {
	AskDepthCount int64 `json:"ask_depth_count"`
	AskOrderCount int64 `json:"ask_order_count"`
	BidDepthCount int64 `json:"bid_depth_count"`
	BidOrderCount int64 `json:"bid_order_count"`
}

// Side represents the order side (Buy/Sell).
type Side int8

const (
	SideBuy  Side = 1
	SideSell Side = 2
)

// OrderType represents the type of order.
type OrderType string

const (
	OrderTypeMarket   OrderType = "market"
	OrderTypeLimit    OrderType = "limit"
	OrderTypeFOK      OrderType = "fok"       // Fill Or Kill
	OrderTypeIOC      OrderType = "ioc"       // Immediate Or Cancel
	OrderTypePostOnly OrderType = "post_only" // Maker only
	OrderTypeCancel   OrderType = "cancel"    // The order has been canceled
)

const (
	OrderTypeUnknownUint8 uint8 = 0
	OrderTypeMarketUint8  uint8 = 1
	OrderTypeLimitUint8   uint8 = 2
	OrderTypeFOKUint8     uint8 = 3
	OrderTypeIOCUint8     uint8 = 4
	OrderTypePostUint8    uint8 = 5
	OrderTypeCancelUint8  uint8 = 6
)

func (ot OrderType) ToUint8() uint8 {
	switch ot {
	case OrderTypeMarket:
		return OrderTypeMarketUint8
	case OrderTypeLimit:
		return OrderTypeLimitUint8
	case OrderTypeFOK:
		return OrderTypeFOKUint8
	case OrderTypeIOC:
		return OrderTypeIOCUint8
	case OrderTypePostOnly:
		return OrderTypePostUint8
	case OrderTypeCancel:
		return OrderTypeCancelUint8
	default:
		return OrderTypeUnknownUint8
	}
}

func OrderTypeFromUint8(v uint8) OrderType {
	switch v {
	case OrderTypeMarketUint8:
		return OrderTypeMarket
	case OrderTypeLimitUint8:
		return OrderTypeLimit
	case OrderTypeFOKUint8:
		return OrderTypeFOK
	case OrderTypeIOCUint8:
		return OrderTypeIOC
	case OrderTypePostUint8:
		return OrderTypePostOnly
	case OrderTypeCancelUint8:
		return OrderTypeCancel
	default:
		return OrderTypeLimit // Default to limit
	}
}

// LogType represents the type of event log.
type LogType string

const (
	LogTypeOpen   LogType = "open"
	LogTypeMatch  LogType = "match"
	LogTypeCancel LogType = "cancel"
	LogTypeAmend  LogType = "amend"
	LogTypeReject LogType = "reject"
	LogTypeUser   LogType = "user_event"
)

// RejectReason represents the reason why an order was rejected.
type RejectReason string

const (
	RejectReasonNone             RejectReason = ""
	RejectReasonNoLiquidity      RejectReason = "no_liquidity"      // Market/IOC/FOK: No orders available to match
	RejectReasonPriceMismatch    RejectReason = "price_mismatch"    // IOC/FOK: Price does not meet requirements
	RejectReasonInsufficientSize RejectReason = "insufficient_size" // FOK: Cannot be fully filled
	RejectReasonPostOnlyMatch    RejectReason = "post_only_match"   // PostOnly: Would match immediately
	RejectReasonDuplicateID      RejectReason = "duplicate_order_id"
	RejectReasonOrderNotFound    RejectReason = "order_not_found"
	RejectReasonInvalidPayload   RejectReason = "invalid_payload"

	// Market Management Reject Reasons
	RejectReasonMarketNotFound      RejectReason = "market_not_found"      // Target market does not exist
	RejectReasonMarketAlreadyExists RejectReason = "market_already_exists" // Market ID already in use
	RejectReasonMarketSuspended     RejectReason = "market_suspended"      // Market is suspended, trading not allowed
	RejectReasonMarketHalted        RejectReason = "market_halted"         // Market is permanently halted
	RejectReasonUnauthorized        RejectReason = "unauthorized"          // Operator not authorized for this action
)
