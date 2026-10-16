package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/x/coreslot/keeper"
	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

var errInjectedDelete = errors.New("injected delete failure")

// deleteFault makes Delete fail for keys under one prefix once armed. Every other
// operation goes to the real store, so the path under test runs unmodified up to
// the removal being checked.
type deleteFault struct {
	inner  corestore.KVStoreService
	prefix []byte
	armed  bool
}

func (f *deleteFault) OpenKVStore(ctx context.Context) corestore.KVStore {
	return faultingStore{KVStore: f.inner.OpenKVStore(ctx), fault: f}
}

type faultingStore struct {
	corestore.KVStore
	fault *deleteFault
}

func (s faultingStore) Delete(key []byte) error {
	if s.fault.armed && bytes.HasPrefix(key, s.fault.prefix) {
		return errInjectedDelete
	}
	return s.KVStore.Delete(key)
}

func setupWithDeleteFault(t *testing.T, prefix []byte) (keeper.Keeper, sdk.Context, string, string, *deleteFault) {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	keys := storetypes.NewKVStoreKeys(types.StoreKey)
	cms := integration.CreateMultiStore(keys, log.NewNopLogger())
	ctx := sdk.NewContext(cms, cmtproto.Header{Height: 1}, false, log.NewNopLogger())
	fault := &deleteFault{inner: runtime.NewKVStoreService(keys[types.StoreKey]), prefix: prefix}
	k := keeper.NewKeeper(cdc, fault, testEconomicAddresses(t), nil)
	authority := sdk.AccAddress(make([]byte, 20)).String()
	emergency := sdk.AccAddress(append([]byte{1}, make([]byte, 19)...)).String()
	return k, ctx, authority, emergency, fault
}

// The secondary indexes are derived from the slot records, and a failed removal
// leaves an entry pointing at a slot that no longer holds that operator or key.
// So each removal's error is returned like its neighbours': a message fails and
// rolls back, and the block path halts rather than committing the divergence.
// These used to be discarded (#120).

func TestRemoveCoreSlotReturnsIndexRemovalErrors(t *testing.T) {
	for name, prefix := range map[string][]byte{
		"by operator":  types.OperatorPrefix,
		"by consensus": types.ConsensusPrefix,
	} {
		t.Run(name, func(t *testing.T) {
			k, ctx, authority, emergency, fault := setupWithDeleteFault(t, prefix)
			twoActiveGenesis(t, k, ctx, authority, emergency)
			msgs := keeper.NewMsgServer(k)
			_, err := msgs.InactivateCoreSlot(ctx, &types.MsgInactivateCoreSlot{AuthorityOrOperator: authority, SlotId: 1, Reason: "x"})
			require.NoError(t, err)

			fault.armed = true
			_, err = msgs.RemoveCoreSlot(ctx, &types.MsgRemoveCoreSlot{Authority: authority, SlotId: 1, Reason: "x"})
			require.ErrorIs(t, err, errInjectedDelete)
		})
	}
}

func TestImmediateKeyRotationReturnsIndexRemovalError(t *testing.T) {
	k, ctx, authority, emergency, fault := setupWithDeleteFault(t, types.ConsensusPrefix)
	twoActiveGenesis(t, k, ctx, authority, emergency)
	msgs := keeper.NewMsgServer(k)
	// A slot that is not ACTIVE rotates immediately rather than at a later height.
	_, err := msgs.InactivateCoreSlot(ctx, &types.MsgInactivateCoreSlot{AuthorityOrOperator: authority, SlotId: 1, Reason: "x"})
	require.NoError(t, err)

	fault.armed = true
	_, err = msgs.RotateConsensusKey(ctx, &types.MsgRotateConsensusKey{Authority: authority, SlotId: 1, NewConsensusPubkey: pubkey(t, 9)})
	require.ErrorIs(t, err, errInjectedDelete)
}

func TestDueKeyRotationInEndBlockReturnsIndexRemovalError(t *testing.T) {
	k, ctx, authority, emergency, fault := setupWithDeleteFault(t, types.ConsensusPrefix)
	twoActiveGenesis(t, k, ctx, authority, emergency)
	msgs := keeper.NewMsgServer(k)
	_, err := msgs.RotateConsensusKey(ctx, &types.MsgRotateConsensusKey{Authority: authority, SlotId: 1, NewConsensusPubkey: pubkey(t, 9)})
	require.NoError(t, err)

	fault.armed = true
	_, err = k.EndBlock(ctx.WithBlockHeight(2))
	require.ErrorIs(t, err, errInjectedDelete, "a due rotation whose old index entry cannot be removed must halt the block")
}
