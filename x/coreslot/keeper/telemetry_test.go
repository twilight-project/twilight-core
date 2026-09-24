package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/twilight-project/twilight-core/x/coreslot/keeper"
	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

func TestTelemetrySnapshotCountsTheValidatorSetState(t *testing.T) {
	k, ctx, authority, emergency := setup(t)
	params := types.DefaultParams(authority, emergency)
	require.NoError(t, k.Params.Set(ctx, params))
	for _, id := range []uint64{1, 2, 3} {
		require.NoError(t, k.ActiveSlots.Set(ctx, id))
	}
	require.NoError(t, k.Rotations.Set(ctx, 2, types.PendingKeyRotation{SlotId: 2, EffectiveHeight: 50}))
	require.NoError(t, k.PendingAuthority.Set(ctx,
		int32(types.AuthorityRole_AUTHORITY_ROLE_EMERGENCY),
		types.PendingAuthorityTransfer{Nominee: authority, NominatedHeight: 1}))

	snap, err := k.TelemetrySnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, keeper.TelemetrySnapshot{
		ActiveSlots:                3,
		MinActiveSlots:             params.MinActiveSlots,
		MaxActiveSlots:             params.MaxActiveSlots,
		PendingKeyRotations:        1,
		PrimaryNominationPending:   false,
		EmergencyNominationPending: true,
	}, snap)
}

// A read failure is an error, never a snapshot that looks like an empty set.
func TestTelemetrySnapshotRefusesMissingParams(t *testing.T) {
	k, ctx, _, _ := setup(t)
	_, err := k.TelemetrySnapshot(ctx)
	require.Error(t, err)
}
