package match

import (
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

// OrderBookSnapshot contains the full state of a single OrderBook.
type OrderBookSnapshot struct {
	MarketID string `json:"market_id"`
	// SeqID is the book depth version this snapshot was taken at. It advances once
	// per SDK call that moves visible depth, so restoring this value lets the book
	// resume the version a downstream reader is already following.
	//
	// A snapshot written before that rule advanced once per depth log, so its SeqID
	// is ahead of what a depth reader expects. Restore accepts such a snapshot - the
	// schema version is unchanged and nothing validates it - and the first depth
	// call after recovery reports a jump, which costs a downstream reader one
	// rebuild. Discard snapshots taken before the upgrade, or reset that reader; see
	// the CHANGELOG.
	SeqID   uint64   `json:"seq_id"`
	TradeID uint64   `json:"trade_id"` // Current Trade sequence ID
	Bids    []*Order `json:"bids"`     // Ordered list of bids (best price first)
	Asks    []*Order `json:"asks"`     // Ordered list of asks (best price first)
}

// SnapshotMetadata holds the global metadata for a snapshot (stored in metadata.json).
type SnapshotMetadata struct {
	SchemaVersion    int    `json:"schema_version"`    // Snapshot schema version for backward compatibility
	Timestamp        int64  `json:"timestamp"`         // Logical timestamp the caller gave; required, never derived
	EngineVersion    string `json:"engine_version"`    // Engine version
	SnapshotChecksum uint32 `json:"snapshot_checksum"` // CRC32 of the entire snapshot.bin file
}

// SnapshotFileFooter is the footer structure stored at the end of snapshot.bin.
// Layout: [BinaryData...][FooterJSON][FooterLength(4 bytes)].
type SnapshotFileFooter struct {
	Markets []MarketSegment `json:"markets"` // Index of market data in this file
}

// MarketSegment contains metadata for a specific market's data within the snapshot binary file.
type MarketSegment struct {
	MarketID string `json:"market_id"`
	Offset   int64  `json:"offset"`   // Start offset in snapshot.bin (relative to file start)
	Length   int64  `json:"length"`   // Length in bytes
	Checksum uint32 `json:"checksum"` // CRC32 Checksum of this segment
}

// calculateFileCRC32 calculates the CRC32 checksum of a file.
func calculateFileCRC32(filePath string) (uint32, error) {
	f, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	hash := crc32.NewIEEE()
	if _, err := io.Copy(hash, f); err != nil {
		return 0, err
	}
	return hash.Sum32(), nil
}
