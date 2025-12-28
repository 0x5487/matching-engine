package match

import (
	"time"

	"github.com/quagmt/udecimal"
)

const (
	// EngineVersion is the current version of the matching engine
	EngineVersion = "v1.0.0"

	// SnapshotSchemaVersion is the current version of the snapshot schema
	// Increment this when the snapshot format changes in a backward-incompatible way
	SnapshotSchemaVersion = 1
)

type Side int8

const (
	Buy  Side = 1
	Sell Side = 2
)

type OrderType string

const (
	Market   OrderType = "market"
	Limit    OrderType = "limit"
	FOK      OrderType = "fok"       // Fill Or Kill
	IOC      OrderType = "ioc"       // Immediate Or Cancel
	PostOnly OrderType = "post_only" // be maker order only
	Cancel   OrderType = "cancel"    // the order has been canceled
)

// PlaceOrderCommand is the input command for placing an order.
// QuoteSize is only used for Market orders to specify amount in quote currency.
type PlaceOrderCommand struct {
	SeqID     uint64           `json:"seq_id"` // MQ sequence ID for snapshot/restore
	MarketID  string           `json:"market_id"`
	ID        string           `json:"id"`
	Side      Side             `json:"side"`
	Type      OrderType        `json:"type"`
	Price     udecimal.Decimal `json:"price"`                // Limit order price
	Size      udecimal.Decimal `json:"size"`                 // Base currency quantity (e.g., BTC)
	QuoteSize udecimal.Decimal `json:"quote_size,omitempty"` // Quote currency amount (e.g., USDT), only for Market orders
	UserID    int64            `json:"user_id"`
}

// Order represents the state of an order in the order book.
// This is the serializable state used for snapshots.
type Order struct {
	ID        string           `json:"id"`
	Side      Side             `json:"side"`
	Price     udecimal.Decimal `json:"price"`
	Size      udecimal.Decimal `json:"size"` // Remaining size
	Type      OrderType        `json:"type"`
	UserID    int64            `json:"user_id"`
	Timestamp int64            `json:"timestamp"` // Unix nano, creation time

	// Intrusive linked list pointers (ignored by JSON)
	next *Order
	prev *Order
}

type LogType string

const (
	LogTypeOpen   LogType = "open"
	LogTypeMatch  LogType = "match"
	LogTypeCancel LogType = "cancel"
	LogTypeAmend  LogType = "amend"
	LogTypeReject LogType = "reject"
)

// RejectReason represents the reason why an order was rejected.
type RejectReason string

const (
	RejectReasonNone             RejectReason = ""
	RejectReasonNoLiquidity      RejectReason = "no_liquidity"       // Market/IOC/FOK: No orders available to match
	RejectReasonPriceMismatch    RejectReason = "price_mismatch"     // IOC/FOK: Price does not meet requirements
	RejectReasonInsufficientSize RejectReason = "insufficient_size"  // FOK: Cannot be fully filled
	RejectReasonWouldCrossSpread RejectReason = "would_cross_spread" // PostOnly: Would match immediately
	RejectReasonDuplicateID      RejectReason = "duplicate_order_id"
	RejectReasonOrderNotFound    RejectReason = "order_not_found"
)

type Response struct {
	Error error
	Data  any
}

type OrderBookUpdateEvent struct {
	Bids []*UpdateEvent
	Asks []*UpdateEvent
	Time time.Time
}

type Depth struct {
	UpdateID uint64       `json:"update_id"`
	Asks     []*DepthItem `json:"asks"`
	Bids     []*DepthItem `json:"bids"`
}

type AmendOrderCommand struct {
	SeqID    uint64           `json:"seq_id"` // MQ sequence ID for snapshot/restore
	OrderID  string           `json:"order_id"`
	UserID   int64            `json:"user_id"`
	NewPrice udecimal.Decimal `json:"new_price"`
	NewSize  udecimal.Decimal `json:"new_size"`
}

type CancelOrderCommand struct {
	SeqID   uint64 `json:"seq_id"` // MQ sequence ID for snapshot/restore
	OrderID string `json:"order_id"`
	UserID  int64  `json:"user_id"`
}

// DepthChange represents a change in the order book depth.
type DepthChange struct {
	Side     Side
	Price    udecimal.Decimal
	SizeDiff udecimal.Decimal
}

// CommandType represents the type of command sent to the order book.
type CommandType int

const (
	CmdPlaceOrder CommandType = iota
	CmdCancelOrder
	CmdAmendOrder
	CmdDepth
	CmdGetStats
	CmdSnapshot
)

// BookStats contains statistics about the order book queues
type BookStats struct {
	AskDepthCount int64
	AskOrderCount int64
	BidDepthCount int64
	BidOrderCount int64
}

// Command represents a unified command sent to the order book.
// It improves deterministic ordering and performance by using a single channel.
// We use flattened fields to avoid 'any' boxing allocations.
type Command struct {
	SeqID uint64
	Type  CommandType

	// Place/Add Order fields
	MarketID  string
	OrderID   string
	Side      Side
	OrderType OrderType
	Price     udecimal.Decimal
	Size      udecimal.Decimal
	QuoteSize udecimal.Decimal
	UserID    int64

	// Amend Order fields
	NewPrice udecimal.Decimal
	NewSize  udecimal.Decimal

	// Query fields
	DepthLimit uint32

	Resp chan any // Optional: for synchronous response (e.g. CmdDepth)
}
