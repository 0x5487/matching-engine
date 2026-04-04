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

	// Timestamp is the logical timestamp of the command (UnixNano).
	Timestamp int64 `json:"timestamp"`

	// Payload contains the serialized business data (e.g., binary bytes of PlaceOrderParams).
	// We use lazy deserialization to optimize routing performance.
	Payload []byte `json:"payload"`
}

// BinarySize returns the size of the Command in binary format.
func (c *Command) BinarySize() int {
	// Version(1) + Type(1) + SeqID(8) + Timestamp(8) + MarketID(2+L) + CommandID(2+L) + Payload(4+L)
	return 1 + 1 + 8 + 8 + 2 + len(c.MarketID) + 2 + len(c.CommandID) + 4 + len(c.Payload)
}

// MarshalBinary serializes the Command (envelope) to binary format.
func (c *Command) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0

	buf[offset] = c.Version
	offset++
	buf[offset] = uint8(c.Type)
	offset++

	binary.BigEndian.PutUint64(buf[offset:], c.SeqID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8

	offset += writeString(buf[offset:], c.MarketID)
	offset += writeString(buf[offset:], c.CommandID)

	// Payload with 4-byte length prefix
	/* #nosec G115 */
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(c.Payload)))
	offset += 4
	copy(buf[offset:], c.Payload)

	return buf, nil
}

// UnmarshalBinary deserializes the Command (envelope) from binary format.
func (c *Command) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 18 { //nolint:mnd // Min size: Version(1)+Type(1)+SeqID(8)+Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0

	c.Version = data[offset]
	offset++
	c.Type = CommandType(data[offset])
	offset++

	c.SeqID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	var n int
	var err error
	c.MarketID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n

	c.CommandID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n

	if len(data[offset:]) < 4 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	payloadLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4

	if len(data[offset:]) < payloadLen {
		return io.ErrUnexpectedEOF
	}
	c.Payload = make([]byte, payloadLen)
	copy(c.Payload, data[offset:offset+payloadLen])

	return nil
}

// PlaceOrderParams is the payload for placing a new order.
type PlaceOrderParams struct {
	CommandID   string    `json:"-"`
	MarketID    string    `json:"-"`
	Timestamp   int64     `json:"-"`
	OrderID     string    `json:"order_id"`
	Side        Side      `json:"side"`
	OrderType   OrderType `json:"order_type"`
	Price       string    `json:"price"` // Using string to prevent precision loss in JSON
	Size        string    `json:"size"`
	VisibleSize string    `json:"visible_size,omitempty"`
	QuoteSize   string    `json:"quote_size,omitempty"`
	UserID      uint64    `json:"user_id"`
}

// MarshalBinary serializes the PlaceOrderParams to binary format.
func (c *PlaceOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0

	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	buf[offset] = uint8(c.Side)
	offset++
	buf[offset] = c.OrderType.ToUint8()
	offset++

	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8

	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.Price)
	offset += writeString(buf[offset:], c.Size)
	offset += writeString(buf[offset:], c.VisibleSize)
	writeString(buf[offset:], c.QuoteSize)

	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *PlaceOrderParams) BinarySize() int {
	return 8 + 1 + 1 + 8 + 2*5 + len(c.OrderID) + len(c.Price) + len(c.Size) + len(c.VisibleSize) + len(c.QuoteSize)
}

