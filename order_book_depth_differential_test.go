package match

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// dlDiffCommandsPerSeed * len(dlDiffSeeds) is the total command count, which
// must reach 100k.
const dlDiffCommandsPerSeed = 20000

var dlDiffSeeds = []uint64{1, 2, 3, 4, 5, 6}

// dlDiffRequiredKinds are the generator outcomes the run must hit at least once.
// A kind that never occurs means the differential test never checked it.
var dlDiffRequiredKinds = []string{
	"limit", "ioc", "fok", "post_only",
	"market_size", "market_quote", "market_protect",
	"iceberg_place", "replenish_open",
	"cancel_hit", "cancel_stale", "cancel_miss",
	"amend_price", "amend_up", "amend_cap", "amend_noop",
	"amend_down_hidden_only", "amend_down_into_visible", "amend_stale", "amend_match",
	"reject_duplicate", "reject_invalid",
	"snapshot_restore",
	"fok_hidden_fill", "fok_insufficient",
}

var dlDiffRequiredLogTypes = []protocol.LogType{
	protocol.LogTypeOpen, protocol.LogTypeMatch, protocol.LogTypeCancel,
	protocol.LogTypeAmend, protocol.LogTypeReject,
}

// dlRef is the reference depth: a per-level sum built only from
// CalculateDepthChange, the one depth rule. It is checked against the OrderBook's
// own levels, so a wrong rule cannot agree with itself and pass.
type dlRef struct {
	levels map[dlRefKey]dlRefLevel
}

type dlRefKey struct {
	side  Side
	price string
}

type dlRefLevel struct {
	price udecimal.Decimal
	size  udecimal.Decimal
}

func (r *dlRef) apply(t *testing.T, log *OrderBookLog) {
	t.Helper()
	change := CalculateDepthChange(log)
	if change.SizeDiff.IsZero() {
		return
	}
	k := dlRefKey{side: change.Side, price: change.Price.String()}
	cur, ok := r.levels[k]
	if !ok {
		cur = dlRefLevel{price: change.Price, size: udecimal.Zero}
	}
	next := cur.size.Add(change.SizeDiff)
	require.False(t, next.LessThan(udecimal.Zero), "reference level %s went negative", k.price)
	if next.IsZero() {
		delete(r.levels, k)
		return
	}
	cur.size = next
	r.levels[k] = cur
}

// rendered returns one side as "price@size", best level first.
func (r *dlRef) rendered(side Side) []string {
	rows := make([]dlRefLevel, 0, len(r.levels))
	for k, v := range r.levels {
		if k.side == side {
			rows = append(rows, v)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	slices.SortFunc(rows, func(a, b dlRefLevel) int {
		if a.price.Equal(b.price) {
			return 0
		}
		less := a.price.LessThan(b.price)
		if side == Buy {
			less = !less
		}
		if less {
			return -1
		}
		return 1
	})
	out := make([]string, 0, len(rows))
	for _, v := range rows {
		out = append(out, v.price.String()+"@"+v.size.String())
	}
	return out
}

// dlLogKey renders every field a consumer reads, so the snapshot path can be
// compared with the path that never snapshots.
func dlLogKey(l *OrderBookLog) string {
	return fmt.Sprintf(
		"%s seq=%d cmd=%s order=%s side=%d price=%s size=%s old=%s/%s vis=%s maker=%s trade=%d ts=%d",
		l.Type, l.SeqID, l.CommandID, l.OrderID, l.Side, l.Price, l.Size,
		l.OldPrice, l.OldSize, l.VisibleRemoved, l.MakerOrderID, l.TradeID, l.Timestamp,
	)
}

// dlDiffCmd is one generated command. Exactly one of the three is set. The same
// request value is sent to the active book and to the control book.
type dlDiffCmd struct {
	kind    string
	orderID string
	place   *protocol.PlaceOrderRequest
	cancel  *protocol.CancelOrderRequest
	amend   *protocol.AmendOrderRequest
}

func (c dlDiffCmd) run(b *OrderBook) (*LogBatch, error) {
	switch {
	case c.place != nil:
		return b.PlaceOrder(c.place)
	case c.cancel != nil:
		return b.CancelOrder(c.cancel)
	default:
		return b.AmendOrder(c.amend)
	}
}

type dlDiffRun struct {
	t       *testing.T
	seed    uint64
	rng     *rand.Rand
	active  *OrderBook
	control *OrderBook
	ab      *AggregatedBook
	ref     *dlRef
	// builder turns the active book's logs into deltas. Even seeds hand it one
	// LogBatch per command, odd seeds one log at a time, so both forms are checked.
	builder  *DepthDeltaBuilder
	perBatch bool
	delta    protocol.DepthDelta
	ids      []string
	nextID   int
	ts       int64
	kinds    map[string]int
	logs     map[protocol.LogType]int
}

func newDLDiffRun(t *testing.T, seed uint64) *dlDiffRun {
	t.Helper()
	return &dlDiffRun{
		t:        t,
		seed:     seed,
		rng:      rand.New(rand.NewPCG(seed, seed*2+1)), //nolint:gosec // fixed seed: every run replays the same stream
		active:   NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog())),
		control:  NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog())),
		ab:       NewAggregatedBook(),
		ref:      &dlRef{levels: map[dlRefKey]dlRefLevel{}},
		builder:  NewDepthDeltaBuilder(),
		perBatch: seed%2 == 0,
		ts:       1000,
		kinds:    map[string]int{},
		logs:     map[protocol.LogType]int{},
	}
}

