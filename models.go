package match

import (
	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

const (
	// EngineVersion is the current version of the matching engine
	EngineVersion = "v1.0.0"

	// SnapshotSchemaVersion is the current version of the snapshot schema
	// Increment this when the snapshot format changes in a backward-incompatible way
	SnapshotSchemaVersion = 1
)

type Side = protocol.Side

const (
	Buy  Side = protocol.SideBuy
	Sell Side = protocol.SideSell
)

type OrderType = protocol.OrderType

const (
	Market   OrderType = protocol.OrderTypeMarket
	Limit    OrderType = protocol.OrderTypeLimit
	FOK      OrderType = protocol.OrderTypeFOK
	IOC      OrderType = protocol.OrderTypeIOC
	PostOnly OrderType = protocol.OrderTypePostOnly
	Cancel   OrderType = protocol.OrderTypeCancel
)

// Order represents the state of an order in the order book.
// This is the serializable state used for snapshots.
type Order struct {
	ID        string           `json:"id"`
	Side      Side             `json:"side"`
	Price     udecimal.Decimal `json:"price"`
	Size      udecimal.Decimal `json:"size"` // Remaining visible size
	Type      OrderType        `json:"type"`
	UserID    uint64           `json:"user_id"`
	Timestamp int64            `json:"timestamp"` // Unix nano, creation time

	// Iceberg fields
	VisibleLimit udecimal.Decimal `json:"visible_limit,omitempty"`
	HiddenSize   udecimal.Decimal `json:"hidden_size,omitempty"`

	// Intrusive linked list pointers (ignored by JSON)
	next *Order
	prev *Order
}

// DepthChange represents a change in the order book depth.
type DepthChange struct {
	Side     Side
	Price    udecimal.Decimal
	SizeDiff udecimal.Decimal
}

// InputEvent is the internal wrapper for all events entering the OrderBook Actor.
type InputEvent struct {
	// Cmd is the external command carrier.
	Cmd *protocol.Command

	// Internal Query fields (Read Path)
	Query any // e.g. *protocol.GetDepthRequest
	Resp  chan any
}
