package match

import "errors"

var (
	ErrInsufficientLiquidity = errors.New("there is not enough depth to fill the order")
	ErrInvalidParam          = errors.New("the param is invalid")
	ErrInternal              = errors.New("internal server error")
	ErrTimeout               = errors.New("timeout")
	ErrShutdown              = errors.New("order book is shutting down")
	ErrNotFound              = errors.New("not found")
)
