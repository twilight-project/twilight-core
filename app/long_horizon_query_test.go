package app_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/twilight-project/twilight-core/app"
	coreslotkeeper "github.com/twilight-project/twilight-core/x/coreslot/keeper"
	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
	miningtypes "github.com/twilight-project/twilight-core/x/mining/types"
	rewardskeeper "github.com/twilight-project/twilight-core/x/rewards/keeper"
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
// the set is enumerated from the real gRPC registration and checked against the
// generated service descriptors, so a query cannot be added without joining the
// harness — and none may answer Internal or Unknown.
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
// other than 1, a settlement cursor other than 0, a finalized epoch — and
// continuation import does not exist yet. That refusal is correct and is not
// worked around here.
//
// What is done instead is a hybrid, in three steps:
//
//  1. The chain is booted with its FIRST block at the height epoch N would start
//     on a chain born at height 1, through the real InitChain, and produces that
//     block for real.
//  2. The committed state is rewritten to the shape an exported N-epoch-old
//     chain has. The geometry: the single epoch-config version effective at
//     epoch 1 from height 1, the open epoch N, that first block's participation
//     credited to N, the slot and its policy in force since height 1, the mining
//     cursor one epoch behind N, a settlement clock that ticked once per block
//     since height 1. And, at the seeded horizons, the LEDGER: for every epoch
//     1..N-1 the finalized-epoch record consensus would have written, the
//     entitlement it created, the settlement that was materialized for it and
//     the anchor that dated it, with the emission those epochs minted actually
//     minted by the rewards module, so cumulative emission, escrow, liability
//     and supply agree exactly as the six rewards invariants require. The ledger
//     is seeded in two modes, because a chain's history has two shapes. SETTLED
//     is what a healthy operator leaves behind: every entitlement released in
//     full through the bank, every settlement finalized. UNSETTLED is what a
//     stalled pipeline leaves behind — #182 itself accumulated one open
//     settlement per epoch for as long as it lasted: nothing released, every
//     settlement open and indexed, the escrow holding all of it. Every seeded
//     row has the shape and values the block path produces for the epochs it
//     then produces for real, and the message server produces when it settles
//     them, which is what the fidelity comparisons in the pull request check.
//  3. Several whole epochs are then produced through the real BeginBlock,
//     EndBlock, epoch finalization, entitlement creation, settlement
//     materialization and configuration promotion, from a state the block path
//     itself verifies on every block: the open epoch's anchor is checked against
//     the canonical history before any boundary decision, so a rewrite the chain
//     would not accept is a halted block, not a passed test.
//
// The result is a head thousands of epochs from its only version anchor with a
// full history behind it, which is the geometry #182 needed and no other fixture
// had. A last horizon at 2^32+1 epochs carries no seeded ledger — four billion
// rows are not a fixture — and exists to catch a cap counted in epochs that reads
// nothing at all; its distant past is empty, and the harness treats "no record of
// epoch 1" as the honest answer it is there.
//
// # What a passing run proves
//
// Every served query answers at every head. The store work of every successful
// query at 3000 epochs is the store work at 10 000, within a few bytes of varint
// drift, in both ledger modes, and a boundary at the head costs what a boundary
// next to the anchor costs; so a read path that visits records in proportion to
// chain age — or to the number of open settlements, which on a stalled chain is
// the same number — or that reads even one extra record per thousand epochs, is
// caught by arithmetic. A generous timer stands behind it. What the gas rule
// does NOT catch is a walk bounded by a constant — a fixed five-seek lookup
// costs the same on every chain — and that is out of scope here, because such a
// walk does not fail with age. All of it is asserted on the real gRPC path with
// the real height header, exactly as a remote consumer reaches the handler.

// agedEpochsDriven is how many whole epochs are produced for real after the
// rewrite, so the head has consensus-made finalized epochs, entitlements and
// settlements behind it whichever horizon it is at.
const agedEpochsDriven = 3

// ledgerMode is the shape of the seeded history behind the head.
type ledgerMode int

const (
	// ledgerBare seeds no history: the epochs before the head were never
	// recorded. Only the geometry is aged.
	ledgerBare ledgerMode = iota
	// ledgerSettled seeds every closed epoch as a healthy operator leaves it:
	// entitlement released in full, settlement finalized.
	ledgerSettled
	// ledgerUnsettled seeds every closed epoch as a stalled pipeline leaves it:
	// nothing released, every settlement open and indexed.
	ledgerUnsettled
)

func (m ledgerMode) String() string {
	switch m {
	case ledgerSettled:
		return "settled"
	case ledgerUnsettled:
		return "unsettled"
	default:
		return "bare"
	}
}

// horizon is one chain age the harness runs at.
type horizon struct {
	// openEpoch is the epoch open at the rewrite.
	openEpoch uint64
	// ledger is the shape of the history seeded for epochs 1..openEpoch-1.
	ledger ledgerMode
}

