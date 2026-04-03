package match

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

func TestMatchingEngine_ContextAwareSubmission(t *testing.T) {
	t.Run("EnqueueCommand_ContextCanceled", func(t *testing.T) {
		// Create engine but don't start it, so the ring buffer will fill up
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		// Fill the ring buffer
		// defaultRingBufferSize is 32768.
		// We need to fill all 32768 slots.
		for i := 0; i < defaultRingBufferSize; i++ {
			err := engine.EnqueueCommand(context.Background(), &protocol.Command{
				CommandID: fmt.Sprintf("fill-%d", i),
			})
			require.NoError(t, err)
		}

		// Now it should be full. Next EnqueueCommand should block.
		// We use a short timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// This call is expected to fail to compile initially once I change the signature,
		// OR I can call it as is and see it block forever (well, until the test times out).
		// But I want to test the NEW signature.
		
		// For TDD, I'll use the new signature here.
		err := engine.EnqueueCommand(ctx, &protocol.Command{
			CommandID: "blocking-command",
		})

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
