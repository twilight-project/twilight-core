package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/twilight-project/twilight-core/x/mining/keeper"
	"github.com/twilight-project/twilight-core/x/mining/types"
)

func TestTelemetrySnapshotReadsTheClocks(t *testing.T) {
	k, ctx := setupKeeper(t, &coreSlotKeeperMock{})
	require.NoError(t, k.SettlementClock.Set(ctx, 41))
	require.NoError(t, k.LastProcessedRewardEpoch.Set(ctx, 6))

	snap, err := k.TelemetrySnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, keeper.TelemetrySnapshot{SettlementClock: 41, LastProcessedRewardEpoch: 6}, snap)
}

// A missing clock is corruption after genesis, and the snapshot says so rather
// than reporting a chain that has never released.
func TestTelemetrySnapshotRefusesAMissingClock(t *testing.T) {
	k, ctx := setupKeeper(t, &coreSlotKeeperMock{})
	require.NoError(t, k.LastProcessedRewardEpoch.Set(ctx, 6))

	_, err := k.TelemetrySnapshot(ctx)
	require.ErrorIs(t, err, types.ErrInvalidState)
}
