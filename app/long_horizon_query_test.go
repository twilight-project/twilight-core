package app_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	storetypes "cosmossdk.io/store/types"

	"github.com/twilight-project/twilight-core/app"
	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
	miningtypes "github.com/twilight-project/twilight-core/x/mining/types"
	rewardstypes "github.com/twilight-project/twilight-core/x/rewards/types"
)

// The long-horizon contract: nothing a consumer asks may get harder, or stop
// being answerable, because the chain is old.
//
// # Why this exists
//
// #182 was a query that broke by chain age alone. EpochBoundaries projected
// forward one epoch per step from the governing epoch-config version under a
// 1000-step cap, and on a chain whose only version is anchored at genesis that
// cap was a cap on the chain's age: every boundary past epoch 1000 was refused,
// past epochs included, and both reward pipelines on the live testnet stalled for
// twenty-one hours about three weeks after genesis. No test had ever advanced a
// chain that far. The horizon test that did exist asked about a far-future epoch,
// which is the case the cap was written for, not the case that broke.
//
// So this harness ages the chain by thousands of epochs and then asks every
// served query of x/rewards, x/mining and x/coreslot the questions a consumer asks
// at the head: about the current epoch, about recent history, about epoch 1 and
// about the epoch after this one. Every method the app registers must be here —
// the set is enumerated from the real gRPC registration, so a query cannot be
// added without joining the harness — and none may answer Internal or Unknown.
//
// # How the chain is aged, and why not by producing blocks
//
// Real blocks are the honest way to age a chain and they are too slow for this:
// one FinalizeBlock+Commit round costs about 0.2 ms on this single-slot in-memory
// chain, so 3000 epochs at the ratified 360-block minimum is roughly 1.08 million
// rounds — some four minutes here and more on a CI runner — and 10 000 epochs is
// three times that, on every push. A gate that costs that much is a gate that
// gets moved to nightly, which is where it stops guarding merges.
//
// A fixture cannot be imported either. Fresh-genesis validation in every module
// deliberately refuses a document that describes an aged chain — an open epoch
// other than 1, a settlement cursor other than 0 — and continuation import does
// not exist yet. That refusal is correct and is not worked around here.
//
// What is done instead is a hybrid, in three steps:
//
//  1. The chain is booted with its FIRST block at the height epoch N would start
//     on a chain born at height 1, through the real InitChain, and produces that
//     block for real.
//  2. The committed state is re-anchored to the shape an exported N-epoch-old
//     chain has: the single epoch-config version effective at epoch 1 from height
//     1, the open epoch N, the participation of that first block credited to N,
//     the mining cursor one epoch behind N, and a settlement clock that has ticked
//     once per block since height 1. Nothing else is fabricated: no finalized
//     epochs, no entitlements, no settlements. Those are produced by the next
//     step, by consensus.
//  3. Several whole epochs are then produced through the real BeginBlock,
//     EndBlock, epoch finalization, entitlement creation, settlement
//     materialization and configuration promotion, from a state the block path
//     itself verifies on every block: the open epoch's anchor is checked against
//     the canonical history before any boundary decision, so a re-anchoring the
//     chain would not accept is a halted block, not a passed test.
//
// The result is a head thousands of epochs from its only version anchor, which is
// precisely the geometry #182 needed and no other fixture had. Its recent history
// is real; its distant past is empty, and the harness treats "no record of epoch
// 1" as the honest answer it is.
//
// # What a passing run proves
//
// Every served query answers at the head. The store work of asking about the head
// epoch is the same as asking about an epoch next to the anchor, and the same at
// 3000 epochs as at 10 000, so a walk proportional to chain age is caught by
// arithmetic rather than by a timer — the timer is there too, generously, as a
// second line. Both are asserted on the real gRPC path with the real height
// header, exactly as a remote consumer reaches the handler.

// agedEpochsDriven is how many whole epochs are produced for real after the
// re-anchoring, so the head has real finalized epochs, entitlements and
// settlements behind it.
const agedEpochsDriven = 3

// horizons are the chain ages the harness runs at, as the epoch open at the
// re-anchoring. The live testnet's 360-block, five-second epochs make the first
// about two months of chain life and the second about seven; #182 struck at
// epoch 1000, three weeks in.
var horizons = []uint64{3_001, 10_001}

