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
	errInvalidDecimal  = errors.New("invalid decimal")
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
	// StepSize is the unit of account for the base asset: a base balance has to be
	// a whole number of steps or it cannot be reconciled against the ledger, so a
	// quote-sized market order has to be floored to it. It travels on the order
	// rather than living on the book so that a replay cannot disagree with itself
	// about which unit was in force, and so that no producer can choose a unit the
	// market does not use.
	StepSize udecimal.Decimal `json:"step_size"`
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
	// StepSize is the unit NewSize must be a whole number of. Zero means no
	// unit is given and NewSize is not checked, as for a new order.
	StepSize udecimal.Decimal `json:"step_size"`
}

// Base returns the embedded BaseCommand.
func (r *AmendOrderRequest) Base() BaseCommand { return r.BaseCommand }

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
		// side(1) + orderType(1) + orderID(string) + price(string) + size(string) + visibleSize(string) + quoteSize(string) + stepSize(string)
		priceStr := r.Price.String()
		sizeStr := r.Size.String()
		visStr := r.VisibleSize.String()
		quoteStr := r.QuoteSize.String()
		stepStr := r.StepSize.String()
		pSize := sideFieldSize + orderTypeSize + stringLenSize*6 + len(
			r.OrderID,
		) + len(
			priceStr,
		) + len(
			sizeStr,
		) + len(
			visStr,
		) + len(
			quoteStr,
		) + len(
			stepStr,
		)
		buf := make([]byte, pSize)
		offset := 0
		buf[offset] = sideToUint8(r.Side)
		offset++
		buf[offset] = r.OrderType.ToUint8()
		offset++
		n, err := writeString(buf[offset:], r.OrderID)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], priceStr)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], sizeStr)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], visStr)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], quoteStr)
		if err != nil {
			return nil, err
		}
		offset += n
		_, err = writeString(buf[offset:], stepStr)
		if err != nil {
			return nil, err
		}
		payload = buf
	case *CancelOrderRequest:
		wireType = CmdCancelOrder
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.OrderID) > maxUint16Value {
			return nil, errStringTooLong
		}
		buf := make([]byte, stringLenSize+len(r.OrderID))
		_, err := writeString(buf, r.OrderID)
		if err != nil {
			return nil, err
		}
		payload = buf
	case *AmendOrderRequest:
		wireType = CmdAmendOrder
		// Validate body string fields before building the buffer to avoid panicking.
		if len(r.OrderID) > maxUint16Value {
			return nil, errStringTooLong
		}
		priceStr := r.NewPrice.String()
		sizeStr := r.NewSize.String()
		stepStr := r.StepSize.String()
		buf := make([]byte, stringLenSize*4+len(r.OrderID)+len(priceStr)+len(sizeStr)+len(stepStr))
		offset := 0
		n, err := writeString(buf[offset:], r.OrderID)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], priceStr)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], sizeStr)
		if err != nil {
			return nil, err
		}
		offset += n
		_, err = writeString(buf[offset:], stepStr)
		if err != nil {
			return nil, err
		}
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
		n, err := writeString(buf[offset:], r.EventType)
		if err != nil {
			return nil, err
		}
		offset += n
		n, err = writeString(buf[offset:], r.Key)
		if err != nil {
			return nil, err
		}
		offset += n
		l, err := safeUint32Len(len(r.Data))
		if err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(buf[offset:], l)
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

	n, err := writeString(buf[offset:], base.MarketID)
	if err != nil {
		return nil, err
	}
	offset += n
	n, err = writeString(buf[offset:], base.CommandID)
	if err != nil {
		return nil, err
	}
	offset += n

	// payloadSize is non-negative and fits in uint32.
	/* #nosec G115 */
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
		/* #nosec G115 */
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
		price, err := udecimal.Parse(priceStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		sizeStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		size, err := udecimal.Parse(sizeStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		visStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		visibleSize, err := udecimal.Parse(visStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		quoteStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		quoteSize, err := udecimal.Parse(quoteStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		stepStr, _, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		stepSize, err := udecimal.Parse(stepStr)
		if err != nil {
			return nil, errInvalidDecimal
		}

		return &PlaceOrderRequest{
			BaseCommand: base,
			OrderID:     orderID,
			Side:        side,
			OrderType:   orderType,
			Price:       price,
			Size:        size,
			VisibleSize: visibleSize,
			QuoteSize:   quoteSize,
			StepSize:    stepSize,
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
		price, err := udecimal.Parse(priceStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		sizeStr, n, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		size, err := udecimal.Parse(sizeStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		pOffset += n
		stepStr, _, err := readString(pData[pOffset:])
		if err != nil {
			return nil, err
		}
		stepSize, err := udecimal.Parse(stepStr)
		if err != nil {
			return nil, errInvalidDecimal
		}
		return &AmendOrderRequest{
			BaseCommand: base,
			OrderID:     orderID,
			NewPrice:    price,
			NewSize:     size,
			StepSize:    stepSize,
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

// writeString writes a length-prefixed string into buf.
func writeString(buf []byte, s string) (int, error) {
	l, err := safeUint16Len(len(s))
	if err != nil {
		return 0, err
	}
	binary.BigEndian.PutUint16(buf, l)
	copy(buf[stringLenSize:], s)
	return stringLenSize + len(s), nil
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
func safeUint16Len(length int) (uint16, error) {
	if length > maxUint16Value {
		return 0, errStringTooLong
	}
	//nolint:gosec // length is bounded by maxUint16Value above.
	return uint16(length), nil
}

// safeUint32Len converts a validated length into uint32 for wire encoding.
func safeUint32Len(length int) (uint32, error) {
	if length > maxUint32Value {
		return 0, errPayloadTooLarge
	}
	//nolint:gosec // length is bounded by maxUint32Value above.
	return uint32(length), nil
}
