package protocol

import (
	"encoding/binary"
	"io"
	"unsafe"

	"github.com/quagmt/udecimal"
)

// CommandType defines the type of the command.
type CommandType uint8

const (
	CmdUnknown       CommandType = 0
	CmdPlaceOrder    CommandType = 1
	CmdCancelOrder   CommandType = 2
	CmdAmendOrder    CommandType = 3
	CmdCreateMarket  CommandType = 11
	CmdSuspendMarket CommandType = 12
	CmdResumeMarket  CommandType = 13
	CmdUpdateConfig  CommandType = 14
	CmdUserEvent     CommandType = 21
)

const (
	stringLenSize  = 2
	payloadLenSize = 4
)

// Params is the common interface for all command parameters.
type Params interface {
	MarshalBinary() ([]byte, error)
	BinarySize() int
	UnmarshalBinary(data []byte) error
}

// Command represents a single command to be processed by the matching engine.
type Command struct {
	Version   uint8
	Type      CommandType
	SeqID     uint64
	UserID    uint64
	MarketID  string
	CommandID string
	Timestamp int64
	Params    any // Parsed specialized params
}

// MarshalCommand serializes a Command and its payload into a single byte slice.
func MarshalCommand(c *Command) ([]byte, error) {
	var payloadSize int
	if p, ok := c.Params.(Params); ok {
		payloadSize = p.BinarySize()
	}

	totalSize := 1 + 8 + 1 + 8 + 8 +
		stringLenSize + len(c.MarketID) +
		stringLenSize + len(c.CommandID) +
		payloadLenSize + payloadSize

	buf := make([]byte, totalSize)
	offset := 0

	buf[offset] = c.Version
	offset++
	binary.BigEndian.PutUint64(buf[offset:], c.UserID)
	offset += 8
	buf[offset] = uint8(c.Type)
	offset++
	binary.BigEndian.PutUint64(buf[offset:], c.SeqID)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(c.Timestamp))
	offset += 8

	offset += writeString(buf[offset:], c.MarketID)
	offset += writeString(buf[offset:], c.CommandID)

	binary.BigEndian.PutUint32(buf[offset:], uint32(payloadSize))
	offset += 4

	if payloadSize > 0 {
		p := c.Params.(Params)
		pData, err := p.MarshalBinary()
		if err != nil {
			return nil, err
		}
		copy(buf[offset:], pData)
	}

	return buf, nil
}

// UnmarshalCommand deserializes a Command and its payload from binary format.
func UnmarshalCommand(data []byte) (*Command, error) {
	if len(data) < 26 {
		return nil, io.ErrUnexpectedEOF
	}

	c := &Command{}
	offset := 0
	c.Version = data[offset]
	offset++
	c.UserID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	c.Type = CommandType(data[offset])
	offset++
	c.SeqID = binary.BigEndian.Uint64(data[offset:])
	offset += 8
	c.Timestamp = int64(binary.BigEndian.Uint64(data[offset:]))
	offset += 8

	var n int
	var err error
	c.MarketID, n, err = readString(data[offset:])
	if err != nil {
		return nil, err
	}
	offset += n
	c.CommandID, n, err = readString(data[offset:])
	if err != nil {
		return nil, err
	}
	offset += n

	if len(data[offset:]) < 4 {
		return nil, io.ErrUnexpectedEOF
	}
	payloadLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4

	if payloadLen > 0 {
		if len(data[offset:]) < payloadLen {
			return nil, io.ErrUnexpectedEOF
		}
		pData := data[offset : offset+payloadLen]
		switch c.Type {
		case CmdPlaceOrder:
			p := &PlaceOrderParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdCancelOrder:
			p := &CancelOrderParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdAmendOrder:
			p := &AmendOrderParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdCreateMarket:
			p := &CreateMarketParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdSuspendMarket:
			p := &SuspendMarketParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdUpdateConfig:
			p := &UpdateConfigParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		case CmdUserEvent:
			p := &UserEventParams{}
			if err := p.UnmarshalBinary(pData); err != nil {
				return nil, err
			}
			c.Params = p
		}
	}

	return c, nil
}

