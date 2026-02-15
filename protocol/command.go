package protocol

// CommandType defines the type of the command (using uint8 for memory alignment and performance)
type CommandType uint8

// Command Type Numbering Strategy:
// - 0-50:  OrderBook Management Commands (internal, low-frequency admin operations)
// - 51+:   Trading Commands (external, high-frequency hot path)
const (
	// OrderBook Management Commands (0-50, internal use)
	CmdUnknown       CommandType = 0
	CmdCreateMarket  CommandType = 1
	CmdSuspendMarket CommandType = 2
	CmdResumeMarket  CommandType = 3
	CmdUpdateConfig  CommandType = 4

	// Trading Commands (51+, external use)
	CmdPlaceOrder  CommandType = 51
	CmdCancelOrder CommandType = 52
	CmdAmendOrder  CommandType = 53
)

// OrderBookState represents the lifecycle state of an order book.
type OrderBookState uint8

const (
	// OrderBookStateRunning indicates the order book is active and accepting all trading operations.
	OrderBookStateRunning OrderBookState = 0
	// OrderBookStateSuspended indicates the order book is temporarily paused; only cancel operations are allowed.
	OrderBookStateSuspended OrderBookState = 1
	// OrderBookStateHalted indicates the order book is permanently stopped; no operations are allowed.
	OrderBookStateHalted OrderBookState = 2
)

// Command is the standard carrier for commands entering the Matching Engine.
// It is designed to be efficient for serialization and compatible with Event Sourcing.
type Command struct {
	// Version is the protocol version for backward compatibility.
	Version uint8 `json:"version"`

	// MarketID is the target market for this command (Routing Header).
	MarketID string `json:"market_id"`

	// SeqID is used for global ordering and deduplication.
	SeqID uint64 `json:"seq_id"`

	// Type identifies the payload type for fast routing.
	Type CommandType `json:"type"`

	// Payload contains the serialized business data (e.g., JSON bytes of PlaceOrderCommand).
	// We use lazy deserialization to optimize routing performance.
	Payload []byte `json:"payload"`

	// Metadata stores non-business context (e.g., Tracing ID, Source IP).
	Metadata map[string]string `json:"metadata,omitempty"`
}

// PlaceOrderCommand is the payload for placing a new order.
type PlaceOrderCommand struct {
	OrderID     string    `json:"order_id"`
	Side        Side      `json:"side"`
	OrderType   OrderType `json:"order_type"`
	Price       string    `json:"price"` // Using string to prevent precision loss in JSON
	Size        string    `json:"size"`
	VisibleSize string    `json:"visible_size,omitempty"`
	QuoteSize   string    `json:"quote_size,omitempty"`
	UserID      uint64    `json:"user_id"`
	Timestamp   int64     `json:"timestamp"`
}

// CancelOrderCommand is the payload for cancelling an existing order.
type CancelOrderCommand struct {
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

// AmendOrderCommand is the payload for modifying an existing order.
type AmendOrderCommand struct {
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
	NewPrice  string `json:"new_price"`
	NewSize   string `json:"new_size"`
	Timestamp int64  `json:"timestamp"`
}

// GetDepthRequest is the payload for querying order book depth.
// This is used for synchronous queries via gRPC/HTTP, separate from the async Command stream.
type GetDepthRequest struct {
	MarketID string `json:"market_id"`
	Limit    uint32 `json:"limit"`
}

// GetStatsRequest is the payload for querying order book statistics.
type GetStatsRequest struct {
	MarketID string `json:"market_id"`
}

// CreateMarketCommand is the payload for creating a new market/order book.
type CreateMarketCommand struct {
	UserID     string `json:"user_id"`      // Operator ID for audit trail
	MarketID   string `json:"market_id"`    // Unique market identifier
	MinLotSize string `json:"min_lot_size"` // Minimum trade unit (e.g., "0.00000001")
}

// SuspendMarketCommand is the payload for suspending a market.
type SuspendMarketCommand struct {
	UserID   string `json:"user_id"`   // Operator ID for audit trail
	MarketID string `json:"market_id"` // Target market to suspend
	Reason   string `json:"reason"`    // Reason for suspension (for audit)
}

// ResumeMarketCommand is the payload for resuming a suspended market.
type ResumeMarketCommand struct {
	UserID   string `json:"user_id"`   // Operator ID for audit trail
	MarketID string `json:"market_id"` // Target market to resume
}

// UpdateConfigCommand is the payload for updating market configuration.
type UpdateConfigCommand struct {
	UserID     string `json:"user_id"`                // Operator ID for audit trail
	MarketID   string `json:"market_id"`              // Target market
	MinLotSize string `json:"min_lot_size,omitempty"` // New minimum lot size (optional)
}
