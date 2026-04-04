package protocol

// DepthItem represents a single price level in the order book depth.
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
	// SideBuy represents the buy side.
	SideBuy Side = 1
	// SideSell represents the sell side.
	SideSell Side = 2
)

// OrderType represents the type of order.
type OrderType string

const (
	// OrderTypeMarket represents a market order.
	OrderTypeMarket OrderType = "market"
	// OrderTypeLimit represents a limit order.
	OrderTypeLimit OrderType = "limit"
	// OrderTypeFOK represents a Fill-Or-Kill order.
	OrderTypeFOK OrderType = "fok" // Fill Or Kill
	// OrderTypeIOC represents an Immediate-Or-Cancel order.
	OrderTypeIOC OrderType = "ioc" // Immediate Or Cancel
	// OrderTypePostOnly represents a post-only (maker-only) order.
	OrderTypePostOnly OrderType = "post_only" // Maker only
	// OrderTypeCancel represents a cancellation request.
	OrderTypeCancel OrderType = "cancel" // The order has been canceled
)

const (
	// OrderTypeUnknownUint8 represents an unknown order type in binary format.
	OrderTypeUnknownUint8 uint8 = 0
	// OrderTypeMarketUint8 represents a market order in binary format.
	OrderTypeMarketUint8 uint8 = 1
	// OrderTypeLimitUint8 represents a limit order in binary format.
	OrderTypeLimitUint8 uint8 = 2
	// OrderTypeFOKUint8 represents a Fill-Or-Kill order in binary format.
	OrderTypeFOKUint8 uint8 = 3
	// OrderTypeIOCUint8 represents an Immediate-Or-Cancel order in binary format.
	OrderTypeIOCUint8 uint8 = 4
	// OrderTypePostUint8 represents a post-only order in binary format.
	OrderTypePostUint8 uint8 = 5
	// OrderTypeCancelUint8 represents a cancellation request in binary format.
	OrderTypeCancelUint8 uint8 = 6
)

// ToUint8 converts the OrderType to its binary representation.
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

// OrderTypeFromUint8 converts the binary representation to an OrderType.
func OrderTypeFromUint8(v uint8) OrderType {
	switch v {
	case OrderTypeMarketUint8:
		return OrderTypeMarket
	case OrderTypeFOKUint8:
		return OrderTypeFOK
	case OrderTypeIOCUint8:
		return OrderTypeIOC
	case OrderTypePostUint8:
		return OrderTypePostOnly
	case OrderTypeCancelUint8:
		return OrderTypeCancel
	case OrderTypeLimitUint8:
		fallthrough
	default:
		return OrderTypeLimit // Default to limit
	}
}

// LogType represents the type of event log.
type LogType string

const (
	// LogTypeOpen is generated when an order is opened.
	LogTypeOpen LogType = "open"
	// LogTypeMatch is generated when a trade match occurs.
	LogTypeMatch LogType = "match"
	// LogTypeCancel is generated when an order is canceled.
	LogTypeCancel LogType = "cancel"
	// LogTypeAmend is generated when an order is amended.
	LogTypeAmend LogType = "amend"
	// LogTypeReject is generated when an order or command is rejected.
	LogTypeReject LogType = "reject"
	// LogTypeUser is generated for generic user events.
	LogTypeUser LogType = "user_event"
	// LogTypeAdmin is generated for successful management commands.
	LogTypeAdmin LogType = "admin_event"
)

// RejectReason represents the reason why an order was rejected.
type RejectReason string

const (
	// RejectReasonNone indicates no rejection.
	RejectReasonNone RejectReason = ""
	// RejectReasonNoLiquidity indicates no orders available to match.
	RejectReasonNoLiquidity RejectReason = "no_liquidity" // Market/IOC/FOK: No orders available to match
	// RejectReasonPriceMismatch indicates price does not meet requirements.
	RejectReasonPriceMismatch RejectReason = "price_mismatch" // IOC/FOK: Price does not meet requirements
	// RejectReasonInsufficientSize indicates order cannot be fully filled.
	RejectReasonInsufficientSize RejectReason = "insufficient_size" // FOK: Cannot be fully filled
	// RejectReasonPostOnlyMatch indicates PostOnly order would match immediately.
	RejectReasonPostOnlyMatch RejectReason = "post_only_match" // PostOnly: Would match immediately
	// RejectReasonDuplicateID indicates duplicate order ID.
	RejectReasonDuplicateID RejectReason = "duplicate_order_id"
	// RejectReasonOrderNotFound indicates order not found for cancel/amend.
	RejectReasonOrderNotFound RejectReason = "order_not_found"
	// RejectReasonInvalidPayload indicates invalid command payload.
	RejectReasonInvalidPayload RejectReason = "invalid_payload"

	// RejectReasonMarketNotFound is returned when the target market does not exist.
	RejectReasonMarketNotFound RejectReason = "market_not_found"
	// RejectReasonMarketAlreadyExists is returned when the market ID is already in use.
	RejectReasonMarketAlreadyExists RejectReason = "market_already_exists"
	// RejectReasonMarketSuspended is returned when the market is suspended.
	RejectReasonMarketSuspended RejectReason = "market_suspended"
	// RejectReasonMarketHalted is returned when the market is permanently halted.
	RejectReasonMarketHalted RejectReason = "market_halted"
	// RejectReasonUnauthorized is returned when the operator is not authorized.
	RejectReasonUnauthorized RejectReason = "unauthorized"
)