// MarshalBinary is kept for compatibility with Params interface.
func (c *Command) MarshalBinary() ([]byte, error) {
	return MarshalCommand(c)
}

// UnmarshalBinary is kept for compatibility.
func (c *Command) UnmarshalBinary(data []byte) error {
	nc, err := UnmarshalCommand(data)
	if err != nil {
		return err
	}
	*c = *nc
	return nil
}

// SetPayload sets the specialized params of the command.
func (c *Command) SetPayload(p Params) error {
	c.Params = p
	return nil
}

// --- Specialized Params (Business Payloads) ---

type PlaceOrderParams struct {
	OrderID     string           `json:"order_id"`
	Side        Side             `json:"side"`
	OrderType   OrderType        `json:"order_type"`
	Price       udecimal.Decimal `json:"price"`
	Size        udecimal.Decimal `json:"size"`
	VisibleSize udecimal.Decimal `json:"visible_size,omitempty"`
	QuoteSize   udecimal.Decimal `json:"quote_size,omitempty"`
}

func (c *PlaceOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	buf[offset] = uint8(c.Side)
	offset++
	buf[offset] = c.OrderType.ToUint8()
	offset++
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.Price.String())
	offset += writeString(buf[offset:], c.Size.String())
	offset += writeString(buf[offset:], c.VisibleSize.String())
	writeString(buf[offset:], c.QuoteSize.String())
	return buf, nil
}

func (c *PlaceOrderParams) BinarySize() int {
	return 1 + 1 + stringLenSize*5 + len(c.OrderID) + len(c.Price.String()) + len(c.Size.String()) + len(c.VisibleSize.String()) + len(c.QuoteSize.String())
}

func (c *PlaceOrderParams) UnmarshalBinary(data []byte) error {
	if len(data) < 2 {
		return io.ErrUnexpectedEOF
	}
	offset := 0
	c.Side = Side(data[offset])
	offset++
	c.OrderType = OrderTypeFromUint8(data[offset])
	offset++
	var n int
	var err error
	var s string
	c.OrderID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	if s, n, err = readString(data[offset:]); err != nil {
		return err
	}
	c.Price, err = udecimal.Parse(s)
	if err != nil {
		return err
	}
	offset += n

	if s, n, err = readString(data[offset:]); err != nil {
		return err
	}
	c.Size, err = udecimal.Parse(s)
	if err != nil {
		return err
	}
	offset += n

	if s, n, err = readString(data[offset:]); err != nil {
		return err
	}
	c.VisibleSize, err = udecimal.Parse(s)
	if err != nil {
		return err
	}
	offset += n

	if s, _, err = readString(data[offset:]); err != nil {
		return err
	}
	c.QuoteSize, err = udecimal.Parse(s)
	return err
}

type CancelOrderParams struct {
	OrderID string `json:"order_id"`
}

func (c *CancelOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.OrderID)
	return buf, nil
}

func (c *CancelOrderParams) BinarySize() int {
	return stringLenSize + len(c.OrderID)
}

func (c *CancelOrderParams) UnmarshalBinary(data []byte) error {
	var err error
	c.OrderID, _, err = readString(data)
	return err
}

type AmendOrderParams struct {
	OrderID  string           `json:"order_id"`
	NewPrice udecimal.Decimal `json:"new_price"`
	NewSize  udecimal.Decimal `json:"new_size"`
}

func (c *AmendOrderParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	offset := 0
	offset += writeString(buf[offset:], c.OrderID)
	offset += writeString(buf[offset:], c.NewPrice.String())
	writeString(buf[offset:], c.NewSize.String())
	return buf, nil
}

func (c *AmendOrderParams) BinarySize() int {
	return stringLenSize*3 + len(c.OrderID) + len(c.NewPrice.String()) + len(c.NewSize.String())
}