// horizons are the chain ages the harness runs at. The live testnet's 360-block,
// five-second epochs make the first about two months of chain life and the
// second about seven; #182 struck at epoch 1000, three weeks in. Each seeded
// mode runs at both, so cost can be compared across age within a mode. The last
// is past any cap a uint32 could hold.
var horizons = []horizon{
	{3_001, ledgerSettled}, {10_001, ledgerSettled},
	{3_001, ledgerUnsettled}, {10_001, ledgerUnsettled},
	{1<<32 + 1, ledgerBare},
}

// agedChain is a pinnedChain whose head is thousands of epochs from genesis.
type agedChain struct {
	*pinnedChain
	ledger ledgerMode
	// headEpoch is the epoch open at the head.
	headEpoch uint64
	// headEpochStart is the canonical first height of headEpoch.
	headEpochStart uint64
	// epochEmission is what every closed epoch of this chain minted: the whole
	// history stays inside the first halving tier, and every block was
	// reward-enabled, so each epoch emitted the full subsidy for its length.
	epochEmission sdkmath.Int
	// nominations are the authority handovers left in flight at the head, in
	// role order: one per role at a seeded horizon, none at the bare one.
	nominations []*coreslottypes.PendingAuthorityTransferEntry
}

// bootAgedChain produces a chain whose open epoch at the head is
// openEpoch+agedEpochsDriven, aged as described in the file header.
func bootAgedChain(t *testing.T, h horizon) *agedChain {
	t.Helper()
	require.Greater(t, h.openEpoch, uint64(1), "an aged chain is one that has left epoch 1")

	length := uint64(epochLength)
	// The first height of openEpoch on a chain born at height 1 with this epoch
	// length and no geometry change since: the recurrence EpochStartHeight uses.
	openStart := 1 + (h.openEpoch-1)*length
	chain := bootPinnedChainAt(t, int64(openStart))
	chain.commitThrough(t, int64(openStart))

	// Rewrite the committed state. Writes through a head context are committed
	// by the next block, and that block's BeginBlock verifies the open epoch
	// against the canonical history before it does anything else.
	ctx := chain.headContext()
	rewards := chain.app.RewardsKeeper
	mining := chain.app.MiningKeeper
	coreslot := chain.app.CoreSlotKeeper

	// --- geometry -------------------------------------------------------------
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
	require.NoError(t, rewards.SetActiveBlocks(ctx, h.openEpoch, 1, credited))

	require.NoError(t, mining.LastProcessedRewardEpoch.Set(ctx, h.openEpoch-1))
	// Every block since height 1 was release-enabled, so the clock is the height.
	require.NoError(t, mining.SettlementClock.Set(ctx, openStart))

	// The slot and its policy have been in force since the chain's first block,
	// as a genesis slot's are. The policy seek index is keyed by the height a
	// version starts at, so its entry moves with the version.
	slot, err := coreslot.Slots.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(openStart), slot.ActivatedHeight)
	slot.ActivatedHeight, slot.ActivationEffectiveHeight = 1, 1
	require.NoError(t, coreslot.Slots.Set(ctx, 1, slot))
	policy, err := coreslot.SelectionPolicies.Get(ctx, collections.Join(uint64(1), uint64(1)))
	require.NoError(t, err)
	require.Equal(t, int64(openStart), policy.ValidFromHeight)
	policy.ValidFromHeight = 1
	require.NoError(t, coreslot.SelectionPolicies.Set(ctx, collections.Join(uint64(1), uint64(1)), policy))
	require.NoError(t, coreslot.PolicyStarts.Remove(ctx, collections.Join(uint64(1), int64(openStart))))
	require.NoError(t, coreslot.PolicyStarts.Set(ctx, collections.Join(uint64(1), int64(1)), policy.PolicyVersion))

	// --- ledger ---------------------------------------------------------------
	params, err := rewards.GetParams(ctx)
	require.NoError(t, err)
	maxSupply, ok := sdkmath.NewIntFromString(params.MaxSupply)
	require.True(t, ok)
	subsidy, ok := sdkmath.NewIntFromString(params.InitialBlockSubsidy)
	require.True(t, ok)
	epochEmission, _, err := rewardskeeper.ComputeEpochEmission(
		sdkmath.ZeroInt(), length, maxSupply, subsidy, params.HalvingMode)
	require.NoError(t, err)
	require.True(t, epochEmission.IsPositive())

	state, err := rewards.GetState(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.CurrentEpoch)
	require.Equal(t, openStart, state.CurrentEpochStartHeight)
	state.CurrentEpoch = h.openEpoch
	if h.ledger != ledgerBare {
		state.CumulativeEmitted = seedHistory(t, ctx, chain, h.openEpoch, epochEmission, h.ledger).String()
	}
	require.NoError(t, rewards.SetState(ctx, state))

	// --- authority handovers in flight ----------------------------------------
	// At a seeded horizon both roles are left nominated and never accepted, so the
	// pending-nomination query is asked with its collection full and the record
	// thousands of epochs old by the head. Through the message server, so each
	// record is the one a nomination writes. Every horizon of a mode carries the
	// same two, which keeps its cost comparable across age; the bare horizon
	// carries none and so asks the empty answer.
	var nominations []*coreslottypes.PendingAuthorityTransferEntry
	if h.ledger != ledgerBare {
		nominate := coreslotkeeper.NewMsgServer(coreslot)
		for _, handover := range []struct {
			role      coreslottypes.AuthorityRole
			incumbent string
			nominee   string
		}{
			{coreslottypes.AuthorityRole_AUTHORITY_ROLE_PRIMARY, app.AuthorityAddress(), acc(0x5a)},
			{coreslottypes.AuthorityRole_AUTHORITY_ROLE_EMERGENCY, app.EmergencyAuthorityAddress(), acc(0x5b)},
		} {
			_, err := nominate.NominateAuthority(ctx, &coreslottypes.MsgNominateAuthority{
				Authority: handover.incumbent, Role: handover.role, Nominee: handover.nominee,
			})
			require.NoError(t, err)
			nominations = append(nominations, &coreslottypes.PendingAuthorityTransferEntry{
				Role:     handover.role,
				Transfer: &coreslottypes.PendingAuthorityTransfer{Nominee: handover.nominee, NominatedHeight: ctx.BlockHeight()},
			})
		}
	}

	chain.commitThrough(t, chain.head+agedEpochsDriven*epochLength+5)

	headEpoch := h.openEpoch + agedEpochsDriven
	aged := &agedChain{
		pinnedChain:    chain,
		ledger:         h.ledger,
		headEpoch:      headEpoch,
		headEpochStart: 1 + (headEpoch-1)*length,
		epochEmission:  epochEmission,
		nominations:    nominations,
	}
	assertAllRewardsInvariants(t, chain.app, chain.headContext())
	return aged
}

