package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/internal/checked"
	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// TelemetrySnapshot is the module's economic state as a set of plain values, for
// export as node-local metrics.
//
// It is a READ. Nothing here is consensus: the snapshot is taken by the app after
// a block has committed, from committed state, and its only consumer is a metrics
// sink. It exists so that the numbers an alert watches are the module's own
// numbers, read through the module's own accessors, rather than a second
// derivation kept in the app that could drift from what the module does.
//
// Every field is O(1) to read, or bounded by a supply-sized loop: the halving
// tier walks the halving thresholds of max_supply, which is at most the bit
// length of the supply. The one definitional scan the module has,
// SumOutstandingEntitlementLiability, is deliberately NOT used — the accumulator
// it backstops is.
type TelemetrySnapshot struct {
	CurrentEpoch            uint64
	CurrentEpochStartHeight uint64
	CurrentEpochEndHeight   uint64
	// BlocksRemainingInEpoch counts the blocks still to be produced before the
	// open epoch's canonical last block, measured from the height the snapshot
	// was taken at. It is zero on the closing block itself.
	BlocksRemainingInEpoch uint64
	// OpenRewardEnabledBlocks is the open epoch's reward-enabled block counter.
	// While the chain is not paused it advances by one every block; a counter
	// that stops while Paused is false is a stalled accrual path.
	OpenRewardEnabledBlocks uint64
	// LastFinalizedEpoch is the greatest finalized epoch number, zero when none
	// has finalized yet.
	LastFinalizedEpoch uint64

	CumulativeEmitted sdkmath.Int
	MaxSupply         sdkmath.Int
	HalvingTier       uint64
	// NextHalvingThreshold is the cumulative-emission threshold at which the
	// subsidy next halves, zero once emission has reached max_supply.
	NextHalvingThreshold sdkmath.Int

	// EscrowBalance is the rewards module account's balance in the native denom.
	EscrowBalance sdkmath.Int
	// OutstandingLiability is the O(1) accumulator of unreleased entitlement
	// value.
	OutstandingLiability  sdkmath.Int
	CarryForwardRemainder sdkmath.Int
	// EscrowSolvencyDelta is EscrowBalance - (OutstandingLiability +
	// CarryForwardRemainder), computed exactly. Finalization asserts this is zero
	// at every epoch boundary; between boundaries it is the identity an alert
	// should watch. It is exported as its own value because the three terms are
	// large enough that a float32 metric of each cannot be subtracted exactly.
	EscrowSolvencyDelta sdkmath.Int

	Paused                 bool
	PauseTransitionPending bool
	ReleaseEnabled         bool
}

// TelemetrySnapshot reads the module's economic state at the height carried by
// ctx.
//
// Any read failure is returned rather than papered over. The caller is a metrics
// path and will skip the snapshot; it must not be handed defaults, because a
// gauge that reports "not paused, nothing owed" on the strength of a failed read
// is worse than a missing one.
func (k Keeper) TelemetrySnapshot(ctx context.Context) (TelemetrySnapshot, error) {
	var snap TelemetrySnapshot

	height, err := checked.Uint64FromInt64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrapf("block height is not representable: %v", err)
	}

	state, err := k.GetState(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.CurrentEpoch = state.CurrentEpoch
	snap.CurrentEpochStartHeight = state.CurrentEpochStartHeight

	snap.CurrentEpochEndHeight, err = k.EpochEndHeight(ctx, state.CurrentEpoch)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	if height > snap.CurrentEpochEndHeight {
		// A committed height past the open epoch's end is the divergence
		// ShouldFinalizeAtHeight halts on; it cannot commit, so it cannot be
		// observed here. Refusing keeps the snapshot honest if it ever were.
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrapf(
			"epoch %d ends at height %d but the committed height is %d",
			state.CurrentEpoch, snap.CurrentEpochEndHeight, height)
	}
	snap.BlocksRemainingInEpoch = snap.CurrentEpochEndHeight - height

	snap.OpenRewardEnabledBlocks, err = k.GetOpenRewardEnabledBlocks(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}

	snap.LastFinalizedEpoch, err = k.latestFinalizedEpochNumber(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}

	snap.CumulativeEmitted, err = types.ParseAmountString("cumulative emitted", state.CumulativeEmitted)
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrap(err.Error())
	}
	snap.CarryForwardRemainder, err = types.ParseAmountString("carry forward remainder", state.CarryForwardRemainder)
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrap(err.Error())
	}

	params, err := k.GetParams(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.MaxSupply, err = types.ParseAmountString("max supply", params.MaxSupply)
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrap(err.Error())
	}
	snap.HalvingTier, err = HalvingTier(snap.CumulativeEmitted, snap.MaxSupply)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	threshold, found, err := NextHalvingThreshold(snap.CumulativeEmitted, snap.MaxSupply)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.NextHalvingThreshold = sdkmath.ZeroInt()
	if found {
		snap.NextHalvingThreshold = threshold
	}

	address := k.accountKeeper.GetModuleAddress(types.ModuleName)
	if address == nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrap("rewards module account is missing")
	}
	snap.EscrowBalance = k.bankKeeper.GetBalance(ctx, address, params.NativeDenom).Amount
	if snap.EscrowBalance.IsNil() {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrap("rewards escrow balance is nil")
	}
	snap.OutstandingLiability, err = k.GetOutstandingEntitlementLiability(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	owed, err := snap.OutstandingLiability.SafeAdd(snap.CarryForwardRemainder)
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrapf("outstanding liability plus carry overflows: %v", err)
	}
	snap.EscrowSolvencyDelta, err = snap.EscrowBalance.SafeSub(owed)
	if err != nil {
		return TelemetrySnapshot{}, types.ErrInvalidState.Wrapf("escrow solvency delta overflows: %v", err)
	}

	pause, err := k.GetPauseState(ctx)
	if err != nil {
		return TelemetrySnapshot{}, err
	}
	snap.Paused = pause.CurrentPaused
	snap.PauseTransitionPending = pause.HasPending
	snap.ReleaseEnabled = !pause.CurrentPaused

	return snap, nil
}

// latestFinalizedEpochNumber returns the greatest finalized epoch number, or zero
// when the history is empty.
//
// One descending seek, not a scan: collections.Uint64Key is big-endian, so the
// first key of a descending iteration is the greatest. Only the key is read; the
// record itself is not decoded, because nothing here needs its contents.
func (k Keeper) latestFinalizedEpochNumber(ctx context.Context) (uint64, error) {
	iter, err := k.FinalizedEpochs.Iterate(ctx, new(collections.Range[uint64]).Descending())
	if err != nil {
		return 0, types.ErrInvalidState.Wrapf("finalized epoch history could not be read: %v", err)
	}
	defer iter.Close()
	if !iter.Valid() {
		return 0, nil
	}
	epoch, err := iter.Key()
	if err != nil {
		return 0, types.ErrInvalidState.Wrapf("latest finalized epoch key could not be read: %v", err)
	}
	if epoch == 0 {
		return 0, types.ErrInvalidState.Wrap("finalized epoch history holds epoch zero")
	}
	return epoch, nil
}
