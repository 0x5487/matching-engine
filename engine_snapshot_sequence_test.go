package match

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTakeSnapshotNeedsATimestamp: a snapshot without a logical timestamp is refused
// before anything is taken or written. There is no fallback value.
func TestTakeSnapshotNeedsATimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ts   []int64
	}{
		{name: "no timestamp", ts: nil},
		{name: "zero timestamp", ts: []int64{0}},
		{name: "negative timestamp", ts: []int64{-1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := setupEngine(t, NewMemoryPublishLog())
			addBook(t, engine, "BTC-USDT")
			dir := filepath.Join(t.TempDir(), "snap")

			meta, err := engine.TakeSnapshot(context.Background(), dir, tt.ts...)
			require.ErrorIs(t, err, ErrInvalidParam)
			assert.Nil(t, meta)
			assert.NoDirExists(t, dir)
			assert.NoDirExists(t, dir+".tmp")
		})
	}
}

// TestSnapshotCarriesNoUpstreamSequence: the files hold the logical timestamp the
// caller gave and no upstream command sequence. The raw metadata keys are asserted,
// not the struct, so a renamed or leftover tag cannot hide behind the Go field.
func TestSnapshotCarriesNoUpstreamSequence(t *testing.T) {
	t.Parallel()

	engine := setupEngine(t, NewMemoryPublishLog())
	addBook(t, engine, "BTC-USDT")
	dir := filepath.Join(t.TempDir(), "snap")

	meta, err := engine.TakeSnapshot(context.Background(), dir, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), meta.Timestamp)

	rawMeta, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "metadata.json")))
	require.NoError(t, err)
	var keys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawMeta, &keys))
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	assert.ElementsMatch(t,
		[]string{"schema_version", "timestamp", "engine_version", "snapshot_checksum"},
		got,
	)

	bin, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "snapshot.bin")))
	require.NoError(t, err)
	assert.NotContains(t, string(bin), "last_cmd_seq_id")
}
