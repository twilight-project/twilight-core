package keeper

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/twilight-project/twilight-core/x/rewards/types"
)

// A canceled or timed-out query is the transport's doing. Both mappers must hand
// it back untouched (identity, not errors.Is: wrapping would keep the chain
// intact while attaching an Internal code, which is exactly the bug this guards),
// keep a classification another mapper already made, and classify everything
// else as Internal: a raw error, and a registered module error, whose own gRPC
// code is the SDK's default Unknown and says nothing.
func TestRewardsQueryMappersKeepTransportErrorsAndClassifyTheRest(t *testing.T) {
	for name, mapper := range map[string]func(error) error{
		"canonicalStateQueryError": func(err error) error { return canonicalStateQueryError("params", err) },
		"epochQueryError":          epochQueryError,
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, context.Canceled, mapper(context.Canceled))
			require.Equal(t, context.DeadlineExceeded, mapper(context.DeadlineExceeded))

			already := status.Error(codes.NotFound, "already classified")
			require.Equal(t, codes.NotFound, status.Code(mapper(already)))

			require.Equal(t, codes.Internal, status.Code(mapper(errors.New("value decode: unexpected EOF"))))
			require.Equal(t, codes.Internal, status.Code(mapper(types.ErrInvalidParams.Wrap("stored"))),
				"a registered module error carries Unknown by default, which is no classification")
		})
	}
}