// seedHistory writes the closed ledger for epochs 1..openEpoch-1 in one of the
// two seeded modes and returns the cumulative emission those epochs minted.
//
// Settled, each epoch is left as a healthy operator leaves it: emission minted,
// the entitlement created and then released in full through the bank to the
// payout address, the settlement materialized, distributed in one chunk and
// finalized by its operator. The ledger then carries no outstanding liability
// from these epochs, and the escrow holds nothing for them, exactly as it would
// after release. Unsettled, each epoch is left as a stalled pipeline leaves it:
// emission minted into escrow and still there, the entitlement unreleased, the
// settlement open and in the OPEN index, the liability carrying every epoch.
//
// The whole history stays in the first halving tier, which is what makes every
// epoch's emission the same closed-form number the block path computes for the
// epochs produced afterwards; the tier is asserted rather than assumed.
func seedHistory(
	t *testing.T, ctx sdk.Context, chain *pinnedChain, openEpoch uint64, epochEmission sdkmath.Int, mode ledgerMode,
) sdkmath.Int {
	t.Helper()
	require.NotEqual(t, ledgerBare, mode, "a bare ledger has nothing to seed")
	settled := mode == ledgerSettled
	rewards := chain.app.RewardsKeeper
	mining := chain.app.MiningKeeper
	length := uint64(epochLength)
	closed := openEpoch - 1
	cumulative := epochEmission.MulRaw(int64(closed))

	params, err := rewards.GetParams(ctx)
	require.NoError(t, err)
	maxSupply, ok := sdkmath.NewIntFromString(params.MaxSupply)
	require.True(t, ok)
	tier, err := rewardskeeper.HalvingTier(cumulative, maxSupply)
	require.NoError(t, err)
	require.Zero(t, tier, "the seeded history must stay inside the first halving tier for its emission to be the closed form")

	cfg, err := rewards.GetCurrentEpochConfig(ctx)
	require.NoError(t, err)

	// The value those epochs minted, by the rewards module, which is the only
	// minter. Settled, it was then released to the payout snapshot the way every
	// entitlement is released: out of escrow, through the bank. Unsettled, it is
	// still in escrow, owed in full.
	coins := sdk.NewCoins(sdk.NewCoin(params.NativeDenom, cumulative))
	require.NoError(t, chain.app.BankKeeper.MintCoins(ctx, rewardstypes.ModuleName, coins))
	released := "0"
	if settled {
		require.NoError(t, chain.app.BankKeeper.SendCoinsFromModuleToAccount(
			ctx, rewardstypes.ModuleName, mustAddr(t, chain.payout), coins))
		released = epochEmission.String()
	} else {
		require.NoError(t, rewards.SetOutstandingEntitlementLiability(ctx, cumulative))
	}

	for epoch := uint64(1); epoch <= closed; epoch++ {
		start := 1 + (epoch-1)*length
		end := epoch * length
		cfgCopy := cfg
		require.NoError(t, rewards.SetFinalizedEpoch(ctx, rewardstypes.EpochReward{
			EpochNumber:                 epoch,
			StartHeight:                 start,
			EndHeight:                   end,
			MintedEmission:              epochEmission.String(),
			CarryIn:                     "0",
			DistributableFees:           "0",
			TreasuryAmount:              "0",
			RewardPool:                  epochEmission.String(),
			AllocatedAmount:             epochEmission.String(),
			CarryOut:                    "0",
			DistributionMethod:          cfg.DistributionMethod,
			RemainderPolicy:             cfg.RemainderPolicy,
			CumulativeEmittedAfterEpoch: epochEmission.MulRaw(int64(epoch)).String(),
			Config:                      &cfgCopy,
			RewardEnabledBlocks:         length,
		}))
		require.NoError(t, rewards.SlotEntitlements.Set(ctx, collections.Join(epoch, uint64(1)),
			rewardstypes.SlotEntitlement{
				SlotId:                         1,
				Epoch:                          epoch,
				TotalBlocksActive:              length,
				EntitlementAmount:              epochEmission.String(),
				ReleasedAmount:                 released,
				PayoutAddress:                  chain.payout,
				RewardConfigVersion:            1,
				SlotStatusAtEpochClose:         coreslottypes.SlotStatus_SLOT_STATUS_ACTIVE,
				ActivationSequenceAtEpochClose: 1,
				CreatedHeight:                  end,
			}))
		// Materialized in the closing block's EndBlock, after the clock ticked, so
		// the anchor is the closing height. Settled: one chunk carried the
		// distribution and the operator finalized in the block after it.
		// Unsettled: the row is as materialization wrote it, and still indexed.
		require.NoError(t, mining.SettlementEpochAnchors.Set(ctx, epoch,
			miningtypes.SettlementEpochAnchor{Epoch: epoch, CreatedSettlementClock: end}))
		settlement := miningtypes.Settlement{
			SlotId:                  1,
			Epoch:                   epoch,
			DistributionModeVersion: 1,
			SettlementMode:          miningtypes.SettlementMode_SETTLEMENT_MODE_TRUSTED_AS,
			SettlementParamsVersion: 1,
		}
		if settled {
			settlement.NextChunkIndex = 1
			settlement.Finalized = true
			settlement.FinalizedHeight = end + 2
			settlement.FinalizationReason = miningtypes.SettlementFinalizationReason_SETTLEMENT_FINALIZATION_REASON_AUTHORIZED_EARLY
		}
		key := collections.Join(uint64(1), epoch)
		require.NoError(t, mining.Settlements.Set(ctx, key, settlement))
		if !settled {
			require.NoError(t, mining.OpenSettlementsBySlot.Set(ctx, key, epoch))
		}
	}
	return cumulative
}

