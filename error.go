package engine

import "errors"

var (
	ErrCanceled              = errors.New("the order has been canceled")
	ErrInsufficientLiquidity = errors.New("there is not enough depth to fill the order")
)
