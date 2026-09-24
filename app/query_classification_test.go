package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"

	"github.com/twilight-project/twilight-core/app"
	coreslotkeeper "github.com/twilight-project/twilight-core/x/coreslot/keeper"
	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
	miningtypes "github.com/twilight-project/twilight-core/x/mining/types"
	rewardstypes "github.com/twilight-project/twilight-core/x/rewards/types"
)

// One classification contract, asserted across every module that serves queries.
//
// The three custom modules answer a caller in exactly three ways:
//
//	InvalidArgument   the request itself is malformed
//	NotFound          genuine, modeled absence
//	Internal          state that exists and cannot be trusted
//
// The failure this guards against is not a wrong code in the abstract. It is a
// handler returning its keeper's error raw, which reaches a consumer as Unknown —
// a code that says nothing, and which a client can only handle by pattern-matching
// on message text. x/coreslot did that on nine of thirteen handlers while the
// mapping helpers it needed sat in the same file, so the guard is a table rather
// than a few spot checks: a handler added later is only covered if the table is
// the thing that grows.

// classificationCase is one query asked for something that cannot be answered.
type classificationCase struct {
	name   string
	method string
	req    protoMessage
	want   codes.Code
}

func absenceCases() []classificationCase {
	// A hex string of the right shape that no chain will have reserved.
	const unknownConsensus = "0011223344556677889900112233445566778899"

	return []classificationCase{
		// --- x/coreslot: absence is an ordinary answer ---------------------
		{"coreslot slot", "/twilight.coreslot.v1.Query/CoreSlot",
			&coreslottypes.QueryCoreSlotRequest{SlotId: 999}, codes.NotFound},
		{"coreslot slot by operator", "/twilight.coreslot.v1.Query/CoreSlotByOperator",
			&coreslottypes.QueryCoreSlotByOperatorRequest{OperatorAddress: acc(0x7e)}, codes.NotFound},
		{"coreslot slot by consensus address", "/twilight.coreslot.v1.Query/CoreSlotByConsensusAddress",
			&coreslottypes.QueryCoreSlotByConsensusAddressRequest{ConsensusAddress: unknownConsensus}, codes.NotFound},
		{"coreslot reservation", "/twilight.coreslot.v1.Query/ReservedConsensusAddress",
			&coreslottypes.QueryReservedConsensusAddressRequest{ConsensusAddress: unknownConsensus}, codes.NotFound},
		{"coreslot reward weight", "/twilight.coreslot.v1.Query/RewardWeight",
			&coreslottypes.QueryRewardWeightRequest{SlotId: 999}, codes.NotFound},
		{"coreslot selection policy", "/twilight.coreslot.v1.Query/SelectionPolicy",
			&coreslottypes.QuerySelectionPolicyRequest{SlotId: 999}, codes.NotFound},
		{"coreslot selection policy version", "/twilight.coreslot.v1.Query/SelectionPolicyVersion",
			&coreslottypes.QuerySelectionPolicyVersionRequest{SlotId: 999, PolicyVersion: 1}, codes.NotFound},
		{"coreslot selection policy at height", "/twilight.coreslot.v1.Query/SelectionPolicyAtHeight",
			&coreslottypes.QuerySelectionPolicyAtHeightRequest{SlotId: 999, AtHeight: 2}, codes.NotFound},

		// --- x/rewards -----------------------------------------------------
		{"rewards epoch", "/twilight.rewards.v1.Query/EpochReward",
			&rewardstypes.QueryEpochRewardRequest{EpochNumber: 999}, codes.NotFound},
		{"rewards entitlement", "/twilight.rewards.v1.Query/SlotEntitlement",
			&rewardstypes.QuerySlotEntitlementRequest{SlotId: 999, Epoch: 999}, codes.NotFound},
		{"rewards config version", "/twilight.rewards.v1.Query/RewardConfigVersion",
			&rewardstypes.QueryRewardConfigVersionRequest{Version: 999}, codes.NotFound},

		// --- x/mining ------------------------------------------------------
		{"mining settlement", "/twilight.mining.v1.Query/Settlement",
			&miningtypes.QuerySettlementRequest{SlotId: 999, Epoch: 999}, codes.NotFound},
		{"mining distribution mode version", "/twilight.mining.v1.Query/DistributionModeVersion",
			&miningtypes.QueryDistributionModeVersionRequest{Version: 999}, codes.NotFound},
		{"mining selection params version", "/twilight.mining.v1.Query/SelectionParamsVersion",
			&miningtypes.QuerySelectionParamsVersionRequest{Version: 999}, codes.NotFound},
		{"mining settlement params version", "/twilight.mining.v1.Query/SettlementParamsVersion",
			&miningtypes.QuerySettlementParamsVersionRequest{Version: 999}, codes.NotFound},
	}
}