// assertAllRewardsInvariants runs every rewards invariant, including the
// full-scan liability check the block-path helper skips, against real bank and
// module state.
func assertAllRewardsInvariants(t *testing.T, a *app.App, ctx sdk.Context) {
	t.Helper()
	k := a.RewardsKeeper
	for name, invariant := range map[string]func(sdk.Context) (string, bool){
		"supply-cap":                k.SupplyCapInvariant(),
		"cumulative-emitted":        k.CumulativeEmittedInvariant(),
		"module-balance-coverage":   k.ModuleBalanceCoverageInvariant(),
		"entitlement-liability":     k.EntitlementLiabilityInvariant(),
		"denom-correctness":         k.DenomCorrectnessInvariant(),
		"closed-epoch-immutability": k.ClosedEpochImmutabilityInvariant(),
	} {
		msg, broken := invariant(ctx)
		require.Falsef(t, broken, "invariant %s broken: %s", name, msg)
	}
}

// horizonCase is one question a consumer asks at the head.
type horizonCase struct {
	name   string
	method string
	req    protoMessage
	// absent lists the codes that are an honest answer for this question besides
	// success. It is only ever NotFound, and only for a record the chain
	// genuinely never wrote: nothing about its future, and — at the bare horizon
	// alone — nothing about its distant past. Internal and Unknown are refused
	// for every case regardless.
	absent []codes.Code
	// verify inspects a successful reply.
	verify func(t *testing.T, reply any)
}

var notFound = []codes.Code{codes.NotFound}

// pastRecord is the contract for a record of epoch 1: it exists at a seeded
// horizon and must be served; it was never written at the bare one.
func (c *agedChain) pastRecord() []codes.Code {
	if c.ledger != ledgerBare {
		return nil
	}
	return notFound
}

// outstandingEpochs is how many closed epochs are still owed at the head: the
// produced ones always, and every seeded one on an unsettled ledger.
func (c *agedChain) outstandingEpochs() int64 {
	if c.ledger == ledgerUnsettled {
		return int64(c.headEpoch - 1)
	}
	return agedEpochsDriven
}

// seededReleased is the released amount every seeded entitlement carries.
func (c *agedChain) seededReleased() string {
	if c.ledger == ledgerSettled {
		return c.epochEmission.String()
	}
	return "0"
}

