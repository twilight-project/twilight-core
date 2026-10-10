package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	coreheader "cosmossdk.io/core/header"
	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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

// chainHaltedAtUpgrade starts a chain from the consensus params CometBFT actually
// sends at InitChain for a `twilightd init` network — CometBFT's defaults, so
// max_gas -1 and the version and ABCI sections present — schedules the upgrade
// through CoreSlot from a binary that cannot run it, and rides to the halt at its
// height. The sims defaults the other tests use already carry a finite max_gas and
// no version or ABCI section, so they never exercise the path v0.4.0 will take.
//
// tweaks adjust the genesis params before they are sent, for a chain whose stored
// values differ from the defaults.
func chainHaltedAtUpgrade(t *testing.T, name string, height int64, versionApp uint64, tweaks ...func(*cmttypes.ConsensusParams)) (dbm.DB, string, cmtproto.ConsensusParams) {
	t.Helper()
	db := dbm.NewMemDB()
	home := t.TempDir()
	withUpgrades(t, nil)
	a := newAppWithHome(t, db, home)
	defaults := cmttypes.DefaultConsensusParams()
	defaults.Version.App = versionApp
	for _, tweak := range tweaks {
		tweak(defaults)
	}
	genesisParams := defaults.ToProto()
	appState, err := json.Marshal(upgradeGenesisMap(t, a))
	require.NoError(t, err)
	_, err = a.InitChain(&abci.RequestInitChain{InitialHeight: 1, ConsensusParams: &genesisParams, AppStateBytes: appState})
	require.NoError(t, err)
	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)

	require.NoError(t, finalize(a, 2))
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 2}).WithHeaderInfo(coreheader.Info{Height: 2})
	params, err := a.CoreSlotKeeper.Params.Get(ctx)
	require.NoError(t, err)
	_, err = coreslotkeeper.NewMsgServer(a.CoreSlotKeeper).ScheduleUpgrade(ctx,
		&coreslottypes.MsgScheduleUpgrade{Authority: params.Authority, Name: name, Height: height})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	for h := int64(3); h < height; h++ {
		require.NoError(t, finalize(a, h))
		_, err = a.Commit()
		require.NoError(t, err)
	}
	require.Error(t, finalize(a, height), "the old binary must halt at the upgrade height")
	return db, home, genesisParams
}

// The v0.4.0 path: max_gas goes from -1 (unlimited) to a finite value on a chain
// whose stored params have CometBFT's shape. Every other section stays
// byte-identical, and the response passes the same checks CometBFT's updateState
// applies before it adopts the params for the next height.
func TestUpgradeTakesMaxGasFromUnlimitedToFinite(t *testing.T) {
	const name, height = "probe-max-gas", int64(5)
	const newMaxBytes, newMaxGas = int64(cmttypes.MaxBlockSizeBytes / 5), int64(75_000_000)
	db, home, genesisParams := chainHaltedAtUpgrade(t, name, height, 0)

	withUpgrades(t, []app.Upgrade{{Name: name, Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
		return app.SetBlockParams(ctx, k.Consensus, newMaxBytes, newMaxGas)
	}}})
	b := newAppWithHome(t, db, home)
	before, err := b.ConsensusKeeper.ParamsStore.Get(b.NewUncachedContext(false, cmtproto.Header{Height: height - 1}))
	require.NoError(t, err)
	require.Equal(t, int64(-1), before.Block.MaxGas, "the chain must start with unlimited block gas")

	res, err := b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	require.NoError(t, err)
	_, err = b.Commit()
	require.NoError(t, err)
	after, err := b.ConsensusKeeper.ParamsStore.Get(b.NewUncachedContext(false, cmtproto.Header{Height: height}))
	require.NoError(t, err)

	for name, pair := range map[string][2]interface{ Marshal() ([]byte, error) }{
		"evidence":  {before.Evidence, after.Evidence},
		"validator": {before.Validator, after.Validator},
		"version":   {before.Version, after.Version},
		"abci":      {before.Abci, after.Abci},
	} {
		was, err := pair[0].Marshal()
		require.NoError(t, err)
		is, err := pair[1].Marshal()
		require.NoError(t, err)
		require.Equalf(t, was, is, "the %s section must be byte-identical", name)
	}

	// CometBFT v0.38 state/execution.go updateState, on the actual response.
	current := cmttypes.ConsensusParamsFromProto(genesisParams)
	next := current.Update(res.ConsensusParamUpdates)
	require.NoError(t, next.ValidateBasic())
	require.NoError(t, current.ValidateUpdate(res.ConsensusParamUpdates, height))
	require.Equal(t, current.Version.App, next.Version.App, "CometBFT copies this into later block headers")
	require.Equal(t, newMaxGas, next.Block.MaxGas)
	require.Equal(t, newMaxBytes, next.Block.MaxBytes)
	require.Equal(t, current.Evidence, next.Evidence)
	require.Equal(t, current.Validator, next.Validator)
	require.Equal(t, current.ABCI, next.ABCI)
}

