package keeper

import (
	"context"

	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

// TelemetrySnapshot is the validator-set state as plain values, for export as
// node-local metrics.
//
// It is a READ taken by the app from committed state after a block has
// committed. Nothing here is consensus, nothing here emits a ValidatorUpdate, and
// nothing here is called from the block path.
//
// Cost: two walks bounded by the active set. The active-slot index is bounded by
// HardMaxActiveCoreSlots, and a pending rotation exists only for an ACTIVE slot
// (every lifecycle transition cancels it), so both are the same walks the
// EndBlocker already performs every block. Neither decodes more than it counts.
type TelemetrySnapshot struct {
	ActiveSlots    uint64
	MinActiveSlots uint64
	MaxActiveSlots uint64
	// PendingKeyRotations counts queued consensus-key rotations that have not
	// yet reached their effective height.
	PendingKeyRotations uint64
	// PrimaryNominationPending and EmergencyNominationPending report whether an
	// authority handover is open for the role: nominated, not yet accepted or
	// canceled.
	PrimaryNominationPending   bool
	EmergencyNominationPending bool
}

// TelemetrySnapshot reads the validator-set state. A read failure is returned
// rather than defaulted: an active count of zero on a failed read would look
// like an empty validator set.
func (k Keeper) TelemetrySnapshot(ctx context.Context) (TelemetrySnapshot, error) {
	var snap TelemetrySnapshot

	params, err := k.Params.Get(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.MinActiveSlots = params.MinActiveSlots
	snap.MaxActiveSlots = params.MaxActiveSlots

	snap.ActiveSlots, err = k.activeCount(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}

	if err := k.Rotations.Walk(ctx, nil, func(_ uint64, _ types.PendingKeyRotation) (bool, error) {
		snap.PendingKeyRotations++
		return false, nil
	}); err != nil {
		return TelemetrySnapshot{}, err
	}

	snap.PrimaryNominationPending, err = k.PendingAuthority.Has(ctx, int32(types.AuthorityRole_AUTHORITY_ROLE_PRIMARY))
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.EmergencyNominationPending, err = k.PendingAuthority.Has(ctx, int32(types.AuthorityRole_AUTHORITY_ROLE_EMERGENCY))
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	return snap, nil
}
