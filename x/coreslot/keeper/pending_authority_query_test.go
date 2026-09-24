package keeper_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/x/coreslot/keeper"
	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

// The pending-nomination query (#150) is the live view of an authority handover
// in flight. An unexpected nomination is the first visible sign of a stolen
// authority key, so the property that matters most is the negative one: the
// answer "nothing is pending" must be unambiguous, and must never be given when
// something is pending or when the record of it cannot be read.
//
// Every case goes through the real gRPC query router and the generated client, as
// the status-code tests in this package do, so the code asserted is the code a
// client receives.

const (
	primaryRole   = types.AuthorityRole_AUTHORITY_ROLE_PRIMARY
	emergencyRole = types.AuthorityRole_AUTHORITY_ROLE_EMERGENCY
)

// pendingSetup is authoritySetup with the raw store key as well, so a case can
// prove what the query does not write and corrupt what it reads.
func pendingSetup(t *testing.T) (types.MsgServer, keeper.Keeper, sdk.Context, string, string, *storetypes.KVStoreKey) {
	t.Helper()
	k, ctx, _, _, storeKey := setupWithRawStore(t)
	primary, emergency := addr(0x11), addr(0x12)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams(primary, emergency)))
	return keeper.NewMsgServer(k), k, ctx, primary, emergency, storeKey
}

