package protocol

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/quagmt/udecimal"
)

// CommandType defines the type of the command.
type CommandType uint8

const (
	// CmdUnknown represents an unknown command type.
	CmdUnknown CommandType = 0
	// CmdPlaceOrder represents a place-order command.
	CmdPlaceOrder CommandType = 1
	// CmdCancelOrder represents a cancel-order command.
	CmdCancelOrder CommandType = 2
	// CmdAmendOrder represents an amend-order command.
	CmdAmendOrder CommandType = 3
	// CmdCreateMarket represents a create-market command.
	CmdCreateMarket CommandType = 11
	// CmdSuspendMarket represents a suspend-market command.
	CmdSuspendMarket CommandType = 12
	// CmdResumeMarket represents a resume-market command.
	CmdResumeMarket CommandType = 13
	// CmdUpdateConfig represents an update-config command.
	CmdUpdateConfig CommandType = 14
	// CmdUserEvent represents a user-event command.
	CmdUserEvent CommandType = 21
)

const (
	stringLenSize    = 2
	payloadLenSize   = 4
	versionSize      = 1
	userIDSize       = 8
	commandTypeSize  = 1
	seqIDSize        = 8
	timestampLenSize = 8
	sideFieldSize    = 1
	orderTypeSize    = 1
	minPayloadSize   = 2

	requestHeaderSize = versionSize + userIDSize + commandTypeSize + seqIDSize + timestampLenSize
	minRequestSize    = requestHeaderSize + stringLenSize + stringLenSize + payloadLenSize

	maxUint16Value = 1<<16 - 1
	maxUint32Value = 1<<32 - 1
	maxInt64Value  = 1<<63 - 1
	sideBuyValue   = 1
	sideSellValue  = 2
)

var (
	errUnknownRequest  = errors.New("unknown request")
	errStringTooLong   = errors.New("string too long")
	errPayloadTooLarge = errors.New("payload too large")
	errInvalidRequest  = errors.New("invalid request")
)

// BaseCommand contains the shared metadata for all command requests.
type BaseCommand struct {
	SeqID     uint64 // Upstream-assigned monotonic sequence used to preserve logical command ordering.
	CommandID string
	UserID    uint64
	MarketID  string
	Timestamp int64
}

// CommandRequest is the interface that all typed command requests must implement.
type CommandRequest interface {
	Base() BaseCommand
}

// GetRequestBase returns the shared metadata for a typed request and a boolean indicating success.
func GetRequestBase(req any) (BaseCommand, bool) {
	if cr, ok := req.(CommandRequest); ok {
		return cr.Base(), true
	}
	// Handle BaseCommand directly (used in tests)
	switch r := req.(type) {
	case *BaseCommand:
		return *r, true
	case BaseCommand:
		return r, true
	}
	return BaseCommand{}, false
}

// Base returns the embedded BaseCommand (implements CommandRequest).
func (r *BaseCommand) Base() BaseCommand { return *r }

// --- Specialized Requests (Typed Payloads) ---

// PlaceOrderRequest represents a typed place-order command.
type PlaceOrderRequest struct {
	BaseCommand

	OrderID     string           `json:"order_id"`
	Side        Side             `json:"side"`
	OrderType   OrderType        `json:"order_type"`
	Price       udecimal.Decimal `json:"price"`
	Size        udecimal.Decimal `json:"size"`
	VisibleSize udecimal.Decimal `json:"visible_size"`
	QuoteSize   udecimal.Decimal `json:"quote_size"`
}

// Base returns the embedded BaseCommand.
func (r *PlaceOrderRequest) Base() BaseCommand { return r.BaseCommand }

// CancelOrderRequest represents a typed cancel-order command.
type CancelOrderRequest struct {
	BaseCommand

	OrderID string `json:"order_id"`
}

// Base returns the embedded BaseCommand.
func (r *CancelOrderRequest) Base() BaseCommand { return r.BaseCommand }

// AmendOrderRequest represents a typed amend-order command.
type AmendOrderRequest struct {
	BaseCommand

	OrderID  string           `json:"order_id"`
	NewPrice udecimal.Decimal `json:"new_price"`
	NewSize  udecimal.Decimal `json:"new_size"`
}

// Base returns the embedded BaseCommand.
func (r *AmendOrderRequest) Base() BaseCommand { return r.BaseCommand }

// CreateMarketRequest represents a typed create-market command.
type CreateMarketRequest struct {
	BaseCommand

	MinLotSize udecimal.Decimal `json:"min_lot_size"`
}

// Base returns the embedded BaseCommand.
func (r *CreateMarketRequest) Base() BaseCommand { return r.BaseCommand }