func (r *dlDiffRun) rangeInt(lo, hi int64) int64 {
	return lo + r.rng.Int64N(hi-lo+1)
}

func (r *dlDiffRun) base(commandID string) protocol.BaseCommand {
	r.ts++
	return protocol.BaseCommand{
		MarketID:  dlMarket,
		CommandID: fmt.Sprintf("dd-%d-%s-%d", r.seed, commandID, r.ts),
		UserID:    1,
		Timestamp: r.ts,
	}
}

func (r *dlDiffRun) side() Side {
	if r.rng.IntN(2) == 0 {
		return Buy
	}
	return Sell
}

func (r *dlDiffRun) newID() string {
	r.nextID++
	return fmt.Sprintf("o%d-%d", r.seed, r.nextID)
}

func (r *dlDiffRun) pickID() string {
	return r.ids[r.rng.IntN(len(r.ids))]
}

// placeCmd builds a limit-shaped order. About 40% carry a visible size, which
// makes them icebergs.
func (r *dlDiffRun) placeCmd(typ OrderType, kind string) dlDiffCmd {
	id := r.newID()
	size := r.rangeInt(1, 10)
	var visible int64
	if size > 1 && r.rng.IntN(100) < 40 {
		visible = r.rangeInt(1, size-1)
		r.kinds["iceberg_place"]++
	}
	r.ids = append(r.ids, id)
	r.kinds[kind]++
	return dlDiffCmd{
		kind:    kind,
		orderID: id,
		place: &protocol.PlaceOrderRequest{
			BaseCommand: r.base(kind),
			OrderID:     id,
			OrderType:   typ,
			Side:        r.side(),
			Price:       r.writtenPrice(),
			Size:        udecimal.MustFromInt64(size, 0),
			VisibleSize: udecimal.MustFromInt64(visible, 0),
			StepSize:    udecimal.MustFromInt64(1, 0),
		},
	}
}

// writtenPrice returns a price in [lo, hi] written with 0 to 3 trailing zeros, so
// one level arrives under different spellings: 100, 100.0 and 100.000 are one price.
func (r *dlDiffRun) writtenPrice() udecimal.Decimal {
	zeros := r.rng.IntN(4)
	scale := int64(1)
	for range zeros {
		scale *= 10
	}
	return udecimal.MustFromInt64(r.rangeInt(95, 105)*scale, uint8(zeros)) //nolint:gosec // zeros is 0..3
}

