package protocol

import "github.com/quagmt/udecimal"

// DepthDelta is the set of price levels one SDK call changed, carried with the
// depth version the call produced. Each level holds the level's current visible
// size, not a change to add or subtract, so a consumer sets it and never has to
// reason about what was there before.
//
// Seq is the book's depth version after the change. One call that moves visible
// depth advances the version by exactly one, and every depth log of that call
// carries it. A consumer applies a delta when Seq is the version after its own
// and treats Seq as the version it reaches.
type DepthDelta struct {
	Seq  uint64       // Depth version after this change
	Asks []DepthLevel // Changed sell-side levels, price ascending
	Bids []DepthLevel // Changed buy-side levels, price descending
}

// DepthLevel is one changed price level. Size is the level's visible size after
// the delta; zero means the level is gone.
type DepthLevel struct {
	Price udecimal.Decimal
	Size  udecimal.Decimal
}
