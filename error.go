package match

import "errors"

var (
	// ErrInsufficientLiquidity is returned when there is not enough depth to fill a Market/IOC/FOK order.
	ErrInsufficientLiquidity = errors.New("there is not enough depth to fill the order")
	// ErrInvalidParam is returned when a parameter is invalid.
	ErrInvalidParam = errors.New("the param is invalid")
	// ErrInternal is returned when an internal server error occurs.
	ErrInternal = errors.New("internal server error")
	// ErrTimeout is returned when an operation times out.
	ErrTimeout = errors.New("timeout")
	// ErrShutdown is returned when the engine is shutting down.
	ErrShutdown = errors.New("order book is shutting down")
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not found")
	// ErrInvalidPrice is returned when a price is invalid.
	ErrInvalidPrice = errors.New("invalid price")
	// ErrInvalidSize is returned when a size is invalid.
	ErrInvalidSize = errors.New("invalid size")
	// ErrOrderBookClosed is returned when the order book is closed.
	ErrOrderBookClosed = errors.New("order book is closed")
)