// protectionPrice is zero (no bound) half the time, otherwise a level in range.
func (r *dlDiffRun) protectionPrice() udecimal.Decimal {
	if r.rng.IntN(2) == 0 {
		return udecimal.Zero
	}
	r.kinds["market_protect"]++
	return r.writtenPrice()
}

func (r *dlDiffRun) marketSizeCmd() dlDiffCmd {
	id := r.newID()
	r.ids = append(r.ids, id)
	r.kinds["market_size"]++
	return dlDiffCmd{
		kind:    "market_size",
		orderID: id,
		place: &protocol.PlaceOrderRequest{
			BaseCommand: r.base("market_size"),
			OrderID:     id,
			OrderType:   Market,
			Side:        r.side(),
			Price:       r.protectionPrice(),
			Size:        udecimal.MustFromInt64(r.rangeInt(1, 8), 0),
			StepSize:    udecimal.MustFromInt64(1, 0),
		},
	}
}

func (r *dlDiffRun) marketQuoteCmd() dlDiffCmd {
	id := r.newID()
	r.ids = append(r.ids, id)
	r.kinds["market_quote"]++
	return dlDiffCmd{
		kind:    "market_quote",
		orderID: id,
		place: &protocol.PlaceOrderRequest{
			BaseCommand: r.base("market_quote"),
			OrderID:     id,
			OrderType:   Market,
			Side:        r.side(),
			Price:       r.protectionPrice(),
			QuoteSize:   udecimal.MustFromInt64(r.rangeInt(10, 150), 0),
			StepSize:    udecimal.MustFromInt64(1, 0),
		},
	}
}

func (r *dlDiffRun) cancelCmd() dlDiffCmd {
	if len(r.ids) == 0 {
		return r.cancelMissCmd()
	}
	id := r.pickID()
	kind := "cancel_hit"
	if _, ok := r.active.findOrder(id); !ok {
		kind = "cancel_stale"
	}
	r.kinds[kind]++
	return dlDiffCmd{
		kind:    kind,
		orderID: id,
		cancel: &protocol.CancelOrderRequest{
			BaseCommand: r.base(kind),
			OrderID:     id,
		},
	}
}

func (r *dlDiffRun) cancelMissCmd() dlDiffCmd {
	r.kinds["cancel_miss"]++
	return dlDiffCmd{
		kind: "cancel_miss",
		cancel: &protocol.CancelOrderRequest{
			BaseCommand: r.base("cancel_miss"),
			OrderID:     "unknown-" + r.newID(),
		},
	}
}

// amendCmd picks a resting order and chooses an amend from its current state,
// so the hidden-only, into-visible and exact-cap decreases are reached on
// purpose rather than by luck.
func (r *dlDiffRun) amendCmd() dlDiffCmd {
	if len(r.ids) == 0 {
		return r.cancelMissCmd()
	}
	id := r.pickID()
	req := &protocol.AmendOrderRequest{BaseCommand: r.base("amend"), OrderID: id}
	o, ok := r.active.findOrder(id)
	if !ok {
		r.kinds["amend_stale"]++
		req.NewSize = udecimal.MustFromInt64(5, 0)
		return dlDiffCmd{kind: "amend_stale", orderID: id, amend: req}
	}

	total := o.Size.Add(o.HiddenSize)
	hidden := o.HiddenSize
	totalI, _ := total.Int64()
	hiddenI, _ := hidden.Int64()
	var kind string
	switch roll := r.rng.IntN(100); {
	case roll < 20:
		kind = "amend_price"
		req.NewPrice = r.writtenPrice()
	case roll < 35:
		kind = "amend_price"
		req.NewPrice = r.writtenPrice()
		req.NewSize = udecimal.MustFromInt64(r.rangeInt(1, 12), 0)
	case roll < 50:
		kind = "amend_up"
		req.NewSize = udecimal.MustFromInt64(totalI+r.rangeInt(1, 4), 0)
	case roll < 60 && hiddenI > 0 && !o.VisibleLimit.IsZero() && o.VisibleLimit.LessThan(total):
		kind = "amend_cap"
		req.NewSize = o.VisibleLimit
	case roll < 90 && totalI > 1:
		newSize := r.rangeInt(1, totalI-1)
		delta := totalI - newSize
		if hiddenI > 0 && delta <= hiddenI {
			kind = "amend_down_hidden_only"
		} else {
			kind = "amend_down_into_visible"
		}
		req.NewSize = udecimal.MustFromInt64(newSize, 0)
	default:
		kind = "amend_noop"
		req.NewPrice = o.Price
		req.NewSize = total
	}
	r.kinds[kind]++
	return dlDiffCmd{kind: kind, orderID: id, amend: req}
}

