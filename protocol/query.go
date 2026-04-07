package protocol

// BaseQuery contains shared metadata for all query requests.
type BaseQuery struct {
	MarketID string `json:"market_id"`
}

// GetDepthQuery contains parameters for a depth query.
type GetDepthQuery struct {
	BaseQuery

	Limit uint32 `json:"limit"`
}

// GetStatsQuery contains parameters for an order book statistics query.
type GetStatsQuery struct {
	BaseQuery
}
