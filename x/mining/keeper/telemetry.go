package keeper

import (
	"context"
)

// TelemetrySnapshot is the module's consensus clock state as plain values, for
// export as node-local metrics.
//
// It is a READ taken by the app from committed state after a block has
// committed; nothing here is consensus and nothing here is called from the block
// path. Both values are single-item reads.
//
// Deliberately absent: a count of open settlements. The module keeps no counter
// for it, and the only way to derive one is to walk OpenSettlementsBySlot, whose
// size is unbounded precisely when settlements go unsealed — which is the failure
// the metric would be watching for. A walk that grows with the outage it detects
// is not a monitoring path; a maintained counter would be a consensus-state
// change and belongs in its own review.
type TelemetrySnapshot struct {
	// SettlementClock is the canonical monotonic settlement clock. It ticks once
	// per block whose beginning-of-block pause state permits release, so it lags
	// block height by exactly the number of paused blocks.
	SettlementClock uint64
	// LastProcessedRewardEpoch is the materialization cursor: the greatest reward
	// epoch whose settlement set has been materialized. It should track the
	// rewards module's last finalized epoch exactly; a gap is an epoch whose
	// settlements do not exist.
	LastProcessedRewardEpoch uint64
}

// TelemetrySnapshot reads the module's clock state. A read failure is returned
// rather than defaulted, for the same reason the accessors it uses refuse to
// default: a clock reported as zero on a failed read looks like a chain that has
// never released, not like a node that could not read its store.
func (k Keeper) TelemetrySnapshot(ctx context.Context) (TelemetrySnapshot, error) {
	clock, err := k.GetSettlementClock(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	cursor, err := k.GetLastProcessedRewardEpoch(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	return TelemetrySnapshot{SettlementClock: clock, LastProcessedRewardEpoch: cursor}, nil
}