// SuspendMarketRequest represents a typed suspend-market command.
type SuspendMarketRequest struct {
	BaseCommand

	Reason string `json:"reason"`
}

// Base returns the embedded BaseCommand.
func (r *SuspendMarketRequest) Base() BaseCommand { return r.BaseCommand }

// ResumeMarketRequest represents a typed resume-market command.
type ResumeMarketRequest struct {
	BaseCommand
}

// Base returns the embedded BaseCommand.
func (r *ResumeMarketRequest) Base() BaseCommand { return r.BaseCommand }

// UpdateConfigRequest represents a typed update-config command.
type UpdateConfigRequest struct {
	BaseCommand

	MinLotSize udecimal.Decimal `json:"min_lot_size"`
}

// Base returns the embedded BaseCommand.
func (r *UpdateConfigRequest) Base() BaseCommand { return r.BaseCommand }

// UserEventRequest represents a typed user-event command.
type UserEventRequest struct {
	BaseCommand

	EventType string `json:"event_type"`
	Key       string `json:"key"`
	Data      []byte `json:"data"`
}

// Base returns the embedded BaseCommand.
func (r *UserEventRequest) Base() BaseCommand { return r.BaseCommand }

// MarshalRequest serializes a typed request into binary format.
// It follows the wire format: version(1), user_id(8), type(1), seq_id(8), timestamp(8), market_id(string), command_id(string), payload_len(4), payload(n).
// The wire CommandType is always derived from the concrete request type.
func MarshalRequest(req any) ([]byte, error) {
	base, ok := GetRequestBase(req)
	if !ok {
		return nil, errUnknownRequest
	}
	if base.Timestamp < 0 {
		return nil, errInvalidRequest
	}

	var (
		payload  []byte
		wireType CommandType
	)

	switch r := req.(type) {
	case *PlaceOrderRequest:
		wireType = CmdPlaceOrder
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.OrderID) > maxUint16Value {
			return nil, errStringTooLong
		}
		// side(1) + orderType(1) + orderID(string) + price(string) + size(string) + visibleSize(string) + quoteSize(string)
		priceStr := r.Price.String()
		sizeStr := r.Size.String()
		visStr := r.VisibleSize.String()
		quoteStr := r.QuoteSize.String()
		pSize := sideFieldSize + orderTypeSize + stringLenSize*5 + len(r.OrderID) + len(priceStr) + len(sizeStr) + len(visStr) + len(quoteStr)
		buf := make([]byte, pSize)
		offset := 0
		buf[offset] = sideToUint8(r.Side)
		offset++
		buf[offset] = r.OrderType.ToUint8()
		offset++
		offset += mustWriteString(buf[offset:], r.OrderID)
		offset += mustWriteString(buf[offset:], priceStr)
		offset += mustWriteString(buf[offset:], sizeStr)
		offset += mustWriteString(buf[offset:], visStr)
		mustWriteString(buf[offset:], quoteStr)
		payload = buf
	case *CancelOrderRequest:
		wireType = CmdCancelOrder
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.OrderID) > maxUint16Value {
			return nil, errStringTooLong
		}
		buf := make([]byte, stringLenSize+len(r.OrderID))
		mustWriteString(buf, r.OrderID)
		payload = buf
	case *AmendOrderRequest:
		wireType = CmdAmendOrder
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.OrderID) > maxUint16Value {
			return nil, errStringTooLong
		}
		priceStr := r.NewPrice.String()
		sizeStr := r.NewSize.String()
		buf := make([]byte, stringLenSize*3+len(r.OrderID)+len(priceStr)+len(sizeStr))
		offset := 0
		offset += mustWriteString(buf[offset:], r.OrderID)
		offset += mustWriteString(buf[offset:], priceStr)
		mustWriteString(buf[offset:], sizeStr)
		payload = buf
	case *CreateMarketRequest:
		wireType = CmdCreateMarket
		s := r.MinLotSize.String()
		buf := make([]byte, stringLenSize+len(s))
		mustWriteString(buf, s)
		payload = buf
	case *SuspendMarketRequest:
		wireType = CmdSuspendMarket
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.Reason) > maxUint16Value {
			return nil, errStringTooLong
		}
		buf := make([]byte, stringLenSize+len(r.Reason))
		mustWriteString(buf, r.Reason)
		payload = buf
	case *ResumeMarketRequest:
		wireType = CmdResumeMarket
		payload = []byte{}
	case *UpdateConfigRequest:
		wireType = CmdUpdateConfig
		s := r.MinLotSize.String()
		buf := make([]byte, stringLenSize+len(s))
		mustWriteString(buf, s)
		payload = buf
	case *UserEventRequest:
		wireType = CmdUserEvent
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.EventType) > maxUint16Value || len(r.Key) > maxUint16Value {
			return nil, errStringTooLong
		}
		pSize := stringLenSize + len(r.EventType) + stringLenSize + len(r.Key) + payloadLenSize + len(r.Data)
		buf := make([]byte, pSize)
		offset := 0
		offset += mustWriteString(buf[offset:], r.EventType)
		offset += mustWriteString(buf[offset:], r.Key)
		binary.BigEndian.PutUint32(buf[offset:], safeUint32Len(len(r.Data)))
		offset += payloadLenSize
		copy(buf[offset:], r.Data)
		payload = buf
	default:
		return nil, errUnknownRequest
	}

	if len(base.MarketID) > maxUint16Value || len(base.CommandID) > maxUint16Value {
		return nil, errStringTooLong
	}
	if len(payload) > maxUint32Value {
		return nil, errPayloadTooLarge
	}

	payloadSize := len(payload)
	totalSize := requestHeaderSize +
		stringLenSize + len(base.MarketID) +
		stringLenSize + len(base.CommandID) +
		payloadLenSize + payloadSize

	buf := make([]byte, totalSize)
	offset := 0

	buf[offset] = 1 // Hardcoded version
	offset++
	binary.BigEndian.PutUint64(buf[offset:], base.UserID)
	offset += 8
	buf[offset] = uint8(wireType)
	offset++
	binary.BigEndian.PutUint64(buf[offset:], base.SeqID)
	offset += 8
	binary.BigEndian.PutUint64(buf[offset:], uint64(base.Timestamp))
	offset += 8

	offset += mustWriteString(buf[offset:], base.MarketID)
	offset += mustWriteString(buf[offset:], base.CommandID)

	binary.BigEndian.PutUint32(buf[offset:], uint32(payloadSize))
	offset += 4

	if payloadSize > 0 {
		copy(buf[offset:], payload)
	}

	return buf, nil
}

