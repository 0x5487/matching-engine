package protocol

import (
	"encoding/binary"
	"io"
)

// CommandType defines the type of the command (using uint8 for memory alignment and performance).
type CommandType uint8

// Command Type Numbering Scheme (Review 8.4.1).
const (
	// CmdUnknown represents an unknown command type.
	CmdUnknown CommandType = 0
	// CmdPlaceOrder represents a place order command.
	CmdPlaceOrder CommandType = 1
	// CmdCancelOrder represents a cancel order command.
	CmdCancelOrder CommandType = 2
	// CmdAmendOrder represents an amend order command.
	CmdAmendOrder CommandType = 3
	// CmdCreateMarket represents a create market command.
	CmdCreateMarket CommandType = 11
	// CmdSuspendMarket represents a suspend market command.
	CmdSuspendMarket CommandType = 12
	// CmdResumeMarket represents a resume market command.
	CmdResumeMarket CommandType = 13
	// CmdUpdateConfig represents an update config command.
	CmdUpdateConfig CommandType = 14
	// CmdUserEvent represents a user event command.
	CmdUserEvent CommandType = 21
)

const (
	stringLenSize  = 2
	payloadLenSize = 4
)

// Params is the common interface for all command parameters.
type Params interface {
	MarshalBinary() ([]byte, error)
	BinarySize() int
}

// Command represents a single command to be processed by the matching engine.
// It serves as an envelope for different specialized payloads.
type Command struct {
	Version   uint8
	Type      CommandType
	SeqID     uint64
	UserID    uint64
	MarketID  string
	CommandID string
	Timestamp int64
	Payload   []byte // Marshaled specialized params
}

// MarshalBinary serializes the Command (envelope) to binary format.
func (c *Command) MarshalBinary() ([]byte, error) {
	// PayloadLen(4) + Payload(L)
	size := 1 + 8 + 1 + 8 + 8 +
		stringLenSize + len(c.MarketID) +
		stringLenSize + len(c.CommandID) +
		payloadLenSize + len(c.Payload)
	buf := make([]byte, size)
	offset := 0

	buf[offset] = c.Version
	offset++
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	buf[offset] = uint8(c.Type)
	offset++

	binary.BigEndian.PutUint64(buf[offset:], c.SeqID)
	offset += 8
	/* #nosec G115 */
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8

	offset += writeString(buf[offset:], c.MarketID)
	offset += writeString(buf[offset:], c.CommandID)

	/* #nosec G115 */
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(c.Payload)))
	offset += 4
	copy(buf[offset:], c.Payload)

	return buf, nil
}

// UnmarshalBinary deserializes the Command (envelope) from binary format.
func (c *Command) UnmarshalBinary(data []byte) error {
	if len(data) < 26 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.Version = data[offset]
	offset++
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	c.Type = CommandType(data[offset])
	offset++
	c.SeqID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
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

// SetPayload sets the marshaled business payload of the command.
func (c *Command) SetPayload(p Params) error {
	data, err := p.MarshalBinary()
	if err != nil {
		return err
	}
	c.Payload = data
	return nil
}

// --- Query Types ---

// GetDepthRequest represents a request to get the order book depth.
type GetDepthRequest struct {
	MarketID string `json:"market_id"`
	Limit    uint32 `json:"limit"`
}

// GetStatsRequest represents a request to get market statistics.
type GetStatsRequest struct {
	MarketID string `json:"market_id"`
}

// --- Specialized Params (Business Payloads) ---

// PlaceOrderParams contains parameters for placing an order.
type PlaceOrderParams struct {
	OrderID     string    `json:"order_id"`
	Side        Side      `json:"side"`
	OrderType   OrderType `json:"order_type"`
	Price       string    `json:"price"`
	Size        string    `json:"size"`
	VisibleSize string    `json:"visible_size,omitempty"`
	QuoteSize   string    `json:"quote_size,omitempty"`
}

// MarshalBinary serializes PlaceOrderParams to binary format.
func (c *PlaceOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	/* #nosec G115 */
	buf[offset] = uint8(c.Side)
	offset++
	buf[offset] = c.OrderType.ToUint8()
	offset++
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.Price)
	offset += writeString(buf[offset:], c.Size)
	offset += writeString(buf[offset:], c.VisibleSize)
	writeString(buf[offset:], c.QuoteSize)
	return buf, nil
}

// BinarySize returns the binary size of PlaceOrderParams.
func (c *PlaceOrderParams) BinarySize() int {
	return 1 + 1 + stringLenSize*5 + len(
		c.OrderID,
	) + len(
		c.Price,
	) + len(
		c.Size,
	) + len(
		c.VisibleSize,
	) + len(
		c.QuoteSize,
	)
}

// UnmarshalBinary deserializes PlaceOrderParams from binary format.
func (c *PlaceOrderParams) UnmarshalBinary(data []byte) error {
	if len(data) < 2 { //nolint:mnd
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.Side = Side(data[offset])
	offset++
	c.OrderType = OrderTypeFromUint8(data[offset])
	offset++
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
	c.QuoteSize, _, err = readString(data[offset:])
	return err
}

// CancelOrderParams contains parameters for canceling an order.
type CancelOrderParams struct {
	OrderID string `json:"order_id"`
}

// MarshalBinary serializes CancelOrderParams to binary format.
func (c *CancelOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.OrderID)
	return buf, nil
}