// A stored app version that is not zero reaches CometBFT unchanged through the
// upgrade block's response.
func TestUpgradeResponseKeepsANonZeroAppVersion(t *testing.T) {
	const name, height = "probe-version", int64(5)
	db, home, genesisParams := chainHaltedAtUpgrade(t, name, height, 9)
	withUpgrades(t, []app.Upgrade{{Name: name, Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
		return app.SetBlockParams(ctx, k.Consensus, 2_000_000, 75_000_000)
	}}})
	b := newAppWithHome(t, db, home)
	res, err := b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	require.NoError(t, err)
	require.Equal(t, uint64(9), res.ConsensusParamUpdates.Version.App)
	current := cmttypes.ConsensusParamsFromProto(genesisParams)
	require.Equal(t, uint64(9), current.Update(res.ConsensusParamUpdates).Version.App)
}

// Fail closed: a handler that sets block params and then fails commits nothing, and
// an invalid value halts the upgrade block.
func TestUpgradeThatFailsAfterSettingBlockParamsCommitsNothing(t *testing.T) {
	const name, height = "probe-fail", int64(5)
	db, home, _ := chainHaltedAtUpgrade(t, name, height, 0)

	withUpgrades(t, []app.Upgrade{{Name: name, Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
		if err := app.SetBlockParams(ctx, k.Consensus, 2_000_000, 75_000_000); err != nil {
			return err
		}
		return errors.New("a later step fails")
	}}})
	b := newAppWithHome(t, db, home)
	res, err := b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	require.Error(t, err)
	require.Nil(t, res)

	// A restart, which is what follows a FinalizeBlock error, sees only committed state.
	c := newAppWithHome(t, db, home)
	require.Equal(t, height-1, c.LastBlockHeight())
	got, err := c.ConsensusKeeper.ParamsStore.Get(c.NewUncachedContext(false, cmtproto.Header{Height: height - 1}))
	require.NoError(t, err)
	require.Equal(t, int64(-1), got.Block.MaxGas, "the params write must not survive the failed block")

	// evidence.max_bytes is 1 MiB by default, and CometBFT requires block.max_bytes
	// to be at least that.
	withUpgrades(t, []app.Upgrade{{Name: name, Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
		return app.SetBlockParams(ctx, k.Consensus, 500_000, 75_000_000)
	}}})
	d := newAppWithHome(t, db, home)
	_, err = d.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	require.Error(t, err, "an invalid value must halt the upgrade block")
}

// gasLimitListener records each block's gas-meter limit as FinalizeBlock ends.
type gasLimitListener struct{ limits map[int64]uint64 }

func (l *gasLimitListener) ListenFinalizeBlock(ctx context.Context, req abci.RequestFinalizeBlock, _ abci.ResponseFinalizeBlock) error {
	l.limits[req.Height] = sdk.UnwrapSDKContext(ctx).BlockGasMeter().Limit()
	return nil
}

func (l *gasLimitListener) ListenCommit(context.Context, abci.ResponseCommit, []*storetypes.StoreKVPair) error {
	return nil
}

