package protocol

import "github.com/quagmt/udecimal"

// DepthItem represents a single price level in the order book.
type DepthItem struct {
	ID    uint32           `json:"id"`
	Price udecimal.Decimal `json:"price"`
	Size  udecimal.Decimal `json:"size"`
}

// GetDepthResponse represents the state of the order book depth.
type GetDepthResponse struct {
	UpdateID uint64       `json:"update_id"`
	Asks     []*DepthItem `json:"asks"`
	Bids     []*DepthItem `json:"bids"`
}

// GetStatsResponse contains statistics about the order book queues.
type GetStatsResponse struct {
	AskDepthCount int64 `json:"ask_depth_count"`
	AskOrderCount int64 `json:"ask_order_count"`
	BidDepthCount int64 `json:"bid_depth_count"`
	BidOrderCount int64 `json:"bid_order_count"`
}

// Side represents the order side (Buy/Sell).
type Side int8

const (
	SideBuy  Side = 1
	SideSell Side = 2
)

// OrderType represents the type of order.
type OrderType string

const (
	OrderTypeMarket   OrderType = "market"
	OrderTypeLimit    OrderType = "limit"
	OrderTypeFOK      OrderType = "fok"       // Fill Or Kill
	OrderTypeIOC      OrderType = "ioc"       // Immediate Or Cancel
	OrderTypePostOnly OrderType = "post_only" // Maker only
	OrderTypeCancel   OrderType = "cancel"    // The order has been canceled
)