// BinarySize returns the binary size of CancelOrderParams.
func (c *CancelOrderParams) BinarySize() int {
	return stringLenSize + len(c.OrderID)
}

// UnmarshalBinary deserializes CancelOrderParams from binary format.
func (c *CancelOrderParams) UnmarshalBinary(data []byte) error {
	var err error
	c.OrderID, _, err = readString(data)
	return err
}

// AmendOrderParams contains parameters for amending an order.
type AmendOrderParams struct {
	OrderID  string `json:"order_id"`
	NewPrice string `json:"new_price"`
	NewSize  string `json:"new_size"`
}

// MarshalBinary serializes AmendOrderParams to binary format.
func (c *AmendOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.NewPrice)
	writeString(buf[offset:], c.NewSize)
	return buf, nil
}

// BinarySize returns the binary size of AmendOrderParams.
func (c *AmendOrderParams) BinarySize() int {
	return stringLenSize*3 + len(c.OrderID) + len(c.NewPrice) + len(c.NewSize)
}

// UnmarshalBinary deserializes AmendOrderParams from binary format.
func (c *AmendOrderParams) UnmarshalBinary(data []byte) error {
	offset := 0
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
	c.NewSize, _, err = readString(data[offset:])
	return err
}

// CreateMarketParams contains parameters for creating a market.
type CreateMarketParams struct {
	MinLotSize string `json:"min_lot_size"`
}

// MarshalBinary serializes CreateMarketParams to binary format.
func (c *CreateMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.MinLotSize)
	return buf, nil
}

// BinarySize returns the binary size of CreateMarketParams.
func (c *CreateMarketParams) BinarySize() int {
	return stringLenSize + len(c.MinLotSize)
}

// UnmarshalBinary deserializes CreateMarketParams from binary format.
func (c *CreateMarketParams) UnmarshalBinary(data []byte) error {
	var err error
	c.MinLotSize, _, err = readString(data)
	return err
}

// SuspendMarketParams contains parameters for suspending a market.
type SuspendMarketParams struct {
	Reason string `json:"reason"`
}

// MarshalBinary serializes SuspendMarketParams to binary format.
func (c *SuspendMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.Reason)
	return buf, nil
}

// BinarySize returns the binary size of SuspendMarketParams.
func (c *SuspendMarketParams) BinarySize() int {
	return stringLenSize + len(c.Reason)
}

// UnmarshalBinary deserializes SuspendMarketParams from binary format.
func (c *SuspendMarketParams) UnmarshalBinary(data []byte) error {
	var err error
	c.Reason, _, err = readString(data)
	return err
}

// ResumeMarketParams contains parameters for resuming a market.
type ResumeMarketParams struct{}

// MarshalBinary serializes ResumeMarketParams to binary format.
func (c *ResumeMarketParams) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

// BinarySize returns the binary size of ResumeMarketParams.
func (c *ResumeMarketParams) BinarySize() int {
	return 0
}

// UnmarshalBinary deserializes ResumeMarketParams from binary format.
func (c *ResumeMarketParams) UnmarshalBinary(_ []byte) error {
	return nil
}

// UpdateConfigParams contains parameters for updating market configuration.
type UpdateConfigParams struct {
	MinLotSize string `json:"min_lot_size"`
}

// MarshalBinary serializes UpdateConfigParams to binary format.
func (c *UpdateConfigParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.MinLotSize)
	return buf, nil
}

// BinarySize returns the binary size of UpdateConfigParams.
func (c *UpdateConfigParams) BinarySize() int {
	return stringLenSize + len(c.MinLotSize)
}

// UnmarshalBinary deserializes UpdateConfigParams from binary format.
func (c *UpdateConfigParams) UnmarshalBinary(data []byte) error {
	var err error
	c.MinLotSize, _, err = readString(data)
	return err
}

// UserEventParams contains parameters for a user event.
type UserEventParams struct {
	EventType string `json:"event_type"`
	Key       string `json:"key"`
	Data      []byte `json:"data"`
}

// MarshalBinary serializes UserEventParams to binary format.
func (c *UserEventParams) MarshalBinary() ([]byte, error) {
	/* #nosec G115 */
	size := stringLenSize + len(
		c.EventType,
	) + stringLenSize + len(
		c.Key,
	) + payloadLenSize + len(
		c.Data,
	)
	buf := make([]byte, size)
	offset := 0
	offset += writeString(buf[offset:], c.EventType)
	offset += writeString(buf[offset:], c.Key)
	/* #nosec G115 */
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(c.Data)))
	offset += payloadLenSize
	copy(buf[offset:], c.Data)
	return buf, nil
}

// BinarySize returns the binary size of UserEventParams.
func (c *UserEventParams) BinarySize() int {
	return stringLenSize + len(c.EventType) + stringLenSize + len(c.Key) + payloadLenSize + len(c.Data)
}

// UnmarshalBinary deserializes UserEventParams from binary format.
func (c *UserEventParams) UnmarshalBinary(data []byte) error {
	offset := 0
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
	if len(data[offset:]) < payloadLenSize {
		return io.ErrUnexpectedEOF
	}
	dataLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += payloadLenSize
	if len(data[offset:]) < dataLen {
		return io.ErrUnexpectedEOF
	}
	c.Data = make([]byte, dataLen)
	copy(c.Data, data[offset:])
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
