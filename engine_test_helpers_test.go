package match

import (
	"context"

	"github.com/0x5487/matching-engine/protocol"
)

func submitCreateMarket(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.CreateMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitPlaceOrder(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.PlaceOrderParams,
) error {
	cmd := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}

func submitCancelOrder(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.CancelOrderParams,
) error {
	cmd := &protocol.Command{
		Type:      protocol.CmdCancelOrder,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}

func submitSuspendMarket(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.SuspendMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitResumeMarket(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.ResumeMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitUserEvent(
	ctx context.Context,
	engine *MatchingEngine,
	userID uint64,
	marketID string,
	commandID string,
	timestamp int64,
	params *protocol.UserEventParams,
) error {
	cmd := &protocol.Command{
		Type:      protocol.CmdUserEvent,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}