func pendingTransfers(t *testing.T, k keeper.Keeper, ctx sdk.Context) []*types.PendingAuthorityTransferEntry {
	t.Helper()
	resp, err := policyQueryClient(t, k, ctx).PendingAuthorityTransfers(context.Background(),
		&types.QueryPendingAuthorityTransfersRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp.Transfers
}

func pendingEntry(role types.AuthorityRole, nominee string, height int64) *types.PendingAuthorityTransferEntry {
	return &types.PendingAuthorityTransferEntry{
		Role:     role,
		Transfer: &types.PendingAuthorityTransfer{Nominee: nominee, NominatedHeight: height},
	}
}

// requireGaugeAgrees holds the query to the telemetry gauge an operator alerts on
// (twilight_coreslot_pending_authority_nomination{role}). The gauge says a
// handover is open and the query says to whom; if they could disagree, the alert
// would point at an answer that denies it.
func requireGaugeAgrees(t *testing.T, k keeper.Keeper, ctx sdk.Context, transfers []*types.PendingAuthorityTransferEntry) {
	t.Helper()
	listed := map[types.AuthorityRole]bool{}
	for _, entry := range transfers {
		listed[entry.Role] = true
	}
	snap, err := k.TelemetrySnapshot(ctx)
	require.NoError(t, err)
	require.Equal(t, snap.PrimaryNominationPending, listed[primaryRole], "the primary gauge and the query disagree")
	require.Equal(t, snap.EmergencyNominationPending, listed[emergencyRole], "the emergency gauge and the query disagree")
}

func TestPendingAuthorityTransfersQueryWithNoNomination(t *testing.T) {
	_, k, ctx, _, _, _ := pendingSetup(t)

	resp, err := policyQueryClient(t, k, ctx).PendingAuthorityTransfers(context.Background(),
		&types.QueryPendingAuthorityTransfersRequest{})
	require.NoError(t, err, "no nomination is an answer, not an error — and in particular not NotFound")
	require.Empty(t, resp.Transfers)
	requireGaugeAgrees(t, k, ctx, resp.Transfers)

	// The rendering an operator reads must say so explicitly. The CLI and the REST
	// gateway both marshal with defaults emitted; an omitted field would leave
	// "nothing pending" indistinguishable from "this node does not know the field".
	rendered, err := codec.ProtoMarshalJSON(resp, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"transfers":[]}`, string(rendered))
}

func TestPendingAuthorityTransfersQueryShowsEachRole(t *testing.T) {
	for name, role := range map[string]types.AuthorityRole{"primary": primaryRole, "emergency": emergencyRole} {
		t.Run(name, func(t *testing.T) {
			ms, k, ctx, primary, emergency, _ := pendingSetup(t)
			incumbent := primary
			if role == emergencyRole {
				incumbent = emergency
			}
			ctx = ctx.WithBlockHeight(42)
			_, err := ms.NominateAuthority(ctx, &types.MsgNominateAuthority{
				Authority: incumbent, Role: role, Nominee: addr(0x21),
			})
			require.NoError(t, err)

			transfers := pendingTransfers(t, k, ctx)
			require.Equal(t, []*types.PendingAuthorityTransferEntry{pendingEntry(role, addr(0x21), 42)}, transfers,
				"exactly the nominated role, with the nominee and the height it was nominated at")
			requireGaugeAgrees(t, k, ctx, transfers)
		})
	}
}

func TestPendingAuthorityTransfersQueryShowsBothRolesInRoleOrder(t *testing.T) {
	ms, k, ctx, primary, emergency, _ := pendingSetup(t)

	// Emergency first, so role order cannot be mistaken for insertion order.
	_, err := ms.NominateAuthority(ctx.WithBlockHeight(7), &types.MsgNominateAuthority{
		Authority: emergency, Role: emergencyRole, Nominee: addr(0x22),
	})
	require.NoError(t, err)
	_, err = ms.NominateAuthority(ctx.WithBlockHeight(9), &types.MsgNominateAuthority{
		Authority: primary, Role: primaryRole, Nominee: addr(0x21),
	})
	require.NoError(t, err)

	transfers := pendingTransfers(t, k, ctx)
	require.Equal(t, []*types.PendingAuthorityTransferEntry{
		pendingEntry(primaryRole, addr(0x21), 9),
		pendingEntry(emergencyRole, addr(0x22), 7),
	}, transfers)
	requireGaugeAgrees(t, k, ctx, transfers)

	// The query answers the same thing genesis export records, so a live view and
	// a captured state cannot describe a different handover.
	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, exported.PendingAuthorityTransfers, transfers)
}

// A replacement nomination is what an attacker holding the incumbent key would
// do to redirect a legitimate handover, so the query must show the CURRENT
// nominee, not the first.
func TestPendingAuthorityTransfersQueryShowsTheReplacementNominee(t *testing.T) {
	ms, k, ctx, primary, _, _ := pendingSetup(t)
	_, err := ms.NominateAuthority(ctx.WithBlockHeight(5), &types.MsgNominateAuthority{
		Authority: primary, Role: primaryRole, Nominee: addr(0x21),
	})
	require.NoError(t, err)
	_, err = ms.NominateAuthority(ctx.WithBlockHeight(6), &types.MsgNominateAuthority{
		Authority: primary, Role: primaryRole, Nominee: addr(0x31),
	})
	require.NoError(t, err)

	require.Equal(t, []*types.PendingAuthorityTransferEntry{pendingEntry(primaryRole, addr(0x31), 6)},
		pendingTransfers(t, k, ctx))
}

func TestPendingAuthorityTransfersQueryClearsOnAcceptAndCancel(t *testing.T) {
	nominateBoth := func(t *testing.T) (types.MsgServer, keeper.Keeper, sdk.Context, string) {
		t.Helper()
		ms, k, ctx, primary, emergency, _ := pendingSetup(t)
		_, err := ms.NominateAuthority(ctx, &types.MsgNominateAuthority{
			Authority: primary, Role: primaryRole, Nominee: addr(0x21),
		})
		require.NoError(t, err)
		_, err = ms.NominateAuthority(ctx, &types.MsgNominateAuthority{
			Authority: emergency, Role: emergencyRole, Nominee: addr(0x22),
		})
		require.NoError(t, err)
		require.Len(t, pendingTransfers(t, k, ctx), 2)
		return ms, k, ctx, primary
	}

	t.Run("accept clears only the accepted role", func(t *testing.T) {
		ms, k, ctx, _ := nominateBoth(t)
		_, err := ms.AcceptAuthority(ctx, &types.MsgAcceptAuthority{Nominee: addr(0x21), Role: primaryRole})
		require.NoError(t, err)

		transfers := pendingTransfers(t, k, ctx)
		require.Equal(t, []*types.PendingAuthorityTransferEntry{pendingEntry(emergencyRole, addr(0x22), 1)}, transfers)
		requireGaugeAgrees(t, k, ctx, transfers)
	})

	t.Run("cancel clears only the canceled role", func(t *testing.T) {
		ms, k, ctx, primary := nominateBoth(t)
		_, err := ms.CancelAuthorityNomination(ctx, &types.MsgCancelAuthorityNomination{
			Authority: primary, Role: primaryRole,
		})
		require.NoError(t, err)

		transfers := pendingTransfers(t, k, ctx)
		require.Equal(t, []*types.PendingAuthorityTransferEntry{pendingEntry(emergencyRole, addr(0x22), 1)}, transfers)
		requireGaugeAgrees(t, k, ctx, transfers)
	})

	t.Run("accepting and canceling both leaves an empty answer", func(t *testing.T) {
		ms, k, ctx, primary := nominateBoth(t)
		_, err := ms.AcceptAuthority(ctx, &types.MsgAcceptAuthority{Nominee: addr(0x22), Role: emergencyRole})
		require.NoError(t, err)
		_, err = ms.CancelAuthorityNomination(ctx, &types.MsgCancelAuthorityNomination{
			Authority: primary, Role: primaryRole,
		})
		require.NoError(t, err)

		transfers := pendingTransfers(t, k, ctx)
		require.Empty(t, transfers)
		requireGaugeAgrees(t, k, ctx, transfers)
	})
}

// The query is read-only. Every byte of the module store is compared before and
// after, with a nomination for each role in place, so a write anywhere — not only
// to the pending collection — fails this.
func TestPendingAuthorityTransfersQueryWritesNothing(t *testing.T) {
	ms, k, ctx, primary, emergency, storeKey := pendingSetup(t)
	for role, incumbent := range map[types.AuthorityRole]string{primaryRole: primary, emergencyRole: emergency} {
		_, err := ms.NominateAuthority(ctx, &types.MsgNominateAuthority{
			Authority: incumbent, Role: role, Nominee: addr(0x21),
		})
		require.NoError(t, err)
	}

	dump := func() [][2][]byte {
		iter := ctx.KVStore(storeKey).Iterator(nil, nil)
		defer iter.Close()
		var out [][2][]byte
		for ; iter.Valid(); iter.Next() {
			out = append(out, [2][]byte{bytes.Clone(iter.Key()), bytes.Clone(iter.Value())})
		}
		return out
	}
	before := dump()
	require.Len(t, pendingTransfers(t, k, ctx), 2)
	require.Equal(t, before, dump(), "the query changed the module store")
}

// A record that cannot be read must never be reported as "nothing pending". That
// answer is the one an operator checking for a hijacked handover would act on.
func TestPendingAuthorityTransfersQueryCorruptionIsInternal(t *testing.T) {
	ms, k, ctx, primary, _, storeKey := pendingSetup(t)
	_, err := ms.NominateAuthority(ctx, &types.MsgNominateAuthority{
		Authority: primary, Role: primaryRole, Nominee: addr(0x21),
	})
	require.NoError(t, err)
	// The same request answers normally before the bytes are damaged, so the
	// Internal below is attributable to the corruption.
	require.Len(t, pendingTransfers(t, k, ctx), 1)

	corruptOnlyRawValue(t, ctx, storeKey, types.PendingAuthorityPrefix)

	resp, err := policyQueryClient(t, k, ctx).PendingAuthorityTransfers(context.Background(),
		&types.QueryPendingAuthorityTransfersRequest{})
	require.Error(t, err)
	require.Nil(t, resp, "an error answer must carry no body, least of all an empty list")
	require.Equal(t, codes.Internal, grpcstatus.Code(err))
}

// A key outside the two roles is state no write path produces: the message server
// and genesis import both refuse any other role. It is refused rather than
// rendered under a role name the caller cannot interpret, and it is not skipped,
// because skipping it would report a damaged record as a clean answer.
func TestPendingAuthorityTransfersQueryRefusesAStrayRole(t *testing.T) {
	for name, key := range map[string]int32{
		"unspecified":  int32(types.AuthorityRole_AUTHORITY_ROLE_UNSPECIFIED),
		"undefined 99": 99,
	} {
		t.Run(name, func(t *testing.T) {
			_, k, ctx, _, _, _ := pendingSetup(t)
			require.NoError(t, k.PendingAuthority.Set(ctx, key,
				types.PendingAuthorityTransfer{Nominee: addr(0x21), NominatedHeight: 1}))

			resp, err := policyQueryClient(t, k, ctx).PendingAuthorityTransfers(context.Background(),
				&types.QueryPendingAuthorityTransfersRequest{})
			require.Error(t, err)
			require.Nil(t, resp)
			require.Equal(t, codes.Internal, grpcstatus.Code(err))
		})
	}
}