func (c *AmendOrderParams) UnmarshalBinary(data []byte) error {
	offset := 0
	var n int
	var err error
	var s string
	c.OrderID, n, err = readString(data[offset:])
	if err != nil {
		return err
	}
	offset += n
	if s, n, err = readString(data[offset:]); err == nil {
		c.NewPrice, _ = udecimal.Parse(s)
		offset += n
	}
	if s, _, err = readString(data[offset:]); err == nil {
		c.NewSize, _ = udecimal.Parse(s)
	}
	return nil
}

type CreateMarketParams struct {
	MinLotSize udecimal.Decimal `json:"min_lot_size"`
}

func (c *CreateMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.MinLotSize.String())
	return buf, nil
}

func (c *CreateMarketParams) BinarySize() int {
	return stringLenSize + len(c.MinLotSize.String())
}

func (c *CreateMarketParams) UnmarshalBinary(data []byte) error {
	s, _, err := readString(data)
	if err == nil {
		c.MinLotSize, _ = udecimal.Parse(s)
	}
	return err
}

type SuspendMarketParams struct {
	Reason string `json:"reason"`
}

func (c *SuspendMarketParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.Reason)
	return buf, nil
}

func (c *SuspendMarketParams) BinarySize() int {
	return stringLenSize + len(c.Reason)
}

func (c *SuspendMarketParams) UnmarshalBinary(data []byte) error {
	var err error
	c.Reason, _, err = readString(data)
	return err
}

type ResumeMarketParams struct{}

func (c *ResumeMarketParams) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

func (c *ResumeMarketParams) BinarySize() int {
	return 0
}

func (c *ResumeMarketParams) UnmarshalBinary(_ []byte) error {
	return nil
}

type UpdateConfigParams struct {
	MinLotSize udecimal.Decimal `json:"min_lot_size"`
}

func (c *UpdateConfigParams) MarshalBinary() ([]byte, error) {
	buf := make([]byte, c.BinarySize())
	writeString(buf, c.MinLotSize.String())
	return buf, nil
}

func (c *UpdateConfigParams) BinarySize() int {
	return stringLenSize + len(c.MinLotSize.String())
}

func (c *UpdateConfigParams) UnmarshalBinary(data []byte) error {
	s, _, err := readString(data)
	if err == nil {
		c.MinLotSize, _ = udecimal.Parse(s)
	}
	return err
}

type UserEventParams struct {
	EventType string `json:"event_type"`
	Key       string `json:"key"`
	Data      []byte `json:"data"`
}

func (c *UserEventParams) MarshalBinary() ([]byte, error) {
	size := stringLenSize + len(c.EventType) + stringLenSize + len(c.Key) + payloadLenSize + len(c.Data)
	buf := make([]byte, size)
	offset := 0
	offset += writeString(buf[offset:], c.EventType)
	offset += writeString(buf[offset:], c.Key)
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(c.Data)))
	offset += payloadLenSize
	copy(buf[offset:], c.Data)
	return buf, nil
}

func (c *UserEventParams) BinarySize() int {
	return stringLenSize + len(c.EventType) + stringLenSize + len(c.Key) + payloadLenSize + len(c.Data)
}

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

// --- Query Types ---

type QueryType uint8

const (
	QueryUnknown  QueryType = 0
	QueryGetDepth QueryType = 1
	QueryGetStats QueryType = 2
	QuerySnapshot QueryType = 3
)

type Query struct {
	Type     QueryType
	MarketID string
	Payload  any
}

type GetDepthRequest struct {
	MarketID string `json:"market_id"`
	Limit    uint32 `json:"limit"`
}

type GetStatsRequest struct {
	MarketID string `json:"market_id"`
}

func writeString(buf []byte, s string) int {
	l := len(s)
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
	b := data[stringLenSize : stringLenSize+l]
	return *(*string)(unsafe.Pointer(&b)), stringLenSize + l, nil
}