func (c *agedChain) horizonCases() []horizonCase {
	head := c.headEpoch
	previous := head - 1
	length := uint64(epochLength)
	emission := c.epochEmission.String()

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
				require.Equal(t, c.expectedCumulativeEmitted().String(), info.State.CumulativeEmitted,
					"the ledger reflects every closed epoch, seeded and produced")
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
				require.Equal(t, emission, epoch.MintedEmission)
				require.Equal(t, c.expectedCumulativeEmitted().String(), epoch.CumulativeEmittedAfterEpoch,
					"the last closed epoch's running total is the ledger's")
			}},
		{name: "rewards epoch reward, epoch 1", method: "/twilight.rewards.v1.Query/EpochReward",
			req: &rewardstypes.QueryEpochRewardRequest{EpochNumber: 1}, absent: c.pastRecord(),
			verify: func(t *testing.T, reply any) {
				epoch := reply.(*rewardstypes.QueryEpochRewardResponse).EpochReward
				require.Equal(t, uint64(1), epoch.EpochNumber)
				require.Equal(t, uint64(1), epoch.StartHeight)
				require.Equal(t, length, epoch.EndHeight)
				require.Equal(t, emission, epoch.MintedEmission)
				require.Equal(t, emission, epoch.CumulativeEmittedAfterEpoch)
			}},
		{name: "rewards epoch reward, next epoch", method: "/twilight.rewards.v1.Query/EpochReward",
			req: &rewardstypes.QueryEpochRewardRequest{EpochNumber: head + 1}, absent: notFound},
		{name: "rewards cumulative emitted", method: "/twilight.rewards.v1.Query/CumulativeEmitted",
			req: &rewardstypes.QueryCumulativeEmittedRequest{},
			verify: func(t *testing.T, reply any) {
				require.Equal(t, c.expectedCumulativeEmitted().String(),
					reply.(*rewardstypes.QueryCumulativeEmittedResponse).CumulativeEmitted)
			}},
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
			req: &rewardstypes.QueryModuleBalancesRequest{},
			verify: func(t *testing.T, reply any) {
				balances := reply.(*rewardstypes.QueryModuleBalancesResponse)
				// The escrow holds exactly what is still owed: the produced epochs'
				// entitlements, plus every seeded one on an unsettled ledger.
				owed := c.epochEmission.MulRaw(c.outstandingEpochs()).String()
				require.Equal(t, owed, balances.OutstandingEntitlementLiability)
				require.Equal(t, owed, balances.RewardsBalance)
			}},
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
				require.Equal(t, emission, entitlement.EntitlementAmount)
				require.Equal(t, "0", entitlement.ReleasedAmount)
			}},
		{name: "rewards slot entitlement, epoch 1", method: "/twilight.rewards.v1.Query/SlotEntitlement",
			req: &rewardstypes.QuerySlotEntitlementRequest{SlotId: 1, Epoch: 1}, absent: c.pastRecord(),
			verify: func(t *testing.T, reply any) {
				entitlement := reply.(*rewardstypes.QuerySlotEntitlementResponse).Entitlement
				require.Equal(t, uint64(1), entitlement.Epoch)
				require.Equal(t, emission, entitlement.EntitlementAmount)
				require.Equal(t, c.seededReleased(), entitlement.ReleasedAmount)
			}},
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
				entitlements := reply.(*rewardstypes.QuerySlotEntitlementsByEpochResponse).Entitlements
				if c.ledger == ledgerBare {
					require.Empty(t, entitlements)
					return
				}
				require.Len(t, entitlements, 1)
				require.Equal(t, c.seededReleased(), entitlements[0].ReleasedAmount)
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
		// Nothing became effective at the open epoch; the identity lookup says so.
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
				require.Equal(t, emission, settlement.RemainingAmount)
				require.Equal(t, uint64(c.head), settlement.CurrentSettlementClock,
					"every block of this chain's life was release-enabled")
			}},
		{name: "mining settlement, epoch 1", method: "/twilight.mining.v1.Query/Settlement",
			req: &miningtypes.QuerySettlementRequest{SlotId: 1, Epoch: 1}, absent: c.pastRecord(),
			verify: func(t *testing.T, reply any) {
				settlement := reply.(*miningtypes.QuerySettlementResponse)
				require.Equal(t, uint64(1), settlement.Settlement.Epoch)
				require.Equal(t, length, settlement.CreatedSettlementClock, "anchored at the closing block of epoch 1")
				if c.ledger == ledgerSettled {
					require.True(t, settlement.Settlement.Finalized)
					require.Equal(t, "0", settlement.RemainingAmount)
					require.False(t, settlement.PermissionlessFinalizationNow, "there is nothing left to finalize")
					return
				}
				require.False(t, settlement.Settlement.Finalized)
				require.Equal(t, emission, settlement.RemainingAmount, "nothing was ever released")
				require.True(t, settlement.PermissionlessFinalizationNow,
					"its window closed thousands of epochs ago; anyone may finalize it")
			}},
		{name: "mining settlement, next epoch", method: "/twilight.mining.v1.Query/Settlement",
			req: &miningtypes.QuerySettlementRequest{SlotId: 1, Epoch: head + 1}, absent: notFound},
		{name: "mining open settlements, first page", method: "/twilight.mining.v1.Query/OpenSettlements",
			req: &miningtypes.QueryOpenSettlementsRequest{SlotId: 1},
			verify: func(t *testing.T, reply any) {
				page := reply.(*miningtypes.QueryOpenSettlementsResponse)
				switch c.ledger {
				case ledgerSettled:
					// The budget is on rows INSPECTED, and the first hundred rows of a
					// settled history are finalized. That the page comes back empty
					// with a cursor is the documented contract, not a defect; the
					// cursor walk below proves the open ones are reachable.
					require.Empty(t, page.Settlements)
					require.NotEmpty(t, page.Pagination.NextKey, "a settled history continues past the first page")
				case ledgerUnsettled:
					// The first hundred rows are all open, and all owed.
					require.Len(t, page.Settlements, 100)
					require.Equal(t, uint64(1), page.Settlements[0].Epoch)
					require.NotEmpty(t, page.Pagination.NextKey, "a stalled history continues past the first page")
				default:
					require.Len(t, page.Settlements, agedEpochsDriven, "one open settlement per epoch produced for real")
					require.Empty(t, page.Pagination.NextKey)
				}
			}},
		{name: "mining settlement clock", method: "/twilight.mining.v1.Query/SettlementClock",
			req: &miningtypes.QuerySettlementClockRequest{},
			verify: func(t *testing.T, reply any) {
				require.Equal(t, uint64(c.head), reply.(*miningtypes.QuerySettlementClockResponse).SettlementClock)
			}},
		{name: "mining distribution mode version 1", method: "/twilight.mining.v1.Query/DistributionModeVersion",
			req: &miningtypes.QueryDistributionModeVersionRequest{Version: 1}},
		// No second version was ever promoted; the exact lookup says so.
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
		{name: "mining economic address", method: economicAddress,
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
				slot := reply.(*coreslottypes.QueryCoreSlotResponse).Slot
				require.Equal(t, c.credential, slot.SettlementAddress)
				require.Equal(t, int64(1), slot.ActivatedHeight, "a genesis slot, active since the first block")
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
			req:    &coreslottypes.QuerySelectionPolicyAtHeightRequest{SlotId: 1, AtHeight: c.head},
			verify: c.verifyGenesisPolicy},
		{name: "coreslot selection policy at height 1", method: "/twilight.coreslot.v1.Query/SelectionPolicyAtHeight",
			req:    &coreslottypes.QuerySelectionPolicyAtHeightRequest{SlotId: 1, AtHeight: 1},
			verify: c.verifyGenesisPolicy},
		// Nothing pending is a success with an empty list, never an absence.
		{name: "coreslot pending authority transfers", method: "/twilight.coreslot.v1.Query/PendingAuthorityTransfers",
			req: &coreslottypes.QueryPendingAuthorityTransfersRequest{},
			verify: func(t *testing.T, reply any) {
				transfers := reply.(*coreslottypes.QueryPendingAuthorityTransfersResponse).Transfers
				if len(c.nominations) == 0 {
					require.Empty(t, transfers)
					return
				}
				require.Equal(t, c.nominations, transfers, "both handovers, in role order, as nominated")
			}},
	}
}