// agedChain is a pinnedChain whose head is thousands of epochs from genesis.
type agedChain struct {
	*pinnedChain
	// headEpoch is the epoch open at the head.
	headEpoch uint64
	// headEpochStart is the canonical first height of headEpoch.
	headEpochStart uint64
}

// bootAgedChain produces a chain whose open epoch at the head is
// openEpoch+agedEpochsDriven, aged as described in the file header.
func bootAgedChain(t *testing.T, openEpoch uint64) *agedChain {
	t.Helper()
	require.Greater(t, openEpoch, uint64(1), "an aged chain is one that has left epoch 1")

	length := uint64(epochLength)
	// The first height of openEpoch on a chain born at height 1 with this epoch
	// length and no geometry change since: the recurrence EpochStartHeight uses.
	openStart := 1 + (openEpoch-1)*length
	chain := bootPinnedChainAt(t, int64(openStart))
	chain.commitThrough(t, int64(openStart))

	// Re-anchor the committed state. Writes through a head context are committed
	// by the next block, and that block's BeginBlock verifies the open epoch
	// against the canonical history before it does anything else.
	ctx := chain.headContext()
	rewards := chain.app.RewardsKeeper

	state, err := rewards.GetState(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.CurrentEpoch)
	require.Equal(t, openStart, state.CurrentEpochStartHeight)
	state.CurrentEpoch = openEpoch
	require.NoError(t, rewards.SetState(ctx, state))

	anchor, err := rewards.EpochConfigVersions.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, openStart, anchor.EffectiveStartHeight)
	anchor.EffectiveStartHeight = 1
	require.NoError(t, rewards.EpochConfigVersions.Set(ctx, 1, anchor))

	// The one real block so far credited its participation under epoch 1. It was
	// a block of openEpoch, so its credit moves with it; the reward-enabled
	// counter is per open epoch and already counts it.
	credited, err := rewards.GetActiveBlocks(ctx, 1, 1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), credited)
	require.NoError(t, rewards.ActiveBlocks.Remove(ctx, collections.Join(uint64(1), uint64(1))))
	require.NoError(t, rewards.SetActiveBlocks(ctx, openEpoch, 1, credited))

	mining := chain.app.MiningKeeper
	require.NoError(t, mining.LastProcessedRewardEpoch.Set(ctx, openEpoch-1))
	// Every block since height 1 was release-enabled, so the clock is the height.
	require.NoError(t, mining.SettlementClock.Set(ctx, openStart))

	chain.commitThrough(t, chain.head+agedEpochsDriven*epochLength+5)

	headEpoch := openEpoch + agedEpochsDriven
	return &agedChain{
		pinnedChain:    chain,
		headEpoch:      headEpoch,
		headEpochStart: 1 + (headEpoch-1)*length,
	}
}

// horizonCase is one question a consumer asks at the head.
type horizonCase struct {
	name   string
	method string
	req    protoMessage
	// absent lists the codes that are an honest answer for this question besides
	// success. It is only ever NotFound: an aged chain has no record of its
	// distant past and no record of its future, and saying so is the contract.
	// Internal and Unknown are refused for every case regardless.
	absent []codes.Code
	// verify inspects a successful reply.
	verify func(t *testing.T, reply any)
}

var notFound = []codes.Code{codes.NotFound}

