package app_test

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	coreheader "cosmossdk.io/core/header"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/app"
	coreslotkeeper "github.com/twilight-project/twilight-core/x/coreslot/keeper"
	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
)

// #170: block parameters are reachable on a running network through an upgrade
// handler, and through nothing else.
//
// The whole path runs on the real modules: a binary without the handler schedules
// the upgrade through CoreSlot and halts at the height, and the binary that carries
// it executes the handler, writes the new block parameters, and hands them to
// CometBFT in the same block's FinalizeBlock response. Reading the store back is not
// enough on its own — CometBFT only learns of a change from that response — so both
// are asserted.
func TestAnUpgradeHandlerSetsBlockParams(t *testing.T) {
	const upgradeName = "probe-block-params"
	const upgradeHeight = int64(5)
	const maxBytes, maxGas = int64(1_048_576), int64(30_000_000)

	db := dbm.NewMemDB()
	home := t.TempDir()

	// ---------- Binary A: does not know the upgrade ----------
	withUpgrades(t, nil)
	binaryA := newAppWithHome(t, db, home)
	initUpgradeGenesis(t, binaryA)

	before, err := binaryA.ConsensusKeeper.ParamsStore.Get(
		binaryA.NewUncachedContext(false, cmtproto.Header{Height: 1}))
	require.NoError(t, err)
	require.NotEqual(t, maxBytes, before.Block.MaxBytes, "the test must change max_bytes")
	require.NotEqual(t, maxGas, before.Block.MaxGas, "the test must change max_gas")

	require.NoError(t, finalize(binaryA, 2))
	ctx := binaryA.NewUncachedContext(false, cmtproto.Header{Height: 2}).
		WithHeaderInfo(coreheader.Info{Height: 2})
	params, err := binaryA.CoreSlotKeeper.Params.Get(ctx)
	require.NoError(t, err)
	_, err = coreslotkeeper.NewMsgServer(binaryA.CoreSlotKeeper).ScheduleUpgrade(ctx,
		&coreslottypes.MsgScheduleUpgrade{Authority: params.Authority, Name: upgradeName, Height: upgradeHeight})
	require.NoError(t, err)
	_, err = binaryA.Commit()
	require.NoError(t, err)
	for height := int64(3); height < upgradeHeight; height++ {
		require.NoError(t, finalize(binaryA, height))
		_, err = binaryA.Commit()
		require.NoError(t, err)
	}
	require.Error(t, finalize(binaryA, upgradeHeight), "the old binary must halt at the upgrade height")

	// ---------- Binary B: carries the handler ----------
	withUpgrades(t, []app.Upgrade{{
		Name: upgradeName,
		Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
			return app.SetBlockParams(ctx, k.Consensus, maxBytes, maxGas)
		},
	}})
	binaryB := newAppWithHome(t, db, home)

	res, err := binaryB.FinalizeBlock(&abci.RequestFinalizeBlock{Height: upgradeHeight})
	require.NoError(t, err, "the new binary must execute the upgrade")
	require.NotNil(t, res.ConsensusParamUpdates)
	require.Equal(t, maxBytes, res.ConsensusParamUpdates.Block.MaxBytes,
		"CometBFT must be handed the new max_bytes in the upgrade block's response")
	require.Equal(t, maxGas, res.ConsensusParamUpdates.Block.MaxGas,
		"CometBFT must be handed the new max_gas in the upgrade block's response")
	_, err = binaryB.Commit()
	require.NoError(t, err)

	after, err := binaryB.ConsensusKeeper.ParamsStore.Get(
		binaryB.NewUncachedContext(false, cmtproto.Header{Height: upgradeHeight}))
	require.NoError(t, err)
	require.Equal(t, maxBytes, after.Block.MaxBytes)
	require.Equal(t, maxGas, after.Block.MaxGas)
	require.Equal(t, before.Evidence, after.Evidence, "only the block section may change")
	require.Equal(t, before.Validator, after.Validator, "only the block section may change")
	// CometBFT's proto round trip writes an absent ABCI section back as an empty one
	// (upstream UpdateParams does the same); what it means must not change.
	require.Equal(t, before.Abci.GetVoteExtensionsEnableHeight(), after.Abci.GetVoteExtensionsEnableHeight(),
		"only the block section may change")

	// The next block still reports the new values: they are stored, not a one-block
	// response artifact.
	res, err = binaryB.FinalizeBlock(&abci.RequestFinalizeBlock{Height: upgradeHeight + 1})
	require.NoError(t, err)
	require.Equal(t, maxGas, res.ConsensusParamUpdates.Block.MaxGas)
	_, err = binaryB.Commit()
	require.NoError(t, err)
}

// An invalid value refuses the write and leaves the stored parameters as they were.
// From a handler, that error aborts the upgrade block: the node halts rather than run
// with parameters it was not asked for.
func TestSetBlockParamsRefusesInvalidValues(t *testing.T) {
	withUpgrades(t, nil)
	a := newAppOnDB(t, dbm.NewMemDB())
	initUpgradeGenesis(t, a)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 2})

	before, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)

	for name, tc := range map[string]struct{ maxBytes, maxGas int64 }{
		"max_gas below -1":                        {maxBytes: before.Block.MaxBytes, maxGas: -2},
		"max_bytes zero":                          {maxBytes: 0, maxGas: before.Block.MaxGas},
		"max_bytes below -1":                      {maxBytes: -2, maxGas: before.Block.MaxGas},
		"max_bytes above CometBFT's hard maximum": {maxBytes: 104_857_601, maxGas: before.Block.MaxGas},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, app.SetBlockParams(ctx, a.ConsensusKeeper, tc.maxBytes, tc.maxGas))
			got, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
			require.NoError(t, err)
			require.Equal(t, before, got, "a refused value must leave the stored params untouched")
		})
	}
}

// The reason SetBlockParams does not call the consensus keeper's UpdateParams: that
// path rebuilds the version section from CometBFT's defaults, and CometBFT copies the
// version it is handed into later block headers. A block-params change must leave
// the app version exactly as it was.
func TestSetBlockParamsPreservesTheVersionSection(t *testing.T) {
	withUpgrades(t, nil)
	a := newAppOnDB(t, dbm.NewMemDB())
	initUpgradeGenesis(t, a)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 2})

	stored, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	stored.Version = &cmtproto.VersionParams{App: 3}
	require.NoError(t, a.ConsensusKeeper.ParamsStore.Set(ctx, stored))

	require.NoError(t, app.SetBlockParams(ctx, a.ConsensusKeeper, 1_048_576, 30_000_000))

	got, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	require.NotNil(t, got.Version)
	require.Equal(t, uint64(3), got.Version.App, "the app version must survive a block-params change")
	require.Equal(t, int64(30_000_000), got.Block.MaxGas)
}
