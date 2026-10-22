package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarshalRequest_FrameHasNoUpstreamSequence pins the frame on its raw bytes: the
// command type is followed directly by the timestamp, with no upstream sequence field
// between them. Decoding the same bytes gives back the request.
func TestMarshalRequest_FrameHasNoUpstreamSequence(t *testing.T) {
	t.Parallel()

	req := &CancelOrderRequest{
		BaseCommand: BaseCommand{CommandID: "c", UserID: 1, MarketID: "M", Timestamp: 1},
		OrderID:     "o",
	}
	want := []byte{
		0x01,                   // version
		0, 0, 0, 0, 0, 0, 0, 1, // user id
		byte(CmdCancelOrder),   // command type
		0, 0, 0, 0, 0, 0, 0, 1, // timestamp, at offset 10
		0, 1, 'M', // market id
		0, 1, 'c', // command id
		0, 0, 0, 3, // payload length
		0, 1, 'o', // order id
	}

	got, err := MarshalRequest(req)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	decoded, err := UnmarshalRequest(want)
	require.NoError(t, err)
	assert.Equal(t, req, decoded)
}
