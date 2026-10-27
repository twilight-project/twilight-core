package keeper_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/twilight-project/twilight-core/x/rewards/keeper"
	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// The ratified version relation is next = previous + 1 under CHECKED addition.
// The write boundary and the leading edge of a history page both implement it
// that way; the in-page relation used ordinary `+1` (#84). With a MaxUint64
// predecessor the sum wrapped to 0: a well-formed follower was then reported as
// "does not immediately follow" (true, but not what the ratified relation says —
// the predecessor has no successor at all), and only a row claiming version 0,
// which the record check refuses first, could ever have matched. A predecessor
// with no representable successor is corruption in its own right, and the page
// must say so, at the in-page edge as it already did at the leading edge.
//
// Mutation-sensitive: restoring `previous.Version+1` makes the second case below
// report "does not immediately follow" (0 != 7) rather than the overflow, and the
// assertion on the message fails. The third case pins the leading edge, whose
// checked add nothing else exercised: reverting it to `+1` there leaves the
// whole suite green without this test.
func TestRewardConfigHistoryPageUsesCheckedSuccessorArithmetic(t *testing.T) {
	// Beyond the fixture's anchor: a predecessor one below the top of the version
	// space and the top itself. The page is opened at the top version so that its
	// leading edge (MaxUint64-1 -> MaxUint64) is a valid checked step; whatever
	// follows is decided by the in-page relation.
	seedTop := func(t *testing.T) (keeper.Keeper, sdk.Context) {
		k, ctx, _ := setupCountingPrefix(t, types.RewardConfigVersionsPrefix.Bytes())
		seedRewardVersion(t, k, ctx, rewardVersionAt(math.MaxUint64-1, 3, "12"))
		seedRewardVersion(t, k, ctx, rewardVersionAt(math.MaxUint64, 5, "13"))
		return k, ctx
	}
	cursor := sdk.Uint64ToBigEndian(5)

	t.Run("a history that ends on the top version is served", func(t *testing.T) {
		k, ctx := seedTop(t)
		resp, err := keeper.NewQueryServer(k).RewardConfigVersions(ctx, &types.QueryRewardConfigVersionsRequest{
			Pagination: &query.PageRequest{Key: cursor, Limit: 10},
		})
		require.NoError(t, err)
		require.Len(t, resp.Versions, 1)
		require.Equal(t, uint64(math.MaxUint64), resp.Versions[0].Version)
		require.Empty(t, resp.Pagination.NextKey)
	})

	t.Run("a row after the top version is corruption, not a successor", func(t *testing.T) {
		k, ctx := seedTop(t)
		// Well-formed on its own, so the record check passes and the successor
		// relation is what judges it. (A row claiming version 0 would be refused by
		// the record check first, in both the checked and the wrapping code.)
		seedRewardVersion(t, k, ctx, rewardVersionAt(7, 7, "14"))
		// On the key-cursor path the paginator validates the row after the page to
		// learn whether a next page exists, so even a page of one is judged on the
		// row that follows it: a corrupt history has no clean page boundary there.
		for _, limit := range []uint64{1, 10} {
			_, err := keeper.NewQueryServer(k).RewardConfigVersions(ctx, &types.QueryRewardConfigVersionsRequest{
				Pagination: &query.PageRequest{Key: cursor, Limit: limit},
			})
			require.Error(t, err)
			require.Equal(t, codes.Internal, status.Code(err), "canonical state corruption is Internal, not the caller's mistake")
			require.Contains(t, err.Error(), "no representable successor")
		}
	})

	t.Run("a page that OPENS after the top version is refused at its leading edge", func(t *testing.T) {
		// Same corruption, different edge: the first row's predecessor seek finds
		// the top version, whose checked successor does not exist. This is the
		// pre-existing checked add in validateAdjacentRewardConfigEdge; the two
		// sites must agree, and this pins the one the in-page case cannot reach.
		k, ctx := seedTop(t)
		seedRewardVersion(t, k, ctx, rewardVersionAt(7, 7, "14"))
		_, err := keeper.NewQueryServer(k).RewardConfigVersions(ctx, &types.QueryRewardConfigVersionsRequest{
			Pagination: &query.PageRequest{Key: sdk.Uint64ToBigEndian(7), Limit: 10},
		})
		require.Error(t, err)
		require.Equal(t, codes.Internal, status.Code(err))
		require.Contains(t, err.Error(), "no representable successor")
	})
}