// UnmarshalBinary deserializes the PlaceOrderParams from binary format.
func (c *PlaceOrderParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 18 { //nolint:mnd // UserID(8) + Side(1) + OrderType(1) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	c.Side = Side(data[offset])
	offset++
	c.OrderType = OrderTypeFromUint8(data[offset])
	offset++

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	var n int
	var err error
	c.OrderID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.Price, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.Size, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.VisibleSize, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.QuoteSize, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n

	return nil
}

// CancelOrderParams is the payload for canceling an existing order.
type CancelOrderParams struct {
	CommandID string `json:"-"`
	MarketID  string `json:"-"`
	Timestamp int64  `json:"-"`
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
}

// MarshalBinary serializes the CancelOrderParams to binary format.
func (c *CancelOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	writeString(buf[offset:], c.OrderID)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *CancelOrderParams) BinarySize() int {
	return 8 + 8 + 2 + len(c.OrderID)
}

// UnmarshalBinary deserializes the CancelOrderParams from binary format.
func (c *CancelOrderParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.OrderID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

// AmendOrderParams is the payload for modifying an existing order.
type AmendOrderParams struct {
	CommandID string `json:"-"`
	MarketID  string `json:"-"`
	Timestamp int64  `json:"-"`
	OrderID   string `json:"order_id"`
	UserID    uint64 `json:"user_id"`
	NewPrice  string `json:"new_price"`
	NewSize   string `json:"new_size"`
}

// MarshalBinary serializes the AmendOrderParams to binary format.
func (c *AmendOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.NewPrice)
	writeString(buf[offset:], c.NewSize)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *AmendOrderParams) BinarySize() int {
	return 8 + 8 + 2*3 + len(c.OrderID) + len(c.NewPrice) + len(c.NewSize)
}

// UnmarshalBinary deserializes the AmendOrderParams from binary format.
func (c *AmendOrderParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.OrderID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.NewPrice, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.NewSize, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

func writeString(buf []byte, s string) int {
	l := len(s)
	/* #nosec G115 */
	binary.BigEndian.PutUint16(buf, uint16(l))
	copy(buf[stringLenSize:], s)
	return stringLenSize + l
}

func readString(data []byte) (string, int, error) {
	if len(data) < stringLenSize {
		return "", 0, io.ErrUnexpectedEOF
	}
	l := int(binary.BigEndian.Uint16(data))
	if len(data) < stringLenSize+l {
		return "", 0, io.ErrUnexpectedEOF
	}
	return string(data[stringLenSize : stringLenSize+l]), stringLenSize + l, nil
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

// CreateMarketParams is the payload for creating a new market/order book.
type CreateMarketParams struct {
	CommandID  string `json:"-"`
	MarketID   string `json:"-"`
	Timestamp  int64  `json:"-"`
	UserID     uint64 `json:"user_id"`      // Operator ID for audit trail
	MinLotSize string `json:"min_lot_size"` // Minimum trade unit (e.g., "0.00000001")
}

// MarshalBinary serializes the CreateMarketParams to binary format.
func (c *CreateMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	writeString(buf[offset:], c.MinLotSize)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *CreateMarketParams) BinarySize() int {
	return 8 + 8 + 2 + len(c.MinLotSize)
}

// UnmarshalBinary deserializes the CreateMarketParams from binary format.
func (c *CreateMarketParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.MinLotSize, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

// SuspendMarketParams is the payload for suspending a market.
type SuspendMarketParams struct {
	CommandID string `json:"-"`
	Timestamp int64  `json:"-"`
	UserID    uint64 `json:"user_id"`   // Operator ID for audit trail
	MarketID  string `json:"market_id"` // Target market to suspend
	Reason    string `json:"reason"`    // Reason for suspension (for audit)
}

// MarshalBinary serializes the SuspendMarketParams to binary format.
func (c *SuspendMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	offset += writeString(buf[offset:], c.MarketID)
	writeString(buf[offset:], c.Reason)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *SuspendMarketParams) BinarySize() int {
	return 8 + 8 + 2*2 + len(c.MarketID) + len(c.Reason)
}

// UnmarshalBinary deserializes the SuspendMarketParams from binary format.
func (c *SuspendMarketParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.MarketID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.Reason, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

// ResumeMarketParams is the payload for resuming a suspended market.
type ResumeMarketParams struct {
	CommandID string `json:"-"`
	Timestamp int64  `json:"-"`
	UserID    uint64 `json:"user_id"`   // Operator ID for audit trail
	MarketID  string `json:"market_id"` // Target market to resume
}

// MarshalBinary serializes the ResumeMarketParams to binary format.
func (c *ResumeMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	writeString(buf[offset:], c.MarketID)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *ResumeMarketParams) BinarySize() int {
	return 8 + 8 + 2 + len(c.MarketID)
}

// UnmarshalBinary deserializes the ResumeMarketParams from binary format.
func (c *ResumeMarketParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.MarketID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

// UpdateConfigParams is the payload for updating market configuration.
type UpdateConfigParams struct {
	CommandID  string `json:"-"`
	Timestamp  int64  `json:"-"`
	UserID     uint64 `json:"user_id"`                // Operator ID for audit trail
	MarketID   string `json:"market_id"`              // Target market
	MinLotSize string `json:"min_lot_size,omitempty"` // New minimum lot size (optional)
}

// MarshalBinary serializes the UpdateConfigParams to binary format.
func (c *UpdateConfigParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	offset += writeString(buf[offset:], c.MarketID)
	writeString(buf[offset:], c.MinLotSize)
	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *UpdateConfigParams) BinarySize() int {
	return 8 + 8 + 2*2 + len(c.MarketID) + len(c.MinLotSize)
}

// UnmarshalBinary deserializes the UpdateConfigParams from binary format.
func (c *UpdateConfigParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.MarketID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.MinLotSize, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	_ = n
	return nil
}

// UserEventParams is the payload for generic user events.
type UserEventParams struct {
	CommandID string `json:"-"`
	MarketID  string `json:"-"`
	Timestamp int64  `json:"-"`
	UserID    uint64 `json:"user_id"`    // Operator ID for audit trail
	EventType string `json:"event_type"` // e.g. "EndOfBlock", "AdminMarker"
	Key       string `json:"key,omitempty"`
	Data      []byte `json:"data,omitempty"`
}

// MarshalBinary serializes the UserEventParams to binary format.
func (c *UserEventParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8
	offset += writeString(buf[offset:], c.EventType)
	offset += writeString(buf[offset:], c.Key)

	/* #nosec G115 */
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(c.Data)))
	offset += 4
	copy(buf[offset:], c.Data)

	return buf, nil
}

// BinarySize returns the size of the command in binary format.
func (c *UserEventParams) BinarySize() int {
	return 8 + 8 + 2*2 + len(c.EventType) + len(c.Key) + 4 + len(c.Data)
}

// UnmarshalBinary deserializes the UserEventParams from binary format.
func (c *UserEventParams) UnmarshalBinary(data []byte) error {
	if len(data) > 0 && data[0] == '{' {
		return errors.New("looks like JSON")
	}
	if len(data) < 16 { //nolint:mnd // UserID(8) + Timestamp(8)
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8

	if len(data[offset:]) < 8 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	/* #nosec G115 */
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8
	var n int
	var err error
	c.EventType, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	c.Key, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n

	if len(data[offset:]) < 4 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	dataLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4

	if len(data[offset:]) < dataLen {
		return io.ErrUnexpectedEOF
	}
	c.Data = make([]byte, dataLen)
	copy(c.Data, data[offset:offset+dataLen])

	return nil
}