// UnmarshalRequest deserializes a typed request from binary format.
func UnmarshalRequest(data []byte) (any, error) {
	if len(data) < minRequestSize {
		return nil, io.ErrUnexpectedEOF
	}

	offset := 0
	_ = data[offset] // Skip version
	offset++
	userID := binary.BigEndian.Uint64(data[offset:])
	offset += 8
	cmdType := CommandType(data[offset])
	offset++
	seqID := binary.BigEndian.Uint64(data[offset:])
	offset += 8
	timestampValue := binary.BigEndian.Uint64(data[offset:])
	if timestampValue > maxInt64Value {
		return nil, errInvalidRequest
	}
	timestamp := int64(timestampValue)
	offset += 8

	marketID, n, err := readString(data[offset:])
	if err != nil {
		return nil, err
	}
	offset += n
	commandID, n, err := readString(data[offset:])
	if err != nil {
		return nil, err
	}
	offset += n

	if len(data[offset:]) < payloadLenSize {
		return nil, io.ErrUnexpectedEOF
	}
	payloadLen := int(binary.BigEndian.Uint32(data[offset:]))
	offset += 4

	if len(data[offset:]) < payloadLen {
		return nil, io.ErrUnexpectedEOF
	}
	pData := data[offset : offset+payloadLen]

	base := BaseCommand{
		SeqID:     seqID,
		CommandID: commandID,
		UserID:    userID,
		MarketID:  marketID,
		Timestamp: timestamp,
	}

	switch cmdType {
	case CmdPlaceOrder:
		if len(pData) < minPayloadSize {
			return nil, io.ErrUnexpectedEOF
		}
		pOffset := 0
		side := Side(pData[pOffset])
		pOffset++
		orderType := OrderTypeFromUint8(pData[pOffset])
		pOffset++
		orderID, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		pOffset += n
		priceStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		price, _ := udecimal.Parse(priceStr)
		pOffset += n
		sizeStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		size, _ := udecimal.Parse(sizeStr)
		pOffset += n
		visStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		visibleSize, _ := udecimal.Parse(visStr)
		pOffset += n
		quoteStr, _, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		quoteSize, _ := udecimal.Parse(quoteStr)

		return &PlaceOrderRequest{
			BaseCommand: base,
			OrderID:     orderID,
			Side:        side,
			OrderType:   orderType,
			Price:       price,
			Size:        size,
			VisibleSize: visibleSize,
			QuoteSize:   quoteSize,
		}, nil
	case CmdCancelOrder:
		orderID, _, err := readString(pData)
		if err != nil {
			return nil, err
		}
		return &CancelOrderRequest{
			BaseCommand: base,
			OrderID:     orderID,
		}, nil
	case CmdAmendOrder:
		pOffset := 0
		orderID, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		pOffset += n
		priceStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		price, _ := udecimal.Parse(priceStr)
		pOffset += n
		sizeStr, _, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		size, _ := udecimal.Parse(sizeStr)
		return &AmendOrderRequest{
			BaseCommand: base,
			OrderID:     orderID,
			NewPrice:    price,
			NewSize:     size,
		}, nil
	case CmdCreateMarket:
		s, _, err := readString(pData)
		if err != nil {
			return nil, err
		}
		minLotSize, _ := udecimal.Parse(s)
		return &CreateMarketRequest{
			BaseCommand: base,
			MinLotSize:  minLotSize,
		}, nil
	case CmdSuspendMarket:
		reason, _, err := readString(pData)
		if err != nil {
			return nil, err
		}
		return &SuspendMarketRequest{
			BaseCommand: base,
			Reason:      reason,
		}, nil
	case CmdResumeMarket:
		return &ResumeMarketRequest{BaseCommand: base}, nil
	case CmdUpdateConfig:
		s, _, err := readString(pData)
		if err != nil {
			return nil, err
		}
		minLotSize, _ := udecimal.Parse(s)
		return &UpdateConfigRequest{
			BaseCommand: base,
			MinLotSize:  minLotSize,
		}, nil
	case CmdUserEvent:
		pOffset := 0
		eventType, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		pOffset += n
		key, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		pOffset += n
		if len(pData[pOffset:]) < payloadLenSize {
			return nil, io.ErrUnexpectedEOF
		}
		dataLen := int(binary.BigEndian.Uint32(pData[pOffset:]))
		pOffset += payloadLenSize
		if len(pData[pOffset:]) < dataLen {
			return nil, io.ErrUnexpectedEOF
		}
		dataBytes := make([]byte, dataLen)
		copy(dataBytes, pData[pOffset:])
		return &UserEventRequest{
			BaseCommand: base,
			EventType:   eventType,
			Key:         key,
			Data:        dataBytes,
		}, nil
	default:
		return nil, errUnknownRequest
	}
}

