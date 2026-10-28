package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/collections"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"

	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
	"github.com/twilight-project/twilight-core/x/rewards/keeper"
	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// Promotion writes the canonical history row and the derived version -> epoch
// index inside the one cache EndBlock runs in, so the two cannot commit apart.
// The existing regression for that — "a failed index write rolls back the
// history row" under TestVersionIndexIsWrittenWithTheHistoryItDescribes — plants
// a conflicting index entry, so promotion fails at the write-once PRECHECK and
// the index Set is never attempted. This one fails the Set itself, after the
// history row has been written into the same cache, and proves that nothing of
// the rewards store's transition survives: not the row, not the index, not the
// finalized epoch, not an entitlement, not the liability (#84). What it adds over
// the precheck case is the Set's own error path — a Set whose error were dropped
// would pass the precheck test and fail this one.

var errInjectedIndexWrite = errors.New("injected: reward configuration version index write refused")

// indexWriteFault, while armed, refuses every Set under the derived-index prefix
// and records two things at the moment of refusal: how many history rows had been
// written into the same store so far, and whether the row the test expects to be
// promoted (historyKey) was present in that store. Together they prove the
// refusal landed AFTER the history write, in the same cache.
type indexWriteFault struct {
	armed                    bool
	historyKey               []byte
	historyWrites            int
	historyWritesBeforeFault int
	historyRowPresentAtFault bool
	faults                   int
}

type faultingKVStoreService struct {
	inner corestore.KVStoreService
	fault *indexWriteFault
}

func (s faultingKVStoreService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return faultingKVStore{inner: s.inner.OpenKVStore(ctx), fault: s.fault}
}

type faultingKVStore struct {
	inner corestore.KVStore
	fault *indexWriteFault
}

func (s faultingKVStore) Get(key []byte) ([]byte, error) { return s.inner.Get(key) }
func (s faultingKVStore) Has(key []byte) (bool, error)   { return s.inner.Has(key) }
func (s faultingKVStore) Delete(key []byte) error        { return s.inner.Delete(key) }
func (s faultingKVStore) Iterator(start, end []byte) (corestore.Iterator, error) {
	return s.inner.Iterator(start, end)
}
func (s faultingKVStore) ReverseIterator(start, end []byte) (corestore.Iterator, error) {
	return s.inner.ReverseIterator(start, end)
}

func (s faultingKVStore) Set(key, value []byte) error {
	switch {
	case bytes.HasPrefix(key, types.RewardConfigVersionsPrefix.Bytes()):
		s.fault.historyWrites++
	case s.fault.armed && bytes.HasPrefix(key, types.RewardConfigVersionIndexPrefix.Bytes()):
		s.fault.faults++
		s.fault.historyWritesBeforeFault = s.fault.historyWrites
		if len(s.fault.historyKey) != 0 {
			present, err := s.inner.Has(s.fault.historyKey)
			s.fault.historyRowPresentAtFault = err == nil && present
		}
		return errInjectedIndexWrite
	}
	return s.inner.Set(key, value)
}

// setupPromotionWithIndexFault mirrors the finalization fixtures: an open epoch
// with two participants whose boundary is one EndBlock away, a multi-version
// reward-configuration history, and the one admissible scheduled reward
// configuration, keyed at the closing epoch + 1 so that promotion has work to do.
// Every store write goes through a fault that is disarmed until the test arms it.
func setupPromotionWithIndexFault(t *testing.T) (keeper.Keeper, sdk.Context, *indexWriteFault, types.Params) {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	keys := storetypes.NewKVStoreKeys(types.StoreKey)
	cms := integration.CreateMultiStore(keys, log.NewNopLogger())
	ctx := sdk.NewContext(cms, cmtproto.Header{Height: 1}, false, log.NewNopLogger())

	fault := &indexWriteFault{}
	service := faultingKVStoreService{inner: runtime.NewKVStoreService(keys[types.StoreKey]), fault: fault}

	params := rewardConfigParams()
	const openEpoch, participants = uint64(7), 2
	slots := make(map[uint64]coreslottypes.CoreSlot, participants)
	for i := 1; i <= participants; i++ {
		id := uint64(i)
		slots[id] = coreslottypes.CoreSlot{
			SlotId:          id,
			OperatorAddress: addr(byte(60 + id)),
			PayoutAddress:   addr(byte(100 + id)),
			Status:          coreslottypes.SlotStatus_SLOT_STATUS_ACTIVE,
		}
	}
	core := &coreSlotKeeperMock{slots: slots}

	k := keeper.NewKeeper(cdc, service, accountKeeperMock{}, &bankKeeperMock{}, core, testEconomicAddresses(t))
	require.NoError(t, k.SetPauseState(ctx, types.RewardsPauseState{}))
	require.NoError(t, k.SetOpenRewardEnabledBlocks(ctx, params.EpochLengthBlocks))
	require.NoError(t, k.SetOutstandingEntitlementLiability(ctx, sdkmath.ZeroInt()))
	require.NoError(t, k.SetParams(ctx, params))

	state := types.RewardsState{
		CurrentEpoch: openEpoch, CurrentEpochStartHeight: 1,
		CumulativeEmitted: "0", CarryForwardRemainder: "0",
	}
	require.NoError(t, k.SetState(ctx, state))
	cfg, err := keeper.BuildEpochConfigSnapshot(params)
	require.NoError(t, err)
	require.NoError(t, k.SetCurrentEpochConfig(ctx, cfg))
	seedEpochTimeline(t, k, ctx, params, state)

	seedRewardConfigTimeline(t, k, ctx, params)
	for i := uint64(2); i <= 6; i++ {
		seedRewardVersion(t, k, ctx, rewardVersionAt(i, i, sdkmath.NewInt(int64(10+i)).String()))
	}
	for i := 1; i <= participants; i++ {
		require.NoError(t, k.SetActiveBlocks(ctx, openEpoch, uint64(i), 1))
	}

	// The single admissible scheduled entry: effective at closing + 1, promoted as
	// version 7 when epoch 7 closes.
	require.NoError(t, k.ScheduledRewardConfigs.Set(ctx, openEpoch+1, types.ScheduledRewardConfig{
		EffectiveEpoch: openEpoch + 1, InitialBlockSubsidy: "20",
	}))
	return k, ctx, fault, params
}