// duplicateCmd places a new order under an ID that is still resting.
func (r *dlDiffRun) duplicateCmd(id string) dlDiffCmd {
	r.kinds["reject_duplicate"]++
	return dlDiffCmd{
		kind:    "reject_duplicate",
		orderID: id,
		place: &protocol.PlaceOrderRequest{
			BaseCommand: r.base("reject_duplicate"),
			OrderID:     id,
			OrderType:   Limit,
			Side:        r.side(),
			Price:       udecimal.MustFromInt64(100, 0),
			Size:        udecimal.MustFromInt64(3, 0),
			StepSize:    udecimal.MustFromInt64(1, 0),
		},
	}
}

func (r *dlDiffRun) next() dlDiffCmd {
	switch roll := r.rng.IntN(100); {
	case roll < 25:
		return r.placeCmd(Limit, "limit")
	case roll < 33:
		return r.placeCmd(IOC, "ioc")
	case roll < 39:
		return r.placeCmd(FOK, "fok")
	case roll < 43:
		return r.placeCmd(PostOnly, "post_only")
	case roll < 51:
		return r.marketSizeCmd()
	case roll < 57:
		return r.marketQuoteCmd()
	case roll < 62:
		return r.cancelCmd()
	case roll < 65:
		return r.cancelMissCmd()
	case roll < 90:
		return r.amendCmd()
	case roll < 95:
		// A duplicate ID is only a refusal while the first order still rests.
		if len(r.ids) > 0 {
			id := r.pickID()
			if _, ok := r.active.findOrder(id); ok {
				return r.duplicateCmd(id)
			}
		}
		return r.placeCmd(Limit, "limit")
	default:
		r.kinds["reject_invalid"]++
		id := r.newID()
		return dlDiffCmd{
			kind:    "reject_invalid",
			orderID: id,
			place: &protocol.PlaceOrderRequest{
				BaseCommand: r.base("reject_invalid"),
				OrderID:     id,
				OrderType:   Limit,
				Side:        r.side(),
				Price:       udecimal.Zero,
				Size:        udecimal.MustFromInt64(3, 0),
				StepSize:    udecimal.MustFromInt64(1, 0),
			},
		}
	}
}

// snapshotAndRestore moves the active side onto a fresh OrderBook restored from a
// snapshot, and a fresh AggregatedBook seeded from the same depth. The control book
// keeps running without the snapshot.
func (r *dlDiffRun) snapshotAndRestore() {
	r.t.Helper()
	snap := r.active.Snapshot()
	restored := NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog()))
	restored.Restore(snap)

	depth := r.active.GetDepth(dlDepthLimit)
	ab := NewAggregatedBook()
	require.NoError(r.t, ab.ApplySnapshot(snapshotFromDepth(r.t, r.active.SeqID(), depth.Asks, depth.Bids)))
	r.active, r.ab = restored, ab
	r.kinds["snapshot_restore"]++
}