func malformedCases() []classificationCase {
	return []classificationCase{
		// The arm x/coreslot was missing entirely: input the caller got wrong.
		{"coreslot non-hex consensus address", "/twilight.coreslot.v1.Query/CoreSlotByConsensusAddress",
			&coreslottypes.QueryCoreSlotByConsensusAddressRequest{ConsensusAddress: "zzzz-not-hex"}, codes.InvalidArgument},
		{"mining target epoch zero", "/twilight.mining.v1.Query/TargetEpochInterpretation",
			&miningtypes.QueryTargetEpochInterpretationRequest{TargetEpoch: 0}, codes.InvalidArgument},
		{"mining slot id zero", "/twilight.mining.v1.Query/OpenSettlements",
			&miningtypes.QueryOpenSettlementsRequest{SlotId: 0}, codes.InvalidArgument},
		{"rewards epoch zero", "/twilight.rewards.v1.Query/EpochBoundaries",
			&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 0}, codes.InvalidArgument},
	}
}

// TestQueriesClassifyAbsenceAsNotFound is the regression this exists for.
//
// Every one of these previously had, or could regress to, a raw keeper error
// surfacing as Unknown. A consumer receiving Unknown cannot distinguish "no such
// object" from "the chain is broken", and the only recourse is to match on error
// text — which is what the downstream integrator was forced into.
func TestQueriesClassifyAbsenceAsNotFound(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)
	querier := newHeaderQuerier(chain.app)

	for _, testCase := range absenceCases() {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, classify(t, querier, testCase),
				"absence must be reported as %s, never as an untyped Unknown", testCase.want)
		})
	}
}

// TestAnOldEpochIsNotRefusedForItsAge pins #182 through the gRPC path a
// consumer uses.
//
// EpochBoundaries once counted its projection horizon in epochs from the
// governing version, which on a single-version chain is genesis — so every
// boundary past the horizon was refused, past epochs included, and a live chain
// hit it about three weeks in. The horizon now counts scheduled entries crossed,
// so an epoch far from genesis answers with the same recurrence history uses.
//
// The horizon refusal is held at the handler
// (TestEpochBoundariesDistinguishesAbsenceFromCorruption), because no transaction
// can yet write the schedule entries needed to reach it from a booted chain. The
// other refusal — an epoch too large to have a representable start height — is
// reachable from any client and is pinned end to end below.
func TestAnOldEpochIsNotRefusedForItsAge(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)

	var near, far rewardstypes.QueryEpochBoundariesResponse
	queryAtHeight(t, chain.app, "/twilight.rewards.v1.Query/EpochBoundaries",
		&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 2}, &near, chain.head)
	queryAtHeight(t, chain.app, "/twilight.rewards.v1.Query/EpochBoundaries",
		&rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 100_000_000}, &far, chain.head)

	require.Equal(t, uint64(2), near.EpochNumber)
	require.NotZero(t, near.EpochLengthBlocks)
	require.Equal(t, uint64(100_000_000), far.EpochNumber)
	require.Equal(t, near.EpochLengthBlocks, far.EpochLengthBlocks)
	require.Equal(t, near.StartHeight+(100_000_000-2)*near.EpochLengthBlocks, far.StartHeight,
		"with no schedule, a far epoch is the history recurrence, not a refusal")
}

// TestARefusalToComputeIsNotAnAbsence covers the fourth answer through the gRPC
// path a consumer uses.
//
// An epoch whose start height does not fit in a uint64 has no derivable
// boundaries. That is the chain declining to compute, so it arrives as
// OutOfRange: not NotFound, which would say the epoch has no configuration, and
// not Internal, which would let any client make the node report its own state
// as untrustworthy (the classification Internal is reserved for).
func TestARefusalToComputeIsNotAnAbsence(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)
	querier := newHeaderQuerier(chain.app)

	unrepresentable := classificationCase{
		name:   "rewards epoch whose start height overflows",
		method: "/twilight.rewards.v1.Query/EpochBoundaries",
		req:    &rewardstypes.QueryEpochBoundariesRequest{EpochNumber: 1 << 62},
		want:   codes.OutOfRange,
	}
	require.Equal(t, codes.OutOfRange, classify(t, querier, unrepresentable),
		"an unrepresentable boundary is a refusal, not a missing configuration and not corruption")
}

