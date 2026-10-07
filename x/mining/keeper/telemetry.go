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
// change and belongs in its own review. The per-slot backlog is read instead from
// the FIRST entry of each slot's prefix (SlotSettlementState), which costs the
// same whatever the backlog's size.
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

// SlotSettlementState is one slot's settlement backlog as plain values: its
// oldest OPEN settlement, and whether that settlement's participant deadline has
// passed.
//
// The oldest open row is the one that matters. A slot can seal every new epoch on
// time and still owe an older one, and the newest row is open by construction for
// most of every epoch, so neither the latest row nor any fixed epoch offset from
// the cursor answers "is this slot behind".
type SlotSettlementState struct {
	SlotID uint64
	// OldestOpenEpoch is the epoch of the slot's oldest open settlement, or 0 when
	// the slot has none. Epochs start at 1, so 0 is never a real epoch.
	OldestOpenEpoch uint64
	// Overdue reports that the oldest open settlement is at or past its
	// participant deadline on the settlement clock: the same predicate the
	// Settlement query returns as permissionless_finalization_now. A pause freezes
	// the clock, so a pause never makes a settlement overdue.
	Overdue bool
}

// SlotSettlementState reads one slot's settlement backlog.
//
// Cost: the first entry of the slot's prefix in OpenSettlementsBySlot, one
// canonical row, and the reads its deadline derives from, however many epochs the
// slot has settled or left open. The app calls it once per ACTIVE slot, so the
// export is bounded by the active set, as the rest of this snapshot is.
//
// The index only nominates the candidate; the canonical row decides. An index
// entry with no row, or naming a row that is already finalized, means the two
// have come apart and is returned as an error rather than exported as a value.
// The index cannot prove absence: a lost entry would hide its row here, which is
// why the Settlement and OpenSettlements queries remain the authority for what a
// slot owes.
func (k Keeper) SlotSettlementState(ctx context.Context, slotID uint64) (SlotSettlementState, error) {
	state := SlotSettlementState{SlotID: slotID}

	iter, err := k.OpenSettlementsBySlot.Iterate(ctx, collections.NewPrefixedPairRange[uint64, uint64](slotID))
	if err != nil {
		return SlotSettlementState{}, err
	}
	defer iter.Close()
	if !iter.Valid() {
		return state, nil
	}
	key, err := iter.Key()
	if err != nil {
		return SlotSettlementState{}, err
	}
	epoch := key.K2()

	settlement, found, err := k.GetSettlement(ctx, slotID, epoch)
	if err != nil {
		return SlotSettlementState{}, err
	}
	if !found {
		return SlotSettlementState{}, types.ErrInvalidState.Wrapf(
			"the open-settlement index lists slot %d in epoch %d, which has no settlement", slotID, epoch)
	}
	if settlement.Finalized {
		return SlotSettlementState{}, types.ErrInvalidState.Wrapf(
			"the open-settlement index lists slot %d in epoch %d, which is finalized", slotID, epoch)
	}

	deadline, err := k.SettlementDeadlineClock(ctx, settlement)
	if err != nil {
		return SlotSettlementState{}, err
	}
	clock, err := k.GetSettlementClock(ctx)
	if err != nil {
		return SlotSettlementState{}, err
	}
	state.OldestOpenEpoch = epoch
	state.Overdue = clock >= deadline
	return state, nil
}
