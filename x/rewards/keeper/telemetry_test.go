package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// The snapshot is what an alert watches, so every field is pinned to seeded
// state rather than to another derivation of the same numbers.
func TestTelemetrySnapshotReflectsCommittedState(t *testing.T) {
	params := types.DefaultParams()
	k, ctx, bank := setupAccountingKeeper(t, &coreSlotKeeperMock{}, 1, params)

	state, err := k.GetState(ctx)
	require.NoError(t, err)
	state.CumulativeEmitted = "1000"
	state.CarryForwardRemainder = "10"
	require.NoError(t, k.SetState(ctx, state))
	require.NoError(t, k.SetOutstandingEntitlementLiability(ctx, sdkmath.NewInt(40)))
	bank.credit(moduleAccountAddress(), sdk.NewCoins(sdk.NewInt64Coin(params.NativeDenom, 50)))
	require.NoError(t, k.SetOpenRewardEnabledBlocks(ctx, 7))
	require.NoError(t, k.SchedulePauseTransition(ctx, 1, true))
	// Two finalized epochs, written out of order: the snapshot must report the
	// greatest, not the last written or the first found.
	require.NoError(t, k.FinalizedEpochs.Set(ctx, 7, types.EpochReward{EpochNumber: 7}))
	require.NoError(t, k.FinalizedEpochs.Set(ctx, 3, types.EpochReward{EpochNumber: 3}))

	snap, err := k.TelemetrySnapshot(ctx.WithBlockHeight(5))
	require.NoError(t, err)

	maxSupply, ok := sdkmath.NewIntFromString(params.MaxSupply)
	require.True(t, ok)
	require.Equal(t, uint64(1), snap.CurrentEpoch)
	require.Equal(t, uint64(1), snap.CurrentEpochStartHeight)
	require.Equal(t, params.EpochLengthBlocks, snap.CurrentEpochEndHeight)
	require.Equal(t, params.EpochLengthBlocks-5, snap.BlocksRemainingInEpoch)
	require.Equal(t, uint64(7), snap.OpenRewardEnabledBlocks)
	require.Equal(t, uint64(7), snap.LastFinalizedEpoch)
	require.Equal(t, "1000", snap.CumulativeEmitted.String())
	require.Equal(t, maxSupply.String(), snap.MaxSupply.String())
	require.Equal(t, uint64(0), snap.HalvingTier)
	require.Equal(t, maxSupply.QuoRaw(2).String(), snap.NextHalvingThreshold.String())
	require.Equal(t, "50", snap.EscrowBalance.String())
	require.Equal(t, "40", snap.OutstandingLiability.String())
	require.Equal(t, "10", snap.CarryForwardRemainder.String())
	require.Equal(t, "0", snap.EscrowSolvencyDelta.String())
	require.False(t, snap.Paused)
	require.True(t, snap.PauseTransitionPending)
	require.True(t, snap.ReleaseEnabled)
}

// A shortfall or a surplus shows up as a signed delta, computed exactly.
func TestTelemetrySnapshotReportsEscrowImbalanceAsASignedDelta(t *testing.T) {
	params := types.DefaultParams()
	k, ctx, bank := setupAccountingKeeper(t, &coreSlotKeeperMock{}, 1, params)
	require.NoError(t, k.SetOutstandingEntitlementLiability(ctx, sdkmath.NewInt(40)))
	bank.credit(moduleAccountAddress(), sdk.NewCoins(sdk.NewInt64Coin(params.NativeDenom, 25)))

	snap, err := k.TelemetrySnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, "-15", snap.EscrowSolvencyDelta.String())
}

// No finalized epoch is reported as zero, and the closing block itself reports
// no blocks remaining.
func TestTelemetrySnapshotOnTheClosingBlock(t *testing.T) {
	params := types.DefaultParams()
	k, ctx, _ := setupAccountingKeeper(t, &coreSlotKeeperMock{}, 1, params)

	snap, err := k.TelemetrySnapshot(ctx.WithBlockHeight(int64(params.EpochLengthBlocks)))
	require.NoError(t, err)
	require.Equal(t, uint64(0), snap.BlocksRemainingInEpoch)
	require.Equal(t, uint64(0), snap.LastFinalizedEpoch)
}

// A read failure is an error, never a snapshot full of zeros.
func TestTelemetrySnapshotRefusesUninitializedState(t *testing.T) {
	k, ctx, _ := setupKeeper(t, &coreSlotKeeperMock{})
	_, err := k.TelemetrySnapshot(ctx)
	require.Error(t, err)
}

// A committed height past the open epoch's end cannot exist — the block that
// would have produced it halts — so the snapshot refuses to describe one.
func TestTelemetrySnapshotRefusesAHeightPastTheEpochEnd(t *testing.T) {
	params := types.DefaultParams()
	k, ctx, _ := setupAccountingKeeper(t, &coreSlotKeeperMock{}, 1, params)
	_, err := k.TelemetrySnapshot(ctx.WithBlockHeight(int64(params.EpochLengthBlocks) + 1))
	require.ErrorIs(t, err, types.ErrInvalidState)
}