// QueryType identifies a read-only query handled by the engine.
type QueryType uint8

const (
	// QueryUnknown represents an unknown query type.
	QueryUnknown QueryType = 0
	// QueryGetDepth returns order-book depth.
	QueryGetDepth QueryType = 1
	// QueryGetStats returns order-book statistics.
	QueryGetStats QueryType = 2
	// QuerySnapshot returns in-memory snapshots for all books.
	QuerySnapshot QueryType = 3
)

// Query represents a read-only request against the matching engine.
type Query struct {
	Type     QueryType
	MarketID string
	Payload  any
}

// GetDepthRequest contains parameters for a depth query.
type GetDepthRequest struct {
	Limit uint32
}

// mustWriteString writes a length-prefixed string into buf.
// Callers MUST validate len(s) <= maxUint16Value before calling this function;
// it panics if the string is too long as a last-resort programming-error guard.
func mustWriteString(buf []byte, s string) int {
	if len(s) > maxUint16Value {
		panic(errStringTooLong)
	}
	binary.BigEndian.PutUint16(buf, safeUint16Len(len(s)))
	copy(buf[stringLenSize:], s)
	return stringLenSize + len(s)
}

// readString reads a length-prefixed string from buf.
func readString(buf []byte) (string, int, error) {
	if len(buf) < stringLenSize {
		return "", 0, io.ErrUnexpectedEOF
	}
	length := int(binary.BigEndian.Uint16(buf))
	if len(buf) < stringLenSize+length {
		return "", 0, io.ErrUnexpectedEOF
	}
	return string(buf[stringLenSize : stringLenSize+length]), stringLenSize + length, nil
}

// sideToUint8 converts a side enum to its on-wire binary form.
func sideToUint8(side Side) uint8 {
	switch side {
	case SideBuy:
		return sideBuyValue
	case SideSell:
		return sideSellValue
	default:
		return 0
	}
}

// safeUint16Len converts a validated length into uint16 for wire encoding.
func safeUint16Len(length int) uint16 {
	if length > maxUint16Value {
		panic(errStringTooLong)
	}
	//nolint:gosec // length is bounded by maxUint16Value above.
	return uint16(length)
}

// safeUint32Len converts a validated length into uint32 for wire encoding.
func safeUint32Len(length int) uint32 {
	if length > maxUint32Value {
		panic(errPayloadTooLarge)
	}
	//nolint:gosec // length is bounded by maxUint32Value above.
	return uint32(length)
}
