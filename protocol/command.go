package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

// CommandType defines the type of the command (using uint8 for memory alignment and performance).
type CommandType uint8

// Command Type Numbering Strategy:
// - 0-50:  OrderBook Management Commands (internal, low-frequency admin operations)
// - 51+:   Trading Commands (external, high-frequency hot path).
const (
	// CmdUnknown is the default unknown command.
	CmdUnknown CommandType = 0
	// CmdCreateMarket creates a new market.
	CmdCreateMarket CommandType = 1
	// CmdSuspendMarket suspends a market.
	CmdSuspendMarket CommandType = 2
	// CmdResumeMarket resumes a suspended market.
	CmdResumeMarket CommandType = 3
	// CmdUpdateConfig updates market configuration.
	CmdUpdateConfig CommandType = 4

	// CmdPlaceOrder places a new order.
	CmdPlaceOrder CommandType = 51
	// CmdCancelOrder cancels an existing order.
	CmdCancelOrder CommandType = 52
	// CmdAmendOrder modifies an existing order.
	CmdAmendOrder CommandType = 53
	// CmdUserEvent is a generic user event.
	CmdUserEvent CommandType = 100 // Generic User Event (e.g., EndOfBlock, Audit)
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

const (
	minPlaceOrderSize  = 18
	minCancelOrderSize = 16
	minAmendOrderSize  = 16
	stringLenSize      = 2
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

	// CommandID is the original identifier provided by the submitter.
	CommandID string `json:"command_id"`

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

// MarshalBinary serializes the PlaceOrderCommand to binary format.
func (c *PlaceOrderCommand) MarshalBinary(buf []byte) (int, error) {
	offset := 0
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	buf[offset] = uint8(c.Side)
	offset++
	buf[offset] = c.OrderType.ToUint8()
	offset++

	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.Price)
	offset += writeString(buf[offset:], c.Size)
	offset += writeString(buf[offset:], c.VisibleSize)
	offset += writeString(buf[offset:], c.QuoteSize)

	return offset, nil
}

// BinarySize returns the size of the command in binary format.
func (c *PlaceOrderCommand) BinarySize() int {
	return 8 + 8 + 1 + 1 + 2*5 + len(c.OrderID) + len(c.Price) + len(c.Size) + len(c.VisibleSize) + len(c.QuoteSize)
}

// UnmarshalBinary deserializes the PlaceOrderCommand from binary format.
func (c *PlaceOrderCommand) UnmarshalBinary(data []byte) (int, error) {
	if len(data) > 0 && data[0] == '{' {
		return 0, errors.New("looks like JSON")
	}
	if len(data) < minPlaceOrderSize {
		return 0, io.ErrUnexpectedEOF
	}
	offset := 0
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:])) //nolint:gosec
	offset += 8
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	c.Side = Side(data[offset])
	offset++
	c.OrderType = OrderTypeFromUint8(data[offset])
	offset++

	var n int
	c.OrderID, n = readString(data[offset:])
	offset += n
	c.Price, n = readString(data[offset:])
	offset += n
	c.Size, n = readString(data[offset:])
	offset += n
	c.VisibleSize, n = readString(data[offset:])
	offset += n
	c.QuoteSize, n = readString(data[offset:])
	_ = n // ignore last assignment

	return offset + n, nil
}

// CancelOrderCommand is the payload for canceling an existing order.
type CancelOrderCommand struct {
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

// MarshalBinary serializes the CancelOrderCommand to binary format.
func (c *CancelOrderCommand) MarshalBinary(buf []byte) (int, error) {
	offset := 0
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	offset += writeString(buf[offset:], c.OrderID)
	return offset, nil
}

// BinarySize returns the size of the command in binary format.
func (c *CancelOrderCommand) BinarySize() int {
	return 8 + 8 + 2 + len(c.OrderID)
}

// UnmarshalBinary deserializes the CancelOrderCommand from binary format.
func (c *CancelOrderCommand) UnmarshalBinary(data []byte) (int, error) {
	if len(data) > 0 && data[0] == '{' {
		return 0, errors.New("looks like JSON")
	}
	if len(data) < minCancelOrderSize {
		return 0, io.ErrUnexpectedEOF
	}
	offset := 0
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:])) //nolint:gosec
	offset += 8
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	var n int
	c.OrderID, n = readString(data[offset:])
	_ = n

	return offset + n, nil
}

// AmendOrderCommand is the payload for modifying an existing order.
type AmendOrderCommand struct {
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
	NewPrice  string `json:"new_price"`
	NewSize   string `json:"new_size"`
	Timestamp int64  `json:"timestamp"`
}

// MarshalBinary serializes the AmendOrderCommand to binary format.
func (c *AmendOrderCommand) MarshalBinary(buf []byte) (int, error) {
	offset := 0
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.NewPrice)
	offset += writeString(buf[offset:], c.NewSize)
	return offset, nil
}

// BinarySize returns the size of the command in binary format.
func (c *AmendOrderCommand) BinarySize() int {
	return 8 + 8 + 2*3 + len(c.OrderID) + len(c.NewPrice) + len(c.NewSize)
}

// UnmarshalBinary deserializes the AmendOrderCommand from binary format.
func (c *AmendOrderCommand) UnmarshalBinary(data []byte) (int, error) {
	if len(data) > 0 && data[0] == '{' {
		return 0, errors.New("looks like JSON")
	}
	if len(data) < minCancelOrderSize {
		return 0, io.ErrUnexpectedEOF
	}
	offset := 0
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:])) //nolint:gosec
	offset += 8
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	var n int
	c.OrderID, n = readString(data[offset:])
	offset += n
	c.NewPrice, n = readString(data[offset:])
	offset += n
	c.NewSize, n = readString(data[offset:])
	_ = n

	return offset + n, nil
}

func writeString(buf []byte, s string) int {
	l := len(s)
	/* #nosec G115 */
	binary.BigEndian.PutUint16(buf, uint16(l))
	copy(buf[stringLenSize:], s)
	return stringLenSize + l
}

func readString(data []byte) (string, int) {
	if len(data) < stringLenSize {
		return "", 0
	}
	l := int(binary.BigEndian.Uint16(data))
	if len(data) < stringLenSize+l {
		return "", 0
	}
	return string(data[stringLenSize : stringLenSize+l]), stringLenSize + l
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

// UserEventCommand is the payload for generic user events.
type UserEventCommand struct {
	UserID    uint64 `json:"user_id"`    // Operator ID for audit trail
	EventType string `json:"event_type"` // e.g. "EndOfBlock", "AdminMarker"
	Key       string `json:"key,omitempty"`
	Data      []byte `json:"data,omitempty"`
}
