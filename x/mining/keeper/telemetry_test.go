package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// setSettlement writes one row at (slot, epoch) with a given outcome, the only
// state LastSlotSettlement reads.
func setSettlement(t *testing.T, k keeper.Keeper, ctx sdk.Context, slot, epoch uint64, finalized bool, reason types.SettlementFinalizationReason) {
	t.Helper()
	require.NoError(t, k.Settlements.Set(ctx, collections.Join(slot, epoch), types.Settlement{
		SlotId:             slot,
		Epoch:              epoch,
		Finalized:          finalized,
		FinalizationReason: reason,
	}))
}

// The latest row is the greatest epoch, read as a single descending row — not a
// scan back for the last finalized one. An open latest row reports open.
func TestLastSlotSettlementReturnsGreatestEpoch(t *testing.T) {
	k, ctx := setupKeeper(t, &coreSlotKeeperMock{})
	// Slot 1 has settled three epochs; the newest (3) is still open.
	setSettlement(t, k, ctx, 1, 1, true, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_AUTHORIZED_EARLY)
	setSettlement(t, k, ctx, 1, 2, true, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_PERMISSIONLESS_AFTER_DEADLINE)
	setSettlement(t, k, ctx, 1, 3, false, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_UNSPECIFIED)

	got, err := k.LastSlotSettlement(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlement{
		SlotID:             1,
		Epoch:              3,
		Finalized:          false,
		FinalizationReason: types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_UNSPECIFIED.String(),
		Found:              true,
	}, got)
}

// A slot with no row yet reports Found=false rather than a fabricated epoch 0,
// which the app relies on to emit no series for it.
func TestLastSlotSettlementReportsNoRow(t *testing.T) {
	k, ctx := setupKeeper(t, &coreSlotKeeperMock{})

	got, err := k.LastSlotSettlement(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlement{SlotID: 7}, got)
	require.False(t, got.Found)
}

// The descending range is prefixed by slot, so one slot's rows never leak into
// another's latest — slot 2's newest epoch is its own, not slot 1's higher one.
func TestLastSlotSettlementIsScopedPerSlot(t *testing.T) {
	k, ctx := setupKeeper(t, &coreSlotKeeperMock{})
	setSettlement(t, k, ctx, 1, 9, true, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_AUTHORIZED_EARLY)
	setSettlement(t, k, ctx, 2, 4, true, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_PERMISSIONLESS_OPERATOR_ONLY)

	got, err := k.LastSlotSettlement(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.SlotID)
	require.Equal(t, uint64(4), got.Epoch)
	require.Equal(t, types.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_PERMISSIONLESS_OPERATOR_ONLY.String(), got.FinalizationReason)
}
