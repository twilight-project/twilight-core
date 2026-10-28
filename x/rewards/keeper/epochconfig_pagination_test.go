package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"

	"github.com/twilight-project/twilight-core/x/rewards/keeper"
	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// Bounded pagination for the epoch-configuration history (#85).
//
// EpochConfigVersions predates the bounded collection-query policy and passed
// the caller's PageRequest straight to the SDK paginator, so the three cases the
// policy exists to refuse were all available on this surface: a nil or zero
// limit silently counted the whole history, an offset walked it row by row, and
// a caller-supplied limit was never capped. The history grows with every accepted
// geometry change and is never pruned, so each of those costs grows with it.
//
// As in pagination_test.go, every case below measures rows the iterator
// actually advanced over, not rows returned: count_total and offset both walk
// the collection without reading a value, so a measurement of returned rows
// would report the two worst cases as free.

// seedEpochConfigVersions writes an epoch-configuration history of count
// entries, each well-formed under validateEpochConfigRecord so the page
// validation has nothing to object to. The fixture holds no epoch history of its
// own; row 1 is the anchor genesis writes (version 1, epoch 1, height 1, the
// minimum length), and the rest follow contiguously.
func seedEpochConfigVersions(t *testing.T, k keeper.Keeper, ctx sdk.Context, count int) {
	t.Helper()
	for epoch := uint64(1); epoch <= uint64(count); epoch++ {
		require.NoError(t, k.EpochConfigVersions.Set(ctx, epoch, types.EpochConfigVersion{
			Version:              epoch,
			EffectiveEpoch:       epoch,
			EffectiveStartHeight: (epoch-1)*minLength + 1,
			EpochLengthBlocks:    minLength,
		}))
	}
}

func TestEpochConfigHistoryIsBoundedByThePageMaximum(t *testing.T) {
	const total = 400

	t.Run("refused requests do not walk a single row", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			page *query.PageRequest
		}{
			{"offset", &query.PageRequest{Offset: 300, Limit: 10}},
			{"count_total", &query.PageRequest{CountTotal: true, Limit: 10}},
			{"reverse", &query.PageRequest{Reverse: true, Limit: 10}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
				seedEpochConfigVersions(t, k, ctx, total)
				*access = storeAccess{}

				_, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx,
					&types.QueryEpochConfigVersionsRequest{Pagination: tc.page})
				require.Equal(t, codes.InvalidArgument, status.Code(err))
				require.Zero(t, access.rows, "a refused request must not touch the collection")
				require.Zero(t, access.reads, "nor open an iterator over it")
			})
		}
	})

	for _, tc := range []struct {
		name string
		page *query.PageRequest
	}{
		{"nil pagination", nil},
		{"zero limit", &query.PageRequest{}},
		{"oversized limit", &query.PageRequest{Limit: 100_000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
			seedEpochConfigVersions(t, k, ctx, total)
			*access = storeAccess{}

			resp, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx,
				&types.QueryEpochConfigVersionsRequest{Pagination: tc.page})
			require.NoError(t, err)
			require.Len(t, resp.Versions, paginationPageMax)
			// Exactly the page. The paginator positions on the row after the page to
			// learn whether one exists and stops without advancing past it, and nothing
			// else reads this collection, so the count is the limit itself.
			require.Equal(t, paginationPageMax, access.rows,
				"one page must walk exactly its own rows; %d of %d rows were visited", access.rows, total)
			require.NotEmpty(t, resp.Pagination.NextKey)
			require.Zero(t, resp.Pagination.Total, "no total: counting the history is unbounded work")
			require.Equal(t, uint64(1), resp.Versions[0].EffectiveEpoch, "the page starts at the anchor")
		})
	}

	t.Run("a smaller limit is honored", func(t *testing.T) {
		k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
		seedEpochConfigVersions(t, k, ctx, total)
		*access = storeAccess{}

		resp, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx,
			&types.QueryEpochConfigVersionsRequest{Pagination: &query.PageRequest{Limit: 7}})
		require.NoError(t, err)
		require.Len(t, resp.Versions, 7)
		require.Equal(t, 7, access.rows)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("a nil request is the default page", func(t *testing.T) {
		k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
		seedEpochConfigVersions(t, k, ctx, total)
		*access = storeAccess{}

		resp, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx, nil)
		require.NoError(t, err)
		require.Len(t, resp.Versions, paginationPageMax)
		require.Equal(t, paginationPageMax, access.rows)
		require.Zero(t, resp.Pagination.Total)
	})

	t.Run("the cap holds on the cursor path too", func(t *testing.T) {
		k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
		seedEpochConfigVersions(t, k, ctx, total)
		*access = storeAccess{}

		resp, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx, &types.QueryEpochConfigVersionsRequest{
			Pagination: &query.PageRequest{Key: sdk.Uint64ToBigEndian(201), Limit: 100_000},
		})
		require.NoError(t, err)
		require.Len(t, resp.Versions, paginationPageMax)
		require.Equal(t, uint64(201), resp.Versions[0].EffectiveEpoch)
		require.Equal(t, paginationPageMax, access.rows)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("the key cursor preserves ascending order across pages", func(t *testing.T) {
		k, ctx, access := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
		seedEpochConfigVersions(t, k, ctx, 250)
		server := keeper.NewQueryServer(k)

		seen := make([]uint64, 0, 250)
		var key []byte
		for page := 0; page < 5; page++ {
			*access = storeAccess{}
			resp, err := server.EpochConfigVersions(ctx, &types.QueryEpochConfigVersionsRequest{
				Pagination: &query.PageRequest{Key: key, Limit: paginationPageMax},
			})
			require.NoError(t, err)
			require.LessOrEqual(t, access.rows, len(resp.Versions), "a cursor page walks only the rows it returns")
			for _, version := range resp.Versions {
				seen = append(seen, version.EffectiveEpoch)
			}
			key = resp.Pagination.NextKey
			if len(key) == 0 {
				break
			}
		}
		require.Len(t, seen, 250)
		for i, epoch := range seen {
			require.Equal(t, uint64(i+1), epoch)
		}
	})

	t.Run("the schedule window is independent of the history page", func(t *testing.T) {
		// The two halves of the response page separately. Bounding the history
		// must not disturb the schedule's own cursor, which was bounded already.
		k, ctx, _ := setupCountingPrefix(t, types.EpochConfigVersionsPrefix.Bytes())
		seedEpochConfigVersions(t, k, ctx, total)
		for epoch := uint64(total + 1); epoch <= uint64(total+5); epoch++ {
			require.NoError(t, k.ScheduledEpochConfigs.Set(ctx, epoch, types.ScheduledEpochConfig{
				EffectiveEpoch: epoch, EpochLengthBlocks: minLength,
			}))
		}
		resp, err := keeper.NewQueryServer(k).EpochConfigVersions(ctx,
			&types.QueryEpochConfigVersionsRequest{Pagination: &query.PageRequest{Limit: 3}, ScheduledLimit: 2})
		require.NoError(t, err)
		require.Len(t, resp.Versions, 3)
		require.NotEmpty(t, resp.Pagination.NextKey, "the history cursor is the history's own")
		require.Len(t, resp.Scheduled, 2)
		require.Equal(t, uint64(total+1), resp.Scheduled[0].EffectiveEpoch)
		require.Equal(t, uint64(total+3), resp.ScheduledNextEpoch)
	})
}