func (c *agedChain) horizonCases() []horizonCase {
	head := c.headEpoch
	previous := head - 1
	length := uint64(epochLength)

	return []horizonCase{
		// --- x/rewards -----------------------------------------------------
		{name: "rewards params", method: "/twilight.rewards.v1.Query/Params",
			req: &rewardstypes.QueryParamsRequest{}},
		{name: "rewards epoch info", method: "/twilight.rewards.v1.Query/EpochInfo",
			req: &rewardstypes.QueryEpochInfoRequest{},
			verify: func(t *testing.T, reply any) {
				info := reply.(*rewardstypes.QueryEpochInfoResponse)
				require.Equal(t, head, info.State.CurrentEpoch)
				require.Equal(t, c.headEpochStart, info.CurrentEpochStartHeight)
				require.Equal(t, c.headEpochStart+length-1, info.CurrentEpochEndHeight)
				require.Equal(t, length, info.CurrentEpochLengthBlocks)
			}},
		{name: "rewards next halving", method: "/twilight.rewards.v1.Query/NextHalving",
			req: &rewardstypes.QueryNextHalvingRequest{}},
		{name: "rewards epoch reward, previous epoch", method: "/twilight.rewards.v1.Query/EpochReward",
			req: &rewardstypes.QueryEpochRewardRequest{EpochNumber: previous},
			verify: func(t *testing.T, reply any) {
				epoch := reply.(*rewardstypes.QueryEpochRewardResponse).EpochReward
				require.Equal(t, previous, epoch.EpochNumber)
				require.Equal(t, c.headEpochStart-length, epoch.StartHeight)
				require.Equal(t, c.headEpochStart-1, epoch.EndHeight)
				require.Equal(t, length, epoch.RewardEnabledBlocks)
			}},
		{name: "rewards epoch reward, epoch 1", method: "/twilight.rewards.v1.Query/EpochReward",
			req: &rewardstypes.QueryEpochRewardRequest{EpochNumber: 1}, absent: notFound},
		{name: "rewards epoch reward, next epoch", method: "/twilight.rewards.v1.Query/EpochReward",
			req: &rewardstypes.QueryEpochRewardRequest{EpochNumber: head + 1}, absent: notFound},
		{name: "rewards cumulative emitted", method: "/twilight.rewards.v1.Query/CumulativeEmitted",
			req: &rewardstypes.QueryCumulativeEmittedRequest{}},
		{name: "rewards supply schedule", method: "/twilight.rewards.v1.Query/SupplySchedule",
			req: &rewardstypes.QuerySupplyScheduleRequest{}},
		{name: "rewards current epoch active blocks", method: "/twilight.rewards.v1.Query/CurrentEpochActiveBlocks",
			req: &rewardstypes.QueryCurrentEpochActiveBlocksRequest{},
			verify: func(t *testing.T, reply any) {
				active := reply.(*rewardstypes.QueryCurrentEpochActiveBlocksResponse)
				require.Equal(t, head, active.EpochNumber)
				require.Len(t, active.ActiveBlocks, 1)
				require.Equal(t, uint64(1), active.ActiveBlocks[0].SlotId)
			}},
		{name: "rewards module balances", method: "/twilight.rewards.v1.Query/ModuleBalances",
			req: &rewardstypes.QueryModuleBalancesRequest{}},
		{name: "rewards epoch config versions", method: "/twilight.rewards.v1.Query/EpochConfigVersions",
			req: &rewardstypes.QueryEpochConfigVersionsRequest{},
			verify: func(t *testing.T, reply any) {
				versions := reply.(*rewardstypes.QueryEpochConfigVersionsResponse)
				require.Len(t, versions.Versions, 1, "the only version is the genesis anchor")
				require.Equal(t, uint64(1), versions.Versions[0].EffectiveEpoch)
				require.Empty(t, versions.Scheduled)
			}},
		{name: "rewards epoch boundaries, current epoch", method: "/twilight.rewards.v1.Query/EpochBoundaries",
			req:    &rewardstypes.QueryEpochBoundariesRequest{EpochNumber: head},
			verify: c.verifyBoundaries(head)},
		{name: "rewards epoch boundaries, epoch 1", method: "/twilight.rewards.v1.Query/EpochBoundaries",
			req:    &rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 1},
			verify: c.verifyBoundaries(1)},
		{name: "rewards epoch boundaries, next epoch", method: "/twilight.rewards.v1.Query/EpochBoundaries",
			req:    &rewardstypes.QueryEpochBoundariesRequest{EpochNumber: head + 1},
			verify: c.verifyBoundaries(head + 1)},
		{name: "rewards epoch boundaries, a year ahead", method: "/twilight.rewards.v1.Query/EpochBoundaries",
			req:    &rewardstypes.QueryEpochBoundariesRequest{EpochNumber: head + 17_520},
			verify: c.verifyBoundaries(head + 17_520)},
		{name: "rewards slot entitlement, previous epoch", method: "/twilight.rewards.v1.Query/SlotEntitlement",
			req: &rewardstypes.QuerySlotEntitlementRequest{SlotId: 1, Epoch: previous},
			verify: func(t *testing.T, reply any) {
				entitlement := reply.(*rewardstypes.QuerySlotEntitlementResponse).Entitlement
				require.Equal(t, previous, entitlement.Epoch)
				require.Equal(t, uint64(1), entitlement.SlotId)
				require.Equal(t, c.payout, entitlement.PayoutAddress)
			}},
		{name: "rewards slot entitlement, epoch 1", method: "/twilight.rewards.v1.Query/SlotEntitlement",
			req: &rewardstypes.QuerySlotEntitlementRequest{SlotId: 1, Epoch: 1}, absent: notFound},
		{name: "rewards slot entitlement, next epoch", method: "/twilight.rewards.v1.Query/SlotEntitlement",
			req: &rewardstypes.QuerySlotEntitlementRequest{SlotId: 1, Epoch: head + 1}, absent: notFound},
		{name: "rewards entitlements by epoch, previous epoch", method: "/twilight.rewards.v1.Query/SlotEntitlementsByEpoch",
			req: &rewardstypes.QuerySlotEntitlementsByEpochRequest{Epoch: previous},
			verify: func(t *testing.T, reply any) {
				require.Len(t, reply.(*rewardstypes.QuerySlotEntitlementsByEpochResponse).Entitlements, 1)
			}},
		{name: "rewards entitlements by epoch, epoch 1", method: "/twilight.rewards.v1.Query/SlotEntitlementsByEpoch",
			req: &rewardstypes.QuerySlotEntitlementsByEpochRequest{Epoch: 1},
			verify: func(t *testing.T, reply any) {
				require.Empty(t, reply.(*rewardstypes.QuerySlotEntitlementsByEpochResponse).Entitlements)
			}},
		{name: "rewards entitlements by epoch, next epoch", method: "/twilight.rewards.v1.Query/SlotEntitlementsByEpoch",
			req: &rewardstypes.QuerySlotEntitlementsByEpochRequest{Epoch: head + 1},
			verify: func(t *testing.T, reply any) {
				require.Empty(t, reply.(*rewardstypes.QuerySlotEntitlementsByEpochResponse).Entitlements)
			}},
		{name: "rewards reward config versions", method: "/twilight.rewards.v1.Query/RewardConfigVersions",
			req: &rewardstypes.QueryRewardConfigVersionsRequest{},
			verify: func(t *testing.T, reply any) {
				versions := reply.(*rewardstypes.QueryRewardConfigVersionsResponse)
				require.Len(t, versions.Versions, 1)
				require.Nil(t, versions.Scheduled)
			}},
		{name: "rewards reward config version 1", method: "/twilight.rewards.v1.Query/RewardConfigVersion",
			req: &rewardstypes.QueryRewardConfigVersionRequest{Version: 1}},
		{name: "rewards reward config version at epoch 1", method: "/twilight.rewards.v1.Query/RewardConfigVersion",
			req: &rewardstypes.QueryRewardConfigVersionRequest{EffectiveEpoch: 1}},
		{name: "rewards reward config version at the current epoch", method: "/twilight.rewards.v1.Query/RewardConfigVersion",
			req: &rewardstypes.QueryRewardConfigVersionRequest{EffectiveEpoch: head}, absent: notFound},
		{name: "rewards pause state", method: "/twilight.rewards.v1.Query/RewardsPauseState",
			req: &rewardstypes.QueryRewardsPauseStateRequest{},
			verify: func(t *testing.T, reply any) {
				require.True(t, reply.(*rewardstypes.QueryRewardsPauseStateResponse).ReleaseEnabled)
			}},

		// --- x/mining ------------------------------------------------------
		{name: "mining settlement, previous epoch", method: "/twilight.mining.v1.Query/Settlement",
			req: &miningtypes.QuerySettlementRequest{SlotId: 1, Epoch: previous},
			verify: func(t *testing.T, reply any) {
				settlement := reply.(*miningtypes.QuerySettlementResponse)
				require.Equal(t, previous, settlement.Settlement.Epoch)
				require.False(t, settlement.Settlement.Finalized)
				require.Equal(t, uint64(c.head), settlement.CurrentSettlementClock,
					"every block of this chain's life was release-enabled")
			}},
		{name: "mining settlement, epoch 1", method: "/twilight.mining.v1.Query/Settlement",
			req: &miningtypes.QuerySettlementRequest{SlotId: 1, Epoch: 1}, absent: notFound},
		{name: "mining settlement, next epoch", method: "/twilight.mining.v1.Query/Settlement",
			req: &miningtypes.QuerySettlementRequest{SlotId: 1, Epoch: head + 1}, absent: notFound},
		{name: "mining open settlements", method: "/twilight.mining.v1.Query/OpenSettlements",
			req: &miningtypes.QueryOpenSettlementsRequest{SlotId: 1},
			verify: func(t *testing.T, reply any) {
				open := reply.(*miningtypes.QueryOpenSettlementsResponse)
				require.Len(t, open.Settlements, agedEpochsDriven, "one open settlement per epoch closed for real")
			}},
		{name: "mining settlement clock", method: "/twilight.mining.v1.Query/SettlementClock",
			req: &miningtypes.QuerySettlementClockRequest{},
			verify: func(t *testing.T, reply any) {
				require.Equal(t, uint64(c.head), reply.(*miningtypes.QuerySettlementClockResponse).SettlementClock)
			}},
		{name: "mining distribution mode version 1", method: "/twilight.mining.v1.Query/DistributionModeVersion",
			req: &miningtypes.QueryDistributionModeVersionRequest{Version: 1}},
		{name: "mining distribution mode version 2", method: "/twilight.mining.v1.Query/DistributionModeVersion",
			req: &miningtypes.QueryDistributionModeVersionRequest{Version: 2}, absent: notFound},
		{name: "mining distribution mode versions", method: "/twilight.mining.v1.Query/DistributionModeVersions",
			req: &miningtypes.QueryDistributionModeVersionsRequest{}},
		{name: "mining selection params version 1", method: "/twilight.mining.v1.Query/SelectionParamsVersion",
			req: &miningtypes.QuerySelectionParamsVersionRequest{Version: 1}},
		{name: "mining selection params versions", method: "/twilight.mining.v1.Query/SelectionParamsVersions",
			req: &miningtypes.QuerySelectionParamsVersionsRequest{}},
		{name: "mining settlement params version 1", method: "/twilight.mining.v1.Query/SettlementParamsVersion",
			req: &miningtypes.QuerySettlementParamsVersionRequest{Version: 1}},
		{name: "mining settlement params versions", method: "/twilight.mining.v1.Query/SettlementParamsVersions",
			req: &miningtypes.QuerySettlementParamsVersionsRequest{}},
		{name: "mining settlement params for the current epoch", method: "/twilight.mining.v1.Query/SettlementParamsForEpoch",
			req: &miningtypes.QuerySettlementParamsForEpochRequest{Epoch: head},
			verify: func(t *testing.T, reply any) {
				params := reply.(*miningtypes.QuerySettlementParamsForEpochResponse)
				require.Equal(t, head-2, params.BindingEpoch)
				require.False(t, params.Bootstrap)
				require.Equal(t, uint64(1), params.SettlementParamsVersion.Version)
			}},
		{name: "mining settlement params for epoch 1", method: "/twilight.mining.v1.Query/SettlementParamsForEpoch",
			req: &miningtypes.QuerySettlementParamsForEpochRequest{Epoch: 1}},
		{name: "mining settlement params for the next epoch", method: "/twilight.mining.v1.Query/SettlementParamsForEpoch",
			req: &miningtypes.QuerySettlementParamsForEpochRequest{Epoch: head + 1}},
		{name: "mining target epoch interpretation, current epoch", method: "/twilight.mining.v1.Query/TargetEpochInterpretation",
			req: &miningtypes.QueryTargetEpochInterpretationRequest{TargetEpoch: head},
			verify: func(t *testing.T, reply any) {
				interpretation := reply.(*miningtypes.QueryTargetEpochInterpretationResponse)
				require.Equal(t, head-2, interpretation.BindingEpoch)
				require.Equal(t, uint64(1), interpretation.DistributionModeVersion.Version)
			}},
		{name: "mining target epoch interpretation, epoch 1", method: "/twilight.mining.v1.Query/TargetEpochInterpretation",
			req: &miningtypes.QueryTargetEpochInterpretationRequest{TargetEpoch: 1}},
		{name: "mining target epoch interpretation, next epoch", method: "/twilight.mining.v1.Query/TargetEpochInterpretation",
			req: &miningtypes.QueryTargetEpochInterpretationRequest{TargetEpoch: head + 1}},
		{name: "mining economic address", method: "/twilight.mining.v1.Query/ValidateEconomicAddress",
			req: &miningtypes.QueryValidateEconomicAddressRequest{Address: c.payout},
			verify: func(t *testing.T, reply any) {
				require.True(t, reply.(*miningtypes.QueryValidateEconomicAddressResponse).Admissible)
			}},

		// --- x/coreslot ----------------------------------------------------
		{name: "coreslot params", method: "/twilight.coreslot.v1.Query/Params",
			req: &coreslottypes.QueryParamsRequest{}},
		{name: "coreslot slot", method: "/twilight.coreslot.v1.Query/CoreSlot",
			req: &coreslottypes.QueryCoreSlotRequest{SlotId: 1},
			verify: func(t *testing.T, reply any) {
				require.Equal(t, c.credential, reply.(*coreslottypes.QueryCoreSlotResponse).Slot.SettlementAddress)
			}},
		{name: "coreslot slots", method: "/twilight.coreslot.v1.Query/CoreSlots",
			req: &coreslottypes.QueryCoreSlotsRequest{},
			verify: func(t *testing.T, reply any) {
				require.Len(t, reply.(*coreslottypes.QueryCoreSlotsResponse).Slots, 1)
			}},
		{name: "coreslot active slots", method: "/twilight.coreslot.v1.Query/ActiveCoreSlots",
			req: &coreslottypes.QueryActiveCoreSlotsRequest{},
			verify: func(t *testing.T, reply any) {
				require.Len(t, reply.(*coreslottypes.QueryCoreSlotsResponse).Slots, 1)
			}},
		{name: "coreslot slot by operator", method: "/twilight.coreslot.v1.Query/CoreSlotByOperator",
			req: &coreslottypes.QueryCoreSlotByOperatorRequest{OperatorAddress: c.operator}},
		{name: "coreslot slot by consensus address", method: "/twilight.coreslot.v1.Query/CoreSlotByConsensusAddress",
			req: &coreslottypes.QueryCoreSlotByConsensusAddressRequest{ConsensusAddress: c.consensus}},
		{name: "coreslot pending key rotations", method: "/twilight.coreslot.v1.Query/PendingKeyRotations",
			req: &coreslottypes.QueryPendingKeyRotationsRequest{}},
		{name: "coreslot last applied validators", method: "/twilight.coreslot.v1.Query/LastAppliedValidators",
			req: &coreslottypes.QueryLastAppliedValidatorsRequest{},
			verify: func(t *testing.T, reply any) {
				require.Len(t, reply.(*coreslottypes.QueryLastAppliedValidatorsResponse).Validators, 1)
			}},
		// No reservation was ever made on this chain, so absence is the answer.
		{name: "coreslot reserved consensus address", method: "/twilight.coreslot.v1.Query/ReservedConsensusAddress",
			req: &coreslottypes.QueryReservedConsensusAddressRequest{ConsensusAddress: c.consensus}, absent: notFound},
		{name: "coreslot reward weight", method: "/twilight.coreslot.v1.Query/RewardWeight",
			req: &coreslottypes.QueryRewardWeightRequest{SlotId: 1}},
		{name: "coreslot selection policy", method: "/twilight.coreslot.v1.Query/SelectionPolicy",
			req: &coreslottypes.QuerySelectionPolicyRequest{SlotId: 1}},
		{name: "coreslot selection policy version 1", method: "/twilight.coreslot.v1.Query/SelectionPolicyVersion",
			req: &coreslottypes.QuerySelectionPolicyVersionRequest{SlotId: 1, PolicyVersion: 1}},
		{name: "coreslot selection policy at the head", method: "/twilight.coreslot.v1.Query/SelectionPolicyAtHeight",
			req: &coreslottypes.QuerySelectionPolicyAtHeightRequest{SlotId: 1, AtHeight: c.head}},
		// The slot was admitted at the height the fixture booted at, which is an
		// ordinary thing for a slot on an old chain to have been; before that
		// height it had no policy, and that too is an honest answer.
		{name: "coreslot selection policy at height 1", method: "/twilight.coreslot.v1.Query/SelectionPolicyAtHeight",
			req: &coreslottypes.QuerySelectionPolicyAtHeightRequest{SlotId: 1, AtHeight: 1}, absent: notFound},
	}
}