// fokFeasibility reads the resting book before a FOK runs. It returns the size the
// FOK can take counting hidden iceberg size, and the size visible size alone
// covers. Both are brute-force sums over the resting orders, not the book's own
// level totals, so the book's accounting is what gets checked.
func (r *dlDiffRun) fokFeasibility(req *protocol.PlaceOrderRequest) (total, visible udecimal.Decimal) {
	target := r.active.askQueue
	if req.Side == Sell {
		target = r.active.bidQueue
	}
	total, visible = udecimal.Zero, udecimal.Zero
	for _, o := range target.orders {
		if req.Side == Buy && req.Price.LessThan(o.Price) {
			continue
		}
		if req.Side == Sell && req.Price.GreaterThan(o.Price) {
			continue
		}
		total = total.Add(o.Size).Add(o.HiddenSize)
		visible = visible.Add(o.Size)
	}
	return total, visible
}

// checkFOK holds a FOK to the brute-force sum: it fills in full when the resting
// visible plus hidden size covers it, and is refused, with no fill, when it does
// not.
func (r *dlDiffRun) checkFOK(
	desc string,
	req *protocol.PlaceOrderRequest,
	total, visible, filled udecimal.Decimal,
	reject bool,
) {
	size := req.Size
	if total.GreaterThanOrEqual(size) {
		require.False(r.t, reject, "FOK covered by visible+hidden size was refused, %s", desc)
		require.True(
			r.t,
			filled.Equal(size),
			"FOK covered by visible+hidden size filled %s of %s, %s",
			filled,
			size,
			desc,
		)
		if visible.LessThan(size) {
			r.kinds["fok_hidden_fill"]++
		}
		return
	}
	require.True(r.t, reject, "FOK short of visible+hidden size was not refused, %s", desc)
	require.True(r.t, filled.IsZero(), "refused FOK filled %s, %s", filled, desc)
	r.kinds["fok_insufficient"]++
}

// execute sends one command to both books and checks, after it, that the
// snapshot path logged exactly what the control logged, that the delta the
// builder settles applies to the AggregatedBook, and that the AggregatedBook, the
// reference and the OrderBook agree on every level and on the version.
func (r *dlDiffRun) execute(step int, cmd dlDiffCmd) {
	t := r.t
	desc := fmt.Sprintf("seed=%d step=%d kind=%s order=%s", r.seed, step, cmd.kind, cmd.orderID)

	var fok *protocol.PlaceOrderRequest
	var fokTotal, fokVisible udecimal.Decimal
	if cmd.place != nil && cmd.place.OrderType == FOK {
		fok = cmd.place
		fokTotal, fokVisible = r.fokFeasibility(fok)
	}

	versionBefore := r.active.SeqID()
	batch, err := cmd.run(r.active)
	require.NoError(t, err, desc)
	ctrl, err := cmd.run(r.control)
	require.NoError(t, err, desc)

	require.Len(t, ctrl.Logs, len(batch.Logs), "control log count, %s", desc)
	for i := range batch.Logs {
		require.Equal(t, dlLogKey(ctrl.Logs[i]), dlLogKey(batch.Logs[i]),
			"log %d differs between snapshot path and control, %s", i, desc)
	}

	sawMatch := false
	movesDepth := false
	filled := udecimal.Zero
	reject := false
	for _, l := range batch.Logs {
		r.logs[l.Type]++
		if !CalculateDepthChange(l).SizeDiff.IsZero() {
			movesDepth = true
		}
		switch l.Type {
		case protocol.LogTypeMatch:
			sawMatch = true
			if fok != nil {
				filled = filled.Add(l.Size)
			}
		case protocol.LogTypeReject:
			reject = true
		case protocol.LogTypeOpen:
			if l.OrderID != cmd.orderID {
				r.kinds["replenish_open"]++
			}
		default:
			// Cancels, amends and refusals need no tally here.
		}
		r.ref.apply(t, l)
	}
	if !r.perBatch {
		for _, l := range batch.Logs {
			r.builder.Add(l)
		}
	} else {
		r.builder.AddBatch(batch.Logs)
	}
	if sawMatch && cmd.amend != nil {
		r.kinds["amend_match"]++
	}
	if fok != nil {
		r.checkFOK(desc, fok, fokTotal, fokVisible, filled, reject)
	}

	// The version moves exactly when the command moves visible depth. A shrink
	// inside the hidden size, a refusal and a no-op amend all leave it alone, and
	// only a command that moves it produces a delta.
	changed := r.builder.Flush(r.active, &r.delta)
	r.builder.Reset()
	require.Equal(t, movesDepth, changed, "a delta exists iff visible depth moves, %s", desc)
	require.Equal(t, movesDepth, r.active.SeqID() != versionBefore,
		"depth version must move iff visible depth moves, %s", desc)
	if changed {
		require.NoError(t, r.ab.ApplyDelta(&r.delta), "apply delta, %s", desc)
		require.Equal(t, r.active.SeqID(), r.delta.Seq, "delta ends at the book version, %s", desc)
	}

	want := r.active.GetDepth(dlDepthLimit)
	got := r.ab.GetDepth(dlDepthLimit)
	require.Equal(t, want.UpdateID, got.UpdateID, "depth version, %s", desc)
	require.Equal(t, r.active.SeqID(), r.ab.SequenceID(), "sequence id, %s", desc)
	require.Equal(t, dlLevels(want.Asks), dlLevels(got.Asks), "AggregatedBook asks, %s", desc)
	require.Equal(t, dlLevels(want.Bids), dlLevels(got.Bids), "AggregatedBook bids, %s", desc)
	require.Equal(t, dlLevels(want.Asks), r.ref.rendered(Sell), "reference asks, %s", desc)
	require.Equal(t, dlLevels(want.Bids), r.ref.rendered(Buy), "reference bids, %s", desc)

	ctrlDepth := r.control.GetDepth(dlDepthLimit)
	require.Equal(t, want.UpdateID, ctrlDepth.UpdateID, "control version, %s", desc)
	require.Equal(t, dlLevels(ctrlDepth.Asks), dlLevels(want.Asks), "control asks, %s", desc)
	require.Equal(t, dlLevels(ctrlDepth.Bids), dlLevels(want.Bids), "control bids, %s", desc)
}