// TestNoPendingNominationIsAnAnswerNotAnAbsence pins the fourth kind of query:
// one that asks what is in flight rather than after a particular object, and so
// has no absence arm and no malformed arm at all. PendingAuthorityTransfers takes
// no argument, and "nothing is pending" is its ordinary reply — a success with an
// empty list. Answering NotFound would make the most reassuring answer an
// operator can get look like an error, and teach clients to treat that error as
// safe to ignore.
//
// It then proves the answer tracks committed state through the height header: a
// nomination appears at the height it was committed and is absent at the height
// before, which is how an operator dates an unexpected one.
func TestNoPendingNominationIsAnAnswerNotAnAbsence(t *testing.T) {
	const method = "/twilight.coreslot.v1.Query/PendingAuthorityTransfers"
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)
	querier := newHeaderQuerier(chain.app)

	ask := func(height int64) []*coreslottypes.PendingAuthorityTransferEntry {
		t.Helper()
		reply, err := querier.call(t, method, &coreslottypes.QueryPendingAuthorityTransfersRequest{}, height)
		require.NoError(t, err, "no pending nomination is an answer, never NotFound")
		return reply.(*coreslottypes.QueryPendingAuthorityTransfersResponse).Transfers
	}
	require.Empty(t, ask(0))

	nominee := acc(0x5a)
	_, err := coreslotkeeper.NewMsgServer(chain.app.CoreSlotKeeper).NominateAuthority(chain.headContext(),
		&coreslottypes.MsgNominateAuthority{
			Authority: app.AuthorityAddress(),
			Role:      coreslottypes.AuthorityRole_AUTHORITY_ROLE_PRIMARY,
			Nominee:   nominee,
		})
	require.NoError(t, err)
	// The head context stamps the head's height, and the write is committed by the
	// next block; a signed transaction would stamp the block that includes it.
	// Either way the record is absent from every version before the one holding it.
	before := chain.head
	chain.commitThrough(t, before+1)

	require.Equal(t, []*coreslottypes.PendingAuthorityTransferEntry{{
		Role:     coreslottypes.AuthorityRole_AUTHORITY_ROLE_PRIMARY,
		Transfer: &coreslottypes.PendingAuthorityTransfer{Nominee: nominee, NominatedHeight: before},
	}}, ask(chain.head))
	require.Empty(t, ask(before), "a height before the nomination was committed must not show it")
}

// TestQueriesClassifyMalformedRequestsAsInvalidArgument covers the arm that is
// about the CALLER rather than the chain.
//
// It is separated from absence deliberately: the two are told apart by the code
// alone, and a consumer that confused them would retry a permanently malformed
// request forever, or give up on an object that simply does not exist yet.
func TestQueriesClassifyMalformedRequestsAsInvalidArgument(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)
	querier := newHeaderQuerier(chain.app)

	for _, testCase := range malformedCases() {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, classify(t, querier, testCase))
		})
	}
}

// TestNoQueryAnswersWithABodyAlongsideAnError pins the second half of the defect.
//
// A handler that returns both hands a client a zero-valued object next to an
// error — a CoreSlot with slot_id 0 reads exactly like a real answer to code that
// checks the body first. The error is authoritative, so there must be nothing
// beside it to mistake for data.
func TestNoQueryAnswersWithABodyAlongsideAnError(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, 3)
	querier := newHeaderQuerier(chain.app)

	for _, testCase := range append(absenceCases(), malformedCases()...) {
		t.Run(testCase.name, func(t *testing.T) {
			data, err := testCase.req.Marshal()
			require.NoError(t, err)
			desc, served := querier.methods[testCase.method]
			require.Truef(t, served, "%s is not served", testCase.method)

			reply, callErr := desc.Handler(querier.handlers[testCase.method], incomingHeight(0),
				func(arg any) error { return arg.(protoMessage).Unmarshal(data) }, nil)
			require.Error(t, callErr)
			require.Nil(t, reply, "an error answer must carry no body")
		})
	}
}

// classify runs one query through the gRPC path a consumer uses and returns the
// code it receives.
func classify(t *testing.T, querier *headerQuerier, testCase classificationCase) codes.Code {
	t.Helper()
	data, err := testCase.req.Marshal()
	require.NoError(t, err)

	desc, served := querier.methods[testCase.method]
	require.Truef(t, served, "%s is not served over gRPC", testCase.method)

	_, callErr := desc.Handler(querier.handlers[testCase.method], incomingHeight(0),
		func(arg any) error { return arg.(protoMessage).Unmarshal(data) }, nil)
	require.Errorf(t, callErr, "%s answered successfully; the case is no longer unanswerable", testCase.name)

	status, ok := grpcstatus.FromError(callErr)
	require.Truef(t, ok,
		"%s returned an error carrying no gRPC status at all (%v) — the untyped Unknown case",
		testCase.name, callErr)
	return status.Code()
}

func incomingHeight(height int64) context.Context {
	return metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(height, 10)))
}
