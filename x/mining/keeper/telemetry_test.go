package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
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

// The oldest OPEN row is reported, not the newest row: with epoch 1 sealed and
// epoch 2 still open, slot 1 reports epoch 2. Slot id and epoch differ, so a
// gauge fed the wrong one of the two cannot pass.
func TestSlotSettlementStateReportsTheOldestOpenSettlement(t *testing.T) {
	k, ctx, rewards := settlementFixture(t)
	rewards.finalize(2, entitlement(1, 2, fixtureEntitlement))
	require.NoError(t, k.EndBlock(ctx))

	state, err := k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 1}, state,
		"two open rows: the older one is the backlog")

	_, _, err = k.FinalizeSettlement(ctx, finalize(settlementSigner))
	require.NoError(t, err)

	state, err = k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 2}, state)
}

// An older row left open while a newer one seals is still the backlog. This is
// the case a latest-row view cannot see.
func TestSlotSettlementStateSeesAnOlderRowLeftOpen(t *testing.T) {
	k, ctx, rewards := settlementFixture(t)
	rewards.finalize(2, entitlement(1, 2, fixtureEntitlement))
	require.NoError(t, k.EndBlock(ctx))

	_, _, err := k.FinalizeSettlement(ctx, &types.MsgFinalizeSettlement{
		Signer: account(settlementSigner), SlotId: 1, Epoch: 2,
	})
	require.NoError(t, err)

	state, err := k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 1}, state)
}

// Overdue turns on exactly at the derived deadline, the boundary at which the
// Settlement query starts reporting permissionless_finalization_now.
func TestSlotSettlementStateTurnsOverdueAtTheDeadline(t *testing.T) {
	k, ctx, _ := settlementFixture(t)

	pastDeadline(t, k, ctx, -1)
	state, err := k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 1, Overdue: false}, state,
		"one tick before the deadline")

	pastDeadline(t, k, ctx, 0)
	state, err = k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 1, Overdue: true}, state,
		"at the deadline")
}

// Nothing open reads as epoch 0 and not overdue, for a slot that has sealed
// everything and for one that never had a settlement at all.
func TestSlotSettlementStateWithNothingOpen(t *testing.T) {
	k, ctx, _ := settlementFixture(t)
	pastDeadline(t, k, ctx, 5)
	_, _, err := k.FinalizeSettlement(ctx, finalize(settlementSigner))
	require.NoError(t, err)

	state, err := k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1}, state,
		"a sealed row past its deadline is not overdue")

	state, err = k.SlotSettlementState(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 7}, state)
}

// The index only nominates; the canonical row decides. An index entry that
// disagrees with the rows is an error, never a value, and it stays inside its
// own slot's prefix.
func TestSlotSettlementStateRefusesAnIndexThatDisagreesWithTheRows(t *testing.T) {
	k, ctx, _ := settlementFixture(t)

	// An entry for slot 2 with no canonical row behind it.
	require.NoError(t, k.OpenSettlementsBySlot.Set(ctx, collections.Join(uint64(2), uint64(1)), 1))
	_, err := k.SlotSettlementState(ctx, 2)
	require.ErrorIs(t, err, types.ErrInvalidState)
	require.ErrorContains(t, err, "which has no settlement")

	state, err := k.SlotSettlementState(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, keeper.SlotSettlementState{SlotID: 1, OldestOpenEpoch: 1}, state,
		"slot 2's entry is outside slot 1's prefix")

	// An entry naming a row that is already finalized.
	_, _, err = k.FinalizeSettlement(ctx, finalize(settlementSigner))
	require.NoError(t, err)
	require.NoError(t, k.OpenSettlementsBySlot.Set(ctx, collections.Join(uint64(1), uint64(1)), 1))
	_, err = k.SlotSettlementState(ctx, 1)
	require.ErrorIs(t, err, types.ErrInvalidState)
	require.ErrorContains(t, err, "which is finalized")
}