// expectedCumulativeEmitted is what every closed epoch minted, seeded and
// produced: at a seeded horizon that is one emission per epoch before the head;
// at the bare one, only the produced epochs.
func (c *agedChain) expectedCumulativeEmitted() sdkmath.Int {
	closed := int64(agedEpochsDriven)
	if c.ledger != ledgerBare {
		closed = int64(c.headEpoch - 1)
	}
	return c.epochEmission.MulRaw(closed)
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

// verifyGenesisPolicy checks that the policy resolved at a height is the one the
// slot has had since the chain's first block.
func (c *agedChain) verifyGenesisPolicy(t *testing.T, reply any) {
	policy := reply.(*coreslottypes.QuerySelectionPolicyResponse).Policy
	require.Equal(t, uint64(1), policy.PolicyVersion)
	require.Equal(t, int64(1), policy.ValidFromHeight)
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

// servedTwilightMethods lists every method the app registers under a twilight
// service, from the registration itself rather than from any list kept beside it.
func servedTwilightMethods(q *headerQuerier) []string {
	var methods []string
	for method := range q.methods {
		if strings.HasPrefix(method, "/twilight.") {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// declaredQueryMethods lists every method the three modules' generated query
// service descriptors declare. It is the other half of the enumeration check:
// the served set must be exactly this, so a fourth module, or a query service
// under another name, cannot be served without appearing here.
func declaredQueryMethods() []string {
	probe := &headerQuerier{methods: map[string]grpc.MethodDesc{}, handlers: map[string]any{}}
	rewardstypes.RegisterQueryServer(probe, nil)
	miningtypes.RegisterQueryServer(probe, nil)
	coreslottypes.RegisterQueryServer(probe, nil)
	var methods []string
	for method := range probe.methods {
		methods = append(methods, method)
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

// queryClockBound is the wall-clock ceiling for one query at the head.
// Deliberately generous — the real queries take microseconds — so only a walk
// over the chain's life could reach it, and so CI noise cannot.
const queryClockBound = 50 * time.Millisecond

// storeWorkDrift is how much the gas of one query may differ between two chain
// ages before the difference is read as extra store work.
//
// Gas is metered per record read (1000 flat), per iterator step (30 flat) and per
// byte read (3). The bytes differ a little with age on their own: a height in the
// millions is one varint byte longer than one in the thousands, and a reply that
// reads a handful of height-bearing records drifts by a handful of bytes. Sixty
// gas is twenty such bytes. It is less than two iterator steps and a sixteenth
// of one point read, so a walk that visits records in proportion to chain age —
// or even one extra record per thousand epochs — cannot hide inside it. A walk
// bounded by a constant, which costs the same at every age, is not what this
// detects and is not what fails with age.
const storeWorkDrift = 60

// requireSameStoreWork asserts two queries read the same records.
func requireSameStoreWork(t *testing.T, want, got uint64, msg string, args ...any) {
	t.Helper()
	var difference uint64
	if got > want {
		difference = got - want
	} else {
		difference = want - got
	}
	require.LessOrEqualf(t, difference, uint64(storeWorkDrift), msg+" (%d gas against %d)", append(args, got, want)...)
	// The constant above is only meaningful while it stays well under one read.
	require.Less(t, uint64(storeWorkDrift), storetypes.KVGasConfig().ReadCostFlat/10)
}

// horizonCosts is the store work of every successful case at one head, by case.
type horizonCosts map[string]queryCost

// TestEveryQueryAnswersAtALongHorizon is the harness.
//
// It runs once per horizon, and the cost of every successful case is carried
// across the runs so the last assertions can say the thing that matters: a chain
// three times older did exactly the same work, query by query, whatever shape
// its history has.
func TestEveryQueryAnswersAtALongHorizon(t *testing.T) {
	costs := map[horizon]horizonCosts{}

	runHorizon := func(h horizon) {
		t.Run(fmt.Sprintf("%s_open_epoch_%d", h.ledger, h.openEpoch), func(t *testing.T) {
			started := time.Now()
			chain := bootAgedChain(t, h)
			t.Logf("aged to epoch %d at height %d in %s (%s ledger)",
				chain.headEpoch, chain.head, time.Since(started).Round(time.Millisecond), h.ledger)
			querier := newHeaderQuerier(chain.app)

			// Every declared query is served, every served twilight method is in
			// the table, and every table entry is served. A query added to a module
			// without a case here fails the run; so does a service the filter
			// would otherwise not know to look for.
			cases := chain.horizonCases()
			covered := map[string]bool{}
			for _, testCase := range cases {
				covered[testCase.method] = true
			}
			served := servedTwilightMethods(querier)
			require.Equal(t, declaredQueryMethods(), served,
				"the app serves exactly the query methods the three modules declare")
			for _, method := range served {
				require.Truef(t, covered[method],
					"%s is served but has no long-horizon case; every query must be asked at the head", method)
			}
			for method := range covered {
				_, isServed := querier.methods[method]
				require.Truef(t, isServed, "%s has a case but is not served", method)
			}

			// Asked twice: with no height, as a consumer asking about now does, and
			// pinned to the head, as a reconciling worker does. The head is the
			// latest height, so the two must return byte-identical answers.
			succeeded := map[string]bool{}
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					var answers [][]byte
					for _, height := range []int64{0, chain.head} {
						reply, err := querier.call(t, testCase.method, testCase.req, height)
						if err == nil {
							require.NotNil(t, reply)
							if testCase.verify != nil {
								testCase.verify(t, reply)
							}
							bytes, err := reply.(protoMessage).Marshal()
							require.NoError(t, err)
							answers = append(answers, bytes)
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
					require.NotEqual(t, 1, len(answers), "no height and the pinned head must answer the same way")
					if len(answers) == 2 {
						require.Equal(t, answers[0], answers[1], "no height and the pinned head must give the same answer")
						succeeded[testCase.name] = true
					}
				})
			}

			// The open settlements are reachable by cursor, however deep the
			// settled history in front of them.
			chain.walkOpenSettlements(t, querier)

			// Every successful question, costed at the head.
			atHead := horizonCosts{}
			for _, testCase := range cases {
				if !succeeded[testCase.name] || testCase.method == economicAddress {
					// The address rule is process configuration and reads no state.
					continue
				}
				cost := measureQuery(t, chain.app, testCase.method, testCase.req, chain.head)
				require.Lessf(t, cost.wall, queryClockBound, "%s took %s at epoch %d", testCase.name, cost.wall, chain.headEpoch)
				atHead[testCase.name] = cost
			}
			costs[h] = atHead

			// The query #182 broke, against the same question next to the anchor.
			nearAnchor := measureQuery(t, chain.app, "/twilight.rewards.v1.Query/EpochBoundaries",
				&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 2}, chain.head)
			requireSameStoreWork(t, nearAnchor.gas, atHead["rewards epoch boundaries, current epoch"].gas,
				"the store work of a boundary %d epochs from the anchor must be the work of one next to it; "+
					"anything more is a walk measured from genesis", chain.headEpoch-1)
			t.Logf("head epoch %d: EpochBoundaries %d gas in %s, EpochInfo %d gas in %s",
				chain.headEpoch,
				atHead["rewards epoch boundaries, current epoch"].gas, atHead["rewards epoch boundaries, current epoch"].wall,
				atHead["rewards epoch info"].gas, atHead["rewards epoch info"].wall)
		})
	}

	// The seeded horizons run and are compared FIRST, mode by mode. A read path
	// that walks the chain's whole life is caught between the two ages of a mode
	// in about a second; run after the bare horizon it would still be caught, but
	// only after four billion epochs' worth of that walk.
	var youngest *horizon
	for _, mode := range []ledgerMode{ledgerSettled, ledgerUnsettled} {
		var ages []horizon
		for _, h := range horizons {
			if h.ledger == mode {
				runHorizon(h)
				ages = append(ages, h)
			}
		}
		require.GreaterOrEqualf(t, len(ages), 2, "two %s horizons are needed to compare work across age", mode)
		if youngest == nil {
			youngest = &ages[0]
		}

		// Query by query, the older chain must have done the same work as the
		// younger one with the same shape of history behind it.
		young := costs[ages[0]]
		require.NotEmpty(t, young, "the youngest %s horizon must have costed its cases", mode)
		for _, older := range ages[1:] {
			for name, cost := range young {
				old, measured := costs[older][name]
				require.Truef(t, measured, "%s was costed at %s open epoch %d but not at %d", name, mode, ages[0].openEpoch, older.openEpoch)
				requireSameStoreWork(t, cost.gas, old.gas,
					"%s did more store work at %s open epoch %d than at %d; its cost grows with chain age",
					name, mode, older.openEpoch, ages[0].openEpoch)
			}
		}
	}

	// A failure inside a subtest above does not stop the parent. Stop it here:
	// a read path that refuses young epochs and walks old ones would otherwise
	// go on to the bare horizon and walk four billion of them, and in a
	// non-verbose run the failures already recorded would not be printed until
	// the whole test timed out.
	if t.Failed() {
		t.Fatal("seeded horizons failed; not running the bare 2^32 horizon")
	}

	// The bare horizon has no history behind it, so only the questions that
	// read no history are compared to it.
	for _, h := range horizons {
		if h.ledger != ledgerBare {
			continue
		}
		runHorizon(h)
		for _, name := range []string{"rewards epoch boundaries, current epoch", "rewards epoch info"} {
			young, measured := costs[*youngest][name]
			require.Truef(t, measured, "%s was not costed at open epoch %d", name, youngest.openEpoch)
			old, measured := costs[h][name]
			require.Truef(t, measured, "%s was not costed at open epoch %d", name, h.openEpoch)
			requireSameStoreWork(t, young.gas, old.gas,
				"%s did more store work at open epoch %d than at %d; its cost grows with chain age", name, h.openEpoch, youngest.openEpoch)
		}
	}
	require.Len(t, costs, len(horizons), "every horizon must have run")
}

// walkOpenSettlements follows the OpenSettlements cursor from the first page to
// the last and requires the open set to be exactly the epochs still owed: the
// produced ones, and every seeded one on an unsettled ledger.
//
// On a seeded chain this is a walk over the whole history, one hundred rows a
// page: the query's budget is on rows inspected, so a worker with a long history
// behind it pages through all of it, whether to skip what is finalized or to
// list what is still open. Each page is bounded, so the query itself does not
// fail with age; the number of pages is what grows, and it is logged rather than
// bounded here because it is the documented shape of the surface, not a defect
// this harness owns.
func (c *agedChain) walkOpenSettlements(t *testing.T, querier *headerQuerier) {
	t.Helper()
	var open []uint64
	var key []byte
	pages := 0
	for {
		reply, err := querier.call(t, "/twilight.mining.v1.Query/OpenSettlements",
			&miningtypes.QueryOpenSettlementsRequest{SlotId: 1, Pagination: pageAfter(key)}, c.head)
		require.NoError(t, err)
		page := reply.(*miningtypes.QueryOpenSettlementsResponse)
		pages++
		for _, settlement := range page.Settlements {
			open = append(open, settlement.Epoch)
		}
		key = page.Pagination.NextKey
		if len(key) == 0 {
			break
		}
		require.Less(t, pages, int(c.headEpoch/100)+3, "the cursor walk must terminate within the history's length")
	}
	var owed []uint64
	for epoch := c.headEpoch - uint64(c.outstandingEpochs()); epoch < c.headEpoch; epoch++ {
		owed = append(owed, epoch)
	}
	require.Equal(t, owed, open, "the open settlements are exactly the epochs still owed")
	t.Logf("%d open settlements reached after %d pages at epoch %d", len(open), pages, c.headEpoch)
}

// pageAfter continues a listing from a cursor, or starts it when there is none.
func pageAfter(key []byte) *query.PageRequest {
	if len(key) == 0 {
		return nil
	}
	return &query.PageRequest{Key: key}
}
