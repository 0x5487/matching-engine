package protocol

// CommandType defines the type of the command (using uint8 for memory alignment and performance)
type CommandType uint8

const (
	CmdUnknown     CommandType = 0
	CmdPlaceOrder  CommandType = 1
	CmdCancelOrder CommandType = 2
	CmdAmendOrder  CommandType = 3
	// CmdDepth       CommandType = 4 // Future use
	// CmdSnapshot    CommandType = 5 // Future use
)

// Command is the standard carrier for commands entering the Matching Engine.
// It is designed to be efficient for serialization and compatible with Event Sourcing.
type Command struct {
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
	MarketID  string `json:"market_id"`
	OrderID   string `json:"order_id"`
	Side      int8   `json:"side"` // 1: Buy, 2: Sell
	OrderType string `json:"order_type"`
	Price     string `json:"price"` // Using string to prevent precision loss in JSON
	Size      string `json:"size"`
	UserID    int64  `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

// CancelOrderCommand is the payload for cancelling an existing order.
type CancelOrderCommand struct {
	MarketID  string `json:"market_id"`
	OrderID   string `json:"order_id"`
	UserID    int64  `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

// AmendOrderCommand is the payload for modifying an existing order.
type AmendOrderCommand struct {
	MarketID  string `json:"market_id"`
	OrderID   string `json:"order_id"`
	UserID    int64  `json:"user_id"`
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
