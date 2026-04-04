package match

import (
	"context"

	"github.com/0x5487/matching-engine/protocol"
)

func submitCreateMarket(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.CreateMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitPlaceOrder(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.PlaceOrderParams,
) error {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}

func submitCancelOrder(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.CancelOrderParams,
) error {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}

func submitSuspendMarket(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.SuspendMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitResumeMarket(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.ResumeMarketParams,
) (*Future[any], error) {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.Submit(ctx, cmd)
}

func submitUserEvent(
	ctx context.Context,
	engine *MatchingEngine,
	params *protocol.UserEventParams,
) error {
	cmd := &protocol.Command{
		CommandID: params.CommandID,
		MarketID:  params.MarketID,
		Timestamp: params.Timestamp,
	}
	_ = cmd.SetPayload(params)
	return engine.SubmitAsync(ctx, cmd)
}
