package protocol

type DepthItem struct {
	Price string `json:"price"`
	Size  string `json:"size"`
	Count int64  `json:"count"`
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

// LogType represents the type of event log.
type LogType string

const (
	LogTypeOpen   LogType = "open"
	LogTypeMatch  LogType = "match"
	LogTypeCancel LogType = "cancel"
	LogTypeAmend  LogType = "amend"
	LogTypeReject LogType = "reject"
)

// RejectReason represents the reason why an order was rejected.
type RejectReason string

const (
	RejectReasonNone             RejectReason = ""
	RejectReasonNoLiquidity      RejectReason = "no_liquidity"      // Market/IOC/FOK: No orders available to match
	RejectReasonPriceMismatch    RejectReason = "price_mismatch"    // IOC/FOK: Price does not meet requirements
	RejectReasonInsufficientSize RejectReason = "insufficient_size" // FOK: Cannot be fully filled
	RejectReasonPostOnlyMatch    RejectReason = "post_only_match"   // PostOnly: Would match immediately
	RejectReasonDuplicateID      RejectReason = "duplicate_order_id"
	RejectReasonOrderNotFound    RejectReason = "order_not_found"
	RejectReasonInvalidPayload   RejectReason = "invalid_payload"
)