// Pins when a new max_gas binds. CometBFT built block H under the old params, but
// the application enforces the new max_gas on block H's own transactions: x/upgrade
// reports ConsensusParamsChanged after the handler, and BaseApp then rebuilds the
// block gas meter and re-reads the per-transaction limit from the store. max_bytes,
// which only CometBFT enforces, binds from H+1. Operators need the first fact: a
// transaction already in the upgrade block that exceeds the new max_gas fails there.
func TestNewMaxGasBindsInTheUpgradeBlockItself(t *testing.T) {
	const upgradeName = "probe-block-params"
	const upgradeHeight = int64(5)
	const maxBytes, newMaxGas = int64(1_048_576), int64(30_000_000)
	const txGas = uint64(50_000_000) // between the new limit and the old one (sims genesis: 100M)

	encoding := app.MakeEncodingConfig()
	heavyTx := func() []byte {
		builder := encoding.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(&banktypes.MsgSend{
			FromAddress: addrOf(0xD1).String(), ToAddress: addrOf(0xD2).String(), Amount: coins(minimum()),
		}))
		builder.SetGasLimit(txGas)
		bz, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}

	db := dbm.NewMemDB()
	home := t.TempDir()
	withUpgrades(t, nil)
	binaryA := newAppWithHome(t, db, home)
	initUpgradeGenesis(t, binaryA)
	listenerA := &gasLimitListener{limits: map[int64]uint64{}}
	binaryA.SetStreamingManager(storetypes.StreamingManager{ABCIListeners: []storetypes.ABCIListener{listenerA}})

	require.NoError(t, finalize(binaryA, 2))
	ctx := binaryA.NewUncachedContext(false, cmtproto.Header{Height: 2}).WithHeaderInfo(coreheader.Info{Height: 2})
	params, err := binaryA.CoreSlotKeeper.Params.Get(ctx)
	require.NoError(t, err)
	_, err = coreslotkeeper.NewMsgServer(binaryA.CoreSlotKeeper).ScheduleUpgrade(ctx,
		&coreslottypes.MsgScheduleUpgrade{Authority: params.Authority, Name: upgradeName, Height: upgradeHeight})
	require.NoError(t, err)
	_, err = binaryA.Commit()
	require.NoError(t, err)

	// Control: the same transaction in block H-1, under the old limit, is rejected
	// for a different reason (it is unsigned), not for its gas.
	var previous *abci.ResponseFinalizeBlock
	for height := int64(3); height < upgradeHeight; height++ {
		req := &abci.RequestFinalizeBlock{Height: height}
		if height == upgradeHeight-1 {
			req.Txs = [][]byte{heavyTx()}
		}
		previous, err = binaryA.FinalizeBlock(req)
		require.NoError(t, err)
		_, err = binaryA.Commit()
		require.NoError(t, err)
	}
	require.NotContains(t, previous.TxResults[0].Log, "exceeds block max gas")
	require.Error(t, finalize(binaryA, upgradeHeight))

	withUpgrades(t, []app.Upgrade{{
		Name: upgradeName,
		Migrate: func(ctx sdk.Context, k app.MigrationKeepers) error {
			return app.SetBlockParams(ctx, k.Consensus, maxBytes, newMaxGas)
		},
	}})
	binaryB := newAppWithHome(t, db, home)
	listenerB := &gasLimitListener{limits: map[int64]uint64{}}
	binaryB.SetStreamingManager(storetypes.StreamingManager{ABCIListeners: []storetypes.ABCIListener{listenerB}})

	res, err := binaryB.FinalizeBlock(&abci.RequestFinalizeBlock{Height: upgradeHeight, Txs: [][]byte{heavyTx()}})
	require.NoError(t, err)
	require.Equal(t, uint64(newMaxGas), listenerB.limits[upgradeHeight],
		"the upgrade block's own gas meter carries the new max_gas")
	require.Contains(t, res.TxResults[0].Log, "exceeds block max gas 30000000")
}