func TestPromotionRollsBackWhenTheIndexWriteItselfFails(t *testing.T) {
	const closingEpoch, promotedEpoch, promotedVersion = uint64(7), uint64(8), uint64(7)

	t.Run("control: the same block commits everything when the index write succeeds", func(t *testing.T) {
		k, ctx, _, params := setupPromotionWithIndexFault(t)
		require.Equal(t, finalizationEndHeight, int64(params.EpochLengthBlocks), "the fixture's boundary is the shared one")
		liabilityBefore, err := k.GetOutstandingEntitlementLiability(ctx)
		require.NoError(t, err)

		require.NoError(t, k.EndBlock(ctx.WithBlockHeight(finalizationEndHeight)))

		// The values the failure case asserts "unchanged" must be ones a successful
		// block actually changes, or those assertions would prove nothing.
		state, err := k.GetState(ctx)
		require.NoError(t, err)
		require.NotEqual(t, "0", state.CumulativeEmitted, "a finalized epoch emits")
		liabilityAfter, err := k.GetOutstandingEntitlementLiability(ctx)
		require.NoError(t, err)
		require.True(t, liabilityAfter.GT(liabilityBefore), "a finalized epoch creates entitlement liability")

		row, err := k.RewardConfigVersions.Get(ctx, promotedEpoch)
		require.NoError(t, err)
		require.Equal(t, promotedVersion, row.Version)
		indexed, err := k.RewardConfigVersionIndex.Get(ctx, promotedVersion)
		require.NoError(t, err)
		require.Equal(t, promotedEpoch, indexed)
		var found bool
		_, found, err = k.ScheduledRewardConfigFor(ctx, promotedEpoch)
		require.NoError(t, err)
		require.False(t, found, "the promoted schedule entry is consumed")
		_, found, err = k.GetFinalizedEpoch(ctx, closingEpoch)
		require.NoError(t, err)
		require.True(t, found, "the boundary finalized in this block")
		has, err := k.SlotEntitlements.Has(ctx, collections.Join(closingEpoch, uint64(1)))
		require.NoError(t, err)
		require.True(t, has, "the closing epoch's entitlements were materialized")
	})

	t.Run("a refused index write discards the history row and the whole transition", func(t *testing.T) {
		k, ctx, fault, _ := setupPromotionWithIndexFault(t)
		liabilityBefore, err := k.GetOutstandingEntitlementLiability(ctx)
		require.NoError(t, err)

		// Armed only now, after the fixture's own index writes, and told which
		// history row to look for at the moment of refusal.
		fault.historyWrites = 0
		fault.historyKey = append(types.RewardConfigVersionsPrefix.Bytes(), sdk.Uint64ToBigEndian(promotedEpoch)...)
		fault.armed = true
		err = k.EndBlock(ctx.WithBlockHeight(finalizationEndHeight))
		require.ErrorIs(t, err, errInjectedIndexWrite)

		// The failure was injected at the real Set, after the history row had been
		// written into the same cache — the boundary the write-once precheck never
		// reaches. Promotion is the only writer under the history prefix in this
		// block, so the count is exact; the presence check is the direct proof.
		require.Equal(t, 1, fault.faults)
		require.Equal(t, 1, fault.historyWritesBeforeFault,
			"the history row must have been written before the index write was refused")
		require.True(t, fault.historyRowPresentAtFault,
			"the promoted row must be in the cache when the index write is refused")

		// Promoted history row: absent.
		has, err := k.RewardConfigVersions.Has(ctx, promotedEpoch)
		require.NoError(t, err)
		require.False(t, has, "the history row written before the failure must not survive it")
		// Derived index row: absent.
		has, err = k.RewardConfigVersionIndex.Has(ctx, promotedVersion)
		require.NoError(t, err)
		require.False(t, has)
		// Scheduled configuration: still queued.
		scheduled, found, err := k.ScheduledRewardConfigFor(ctx, promotedEpoch)
		require.NoError(t, err)
		require.True(t, found, "a promotion that did not commit must leave the schedule intact")
		require.Equal(t, "20", scheduled.InitialBlockSubsidy)
		// Finalization, entitlement and liability state: none of it committed.
		_, found, err = k.GetFinalizedEpoch(ctx, closingEpoch)
		require.NoError(t, err)
		require.False(t, found, "the epoch must not read as finalized")
		state, err := k.GetState(ctx)
		require.NoError(t, err)
		require.Equal(t, "0", state.CumulativeEmitted, "nothing was emitted")
		has, err = k.SlotEntitlements.Has(ctx, collections.Join(closingEpoch, uint64(1)))
		require.NoError(t, err)
		require.False(t, has, "no entitlement may exist for an epoch whose block failed")
		liabilityAfter, err := k.GetOutstandingEntitlementLiability(ctx)
		require.NoError(t, err)
		require.True(t, liabilityBefore.Equal(liabilityAfter), "the entitlement liability must not move")
	})
}
