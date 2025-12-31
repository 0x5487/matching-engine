package match

import (
	"hash/crc32"
	"io"
	"os"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

// OrderBookSnapshot contains the full state of a single OrderBook.
type OrderBookSnapshot struct {
	MarketID     string                  `json:"market_id"`
	SeqID        uint64                  `json:"seq_id"`          // Current BookLog sequence ID
	LastCmdSeqID uint64                  `json:"last_cmd_seq_id"` // Last processed command sequence ID from MQ
	TradeID      uint64                  `json:"trade_id"`        // Current Trade sequence ID
	Bids         []*Order                `json:"bids"`            // Ordered list of bids (best price first)
	Asks         []*Order                `json:"asks"`            // Ordered list of asks (best price first)
	State        protocol.OrderBookState `json:"state"`
	MinLotSize   udecimal.Decimal        `json:"min_lot_size"`
}

// SnapshotMetadata holds the global metadata for a snapshot (stored in metadata.json).
type SnapshotMetadata struct {
	SchemaVersion      int    `json:"schema_version"`         // Snapshot schema version for backward compatibility
	Timestamp          int64  `json:"timestamp"`              // Unix Nano
	GlobalLastCmdSeqID uint64 `json:"global_last_cmd_seq_id"` // Global MQ offset to resume from (max of all markets)
	EngineVersion      string `json:"engine_version"`         // Engine version
	SnapshotChecksum   uint32 `json:"snapshot_checksum"`      // CRC32 of the entire snapshot.bin file
}

// SnapshotFileFooter is the footer structure stored at the end of snapshot.bin.
// Layout: [BinaryData...][FooterJSON][FooterLength(4 bytes)]
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
	f, err := os.Open(filePath)
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