// verifyBoundaries checks a boundary reply against the recurrence a chain with
// one version anchored at genesis follows for every epoch.
func (c *agedChain) verifyBoundaries(epoch uint64) func(t *testing.T, reply any) {
	return func(t *testing.T, reply any) {
		boundaries := reply.(*rewardstypes.QueryEpochBoundariesResponse)
		length := uint64(epochLength)
		require.Equal(t, epoch, boundaries.EpochNumber)
		require.Equal(t, 1+(epoch-1)*length, boundaries.StartHeight)
		require.Equal(t, epoch*length, boundaries.EndHeight)
		require.Equal(t, length, boundaries.EpochLengthBlocks)
	}
}

// call sends one request through the gRPC registration path with the height
// header set, and returns whatever the handler returned.
func (q *headerQuerier) call(t *testing.T, method string, req protoMessage, height int64) (any, error) {
	t.Helper()
	desc, served := q.methods[method]
	require.Truef(t, served, "%s is not served over gRPC", method)
	data, err := req.Marshal()
	require.NoError(t, err)
	return desc.Handler(q.handlers[method], incomingHeight(height),
		func(arg any) error { return arg.(protoMessage).Unmarshal(data) }, nil)
}

// servedQueryMethods lists every query method the app registers for the three
// custom modules, from the registration itself rather than from any list kept
// beside it.
func servedQueryMethods(q *headerQuerier) []string {
	var methods []string
	for method := range q.methods {
		if strings.HasPrefix(method, "/twilight.") && strings.Contains(method, ".Query/") {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// queryCost is what one query cost the node, measured two ways: gas, which the
// query context meters for every store read and is deterministic, and wall
// clock, which is not but is what an operator experiences.
type queryCost struct {
	gas  uint64
	wall time.Duration
}

// measureQuery runs one query through the app's own query router at a pinned
// height and reports its cost. The best of several runs is reported for the
// clock, so a scheduling hiccup cannot fail the bound; gas is required to be
// identical on every run, because a read path that metered differently from one
// run to the next would not be a deterministic read path.
func measureQuery(t *testing.T, a *app.App, method string, req protoMessage, height int64) queryCost {
	t.Helper()
	data, err := req.Marshal()
	require.NoError(t, err)
	route := a.GRPCQueryRouter().Route(method)
	require.NotNilf(t, route, "%s is not routed", method)

	const runs = 5
	best := queryCost{wall: time.Hour}
	for run := 0; run < runs; run++ {
		ctx, err := a.CreateQueryContext(height, false)
		require.NoError(t, err)
		started := time.Now()
		res, err := route(ctx, &abci.RequestQuery{Path: method, Data: data, Height: height})
		elapsed := time.Since(started)
		require.NoError(t, err)
		require.Zerof(t, res.Code, "%s failed: %s", method, res.Log)

		gas := ctx.GasMeter().GasConsumed()
		require.Positive(t, gas, "a query that read nothing answered from nothing")
		if run == 0 {
			best.gas = gas
		}
		require.Equalf(t, best.gas, gas, "%s metered different store work on two identical runs", method)
		if elapsed < best.wall {
			best.wall = elapsed
		}
	}
	return best
}

// queryClockBound is the wall-clock ceiling for one boundary or epoch-info query
// at the head. Deliberately generous — the real queries take microseconds — so
// only a walk over the chain's life could reach it, and so CI noise cannot.
const queryClockBound = 50 * time.Millisecond

// TestEveryQueryAnswersAtALongHorizon is the harness.
//
// It runs once per horizon, and the costs of the two age-sensitive queries are
// carried across the runs so the last assertion can say the thing that matters:
// a chain three times older did exactly the same work.
func TestEveryQueryAnswersAtALongHorizon(t *testing.T) {
	type ageSensitive struct{ boundaries, info queryCost }
	costs := map[uint64]ageSensitive{}

	for _, horizon := range horizons {
		t.Run(fmt.Sprintf("open_epoch_%d", horizon), func(t *testing.T) {
			chain := bootAgedChain(t, horizon)
			assertInvariants(t, chain.app, chain.headContext())
			querier := newHeaderQuerier(chain.app)

			// Every served method is in the table, and every table entry is served.
			// A query added to a module without a case here fails the run.
			cases := chain.horizonCases()
			covered := map[string]bool{}
			for _, testCase := range cases {
				covered[testCase.method] = true
			}
			for _, method := range servedQueryMethods(querier) {
				require.Truef(t, covered[method],
					"%s is served but has no long-horizon case; every query must be asked at the head", method)
			}
			for method := range covered {
				_, served := querier.methods[method]
				require.Truef(t, served, "%s has a case but is not served", method)
			}

			// Asked twice: with no height, as a consumer asking about now does, and
			// pinned to the head, as a reconciling worker does. The two must agree
			// on the code and both must stay inside the contract.
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					for _, height := range []int64{0, chain.head} {
						reply, err := querier.call(t, testCase.method, testCase.req, height)
						if err == nil {
							require.NotNil(t, reply)
							if testCase.verify != nil {
								testCase.verify(t, reply)
							}
							continue
						}
						require.Nil(t, reply, "an error answer must carry no body")
						status, ok := grpcstatus.FromError(err)
						require.Truef(t, ok, "%s returned an error carrying no gRPC status (%v): the untyped Unknown case", testCase.name, err)
						require.NotEqual(t, codes.Internal, status.Code(),
							"%s reported the chain's own state as untrustworthy for a well-formed question: %v", testCase.name, err)
						require.NotEqual(t, codes.Unknown, status.Code(),
							"%s answered with an unclassified error: %v", testCase.name, err)
						require.Containsf(t, testCase.absent, status.Code(),
							"%s must succeed at the head, but answered %s: %v", testCase.name, status.Code(), err)
					}
				})
			}

			// The two queries #182 broke, costed at the head.
			boundariesAtHead := measureQuery(t, chain.app, "/twilight.rewards.v1.Query/EpochBoundaries",
				&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: chain.headEpoch}, chain.head)
			boundariesNearAnchor := measureQuery(t, chain.app, "/twilight.rewards.v1.Query/EpochBoundaries",
				&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 2}, chain.head)
			info := measureQuery(t, chain.app, "/twilight.rewards.v1.Query/EpochInfo",
				&rewardstypes.QueryEpochInfoRequest{}, chain.head)

			requireSameStoreWork(t, boundariesNearAnchor.gas, boundariesAtHead.gas,
				"the store work of a boundary %d epochs from the anchor must be the work of one next to it; "+
					"anything more is a walk measured from genesis", chain.headEpoch-1)
			require.Lessf(t, boundariesAtHead.wall, queryClockBound,
				"EpochBoundaries at epoch %d took %s", chain.headEpoch, boundariesAtHead.wall)
			require.Lessf(t, info.wall, queryClockBound,
				"EpochInfo at epoch %d took %s", chain.headEpoch, info.wall)
			t.Logf("head epoch %d at height %d: EpochBoundaries %d gas in %s, EpochInfo %d gas in %s",
				chain.headEpoch, chain.head, boundariesAtHead.gas, boundariesAtHead.wall, info.gas, info.wall)

			costs[horizon] = ageSensitive{boundaries: boundariesAtHead, info: info}
		})
	}

	require.Len(t, costs, len(horizons), "every horizon must have run to compare them")
	youngest, oldest := costs[horizons[0]], costs[horizons[len(horizons)-1]]
	requireSameStoreWork(t, youngest.boundaries.gas, oldest.boundaries.gas,
		"EpochBoundaries did more store work on the older chain; its cost grows with chain age")
	requireSameStoreWork(t, youngest.info.gas, oldest.info.gas,
		"EpochInfo did more store work on the older chain; its cost grows with chain age")
}

// requireSameStoreWork asserts two queries read the same records.
//
// Gas is metered per record read and per byte read, and the bytes differ a little
// with age on their own: a height in the millions is one varint byte longer than
// one in the thousands, and the record holding it is read either way. So the
// two are allowed to differ by less than the flat cost of ONE read. A walk that
// visited one more record — let alone one per epoch — cannot hide inside that.
func requireSameStoreWork(t *testing.T, want, got uint64, msg string, args ...any) {
	t.Helper()
	oneRead := storetypes.KVGasConfig().ReadCostFlat
	var difference uint64
	if got > want {
		difference = got - want
	} else {
		difference = want - got
	}
	require.Lessf(t, difference, oneRead, msg+" (%d gas against %d)", append(args, got, want)...)
}
