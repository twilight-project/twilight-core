package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"github.com/twilight-project/twilight-core/x/mining/types"
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

// SlotSettlement is the latest settlement row for one slot: the greatest epoch
// that has a settlement, and that settlement's outcome. Found is false when the
// slot has no settlement row yet.
type SlotSettlement struct {
	SlotID             uint64
	Epoch              uint64
	Finalized          bool
	FinalizationReason string
	Found              bool
}

// LastSlotSettlement returns the greatest-epoch settlement row for slotID. It is
// read with a descending range limited to the first row, so the cost is one
// decode per slot however many epochs that slot has settled — never a walk of
// the slot's history. The app calls it once per ACTIVE slot (coreslot's bounded
// set), so the per-slot settlement export stays bounded by the active-slot count,
// the same discipline this module's TelemetrySnapshot keeps.
//
// The outcome is the row's own Finalized flag and FinalizationReason; the latest
// row may itself be open (not yet finalized), which is the true state to report
// rather than scanning back for the last finalized one (that scan is the
// unbounded read the bound exists to avoid).
func (k Keeper) LastSlotSettlement(ctx context.Context, slotID uint64) (SlotSettlement, error) {
	rng := collections.NewPrefixedPairRange[uint64, uint64](slotID).Descending()
	iter, err := k.Settlements.Iterate(ctx, rng)
	if err != nil {
		return SlotSettlement{}, err
	}
	defer iter.Close()
	if !iter.Valid() {
		return SlotSettlement{SlotID: slotID}, nil
	}
	s, err := iter.Value()
	if err != nil {
		return SlotSettlement{}, err
	}
	return SlotSettlement{
		SlotID:             slotID,
		Epoch:              s.Epoch,
		Finalized:          s.Finalized,
		FinalizationReason: types.SettlementFinalizationReason_name[int32(s.FinalizationReason)],
		Found:              true,
	}, nil
}
