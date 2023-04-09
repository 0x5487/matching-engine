package match

import "errors"

var (
	ErrCanceled              = errors.New("the order has been canceled")
	ErrInsufficientLiquidity = errors.New("there is not enough depth to fill the order")
	ErrInvalidParam          = errors.New("the param is invalid")
	ErrInternal              = errors.New("internal server error")
)