func (r *dlDiffRun) run(total int) {
	for step := 1; step <= total; step++ {
		if step == total/2 {
			r.snapshotAndRestore()
		}
		r.execute(step, r.next())
	}
}

// dlCoverage merges the per-seed counts. The parent test checks them once every
// seed has finished.
type dlCoverage struct {
	mu       sync.Mutex
	commands int
	kinds    map[string]int
	logs     map[protocol.LogType]int
}

func (c *dlCoverage) add(r *dlDiffRun, commands int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands += commands
	for k, v := range r.kinds {
		c.kinds[k] += v
	}
	for k, v := range r.logs {
		c.logs[k] += v
	}
}

func (c *dlCoverage) require(t *testing.T) {
	t.Helper()
	require.GreaterOrEqual(t, c.commands, 100000, "differential run must cover at least 100k commands")
	for _, k := range dlDiffRequiredKinds {
		require.Positive(t, c.kinds[k], "command kind %q never generated", k)
	}
	for _, lt := range dlDiffRequiredLogTypes {
		require.Positive(t, c.logs[lt], "log type %s never emitted", lt)
	}
}

// TestDepthLogDifferential drives OrderBook with fixed-seed random command streams
// and checks, after every command, that the AggregatedBook and a reference built
// from CalculateDepthChange equal the OrderBook's own levels and version. A
// snapshot is restored in the middle of each stream, and the restored book must
// log exactly what a book that never snapshots logs.
//
// The seeds run one after another, and the coverage check runs in the body. A
// coverage check in t.Cleanup, after parallel subtests, let a failing seed's
// output be lost from go test's report, so a failure looked like a bare FAIL.
func TestDepthLogDifferential(t *testing.T) {
	cov := &dlCoverage{kinds: map[string]int{}, logs: map[protocol.LogType]int{}}

	for _, seed := range dlDiffSeeds {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			r := newDLDiffRun(t, seed)
			r.run(dlDiffCommandsPerSeed)
			cov.add(r, dlDiffCommandsPerSeed)
		})
	}

	cov.require(t)
}
