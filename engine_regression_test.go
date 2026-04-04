package match

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

func TestManagement_UpdateConfig_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-TEST"
	ctx := context.Background()

	// 1. Create market first
	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
		CommandID:  "config-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "1.0",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed UpdateConfig
	// We manually construct a command with a malformed payload
	protoCmd := &protocol.Command{
		Type:      protocol.CmdUpdateConfig,
		MarketID:  marketID,
		CommandID: "malformed-config",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang and should return an error
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err, "Should return error for malformed payload")
}

func TestManagement_SuspendMarket_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-SUSPEND"
	ctx := context.Background()

	// 1. Create market first
	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
		CommandID:  "config-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "1.0",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed SuspendMarket
	protoCmd := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		MarketID:  marketID,
		CommandID: "malformed-suspend",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err)
}

func TestManagement_ResumeMarket_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-RESUME"
	ctx := context.Background()

	// 1. Create market first
	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
		CommandID:  "config-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "1.0",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed ResumeMarket
	protoCmd := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		MarketID:  marketID,
		CommandID: "malformed-resume",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err)
}
