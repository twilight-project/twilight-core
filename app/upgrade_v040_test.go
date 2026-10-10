package app_test

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"github.com/twilight-project/twilight-core/app"
)

// Every released boundary and the new one must be registered on the built
// application. v0.2.0 and v0.3.0 are asserted again so a refactor of
// registerUpgradeHandlers that drops entries cannot pass by keeping only the newest.
func TestEveryUpgradeBoundaryIsRegisteredOnTheBuiltApp(t *testing.T) {
	a := newAppOnDB(t, memDB())
	for _, name := range []string{"v0.2.0", "v0.3.0", "v0.4.0"} {
		require.Truef(t, a.UpgradeKeeper.HasHandler(name),
			"a node reaching the %s height without a registered handler halts and cannot resume", name)
	}
}

// The v0.4.0 entry's shape: a Migrate (the block gas ceiling) and no store change.
// The module version map is pinned by TestThisReleaseMovesNoModuleVersionAndMountsNoNewModule.
func TestTheV040EntrySetsBlockGasAndNothingElse(t *testing.T) {
	var found *app.Upgrade
	for i := range app.Upgrades {
		if app.Upgrades[i].Name == "v0.4.0" {
			found = &app.Upgrades[i]
		}
	}
	require.NotNil(t, found, "the v0.4.0 boundary must exist in the registry")
	require.Nil(t, found.StoreUpgrades, "no store is added, renamed or deleted")
	require.NotNil(t, found.Migrate, "v0.4.0 exists to set block.max_gas")
	require.Equal(t, int64(30_000_000), app.V040BlockMaxGas, "the ratified ceiling")
	require.NoError(t, app.ValidateUpgrades(app.Upgrades))
}

// The released v0.4.0 entry, run through the real upgrade path on a chain shaped
// like every network so far: CometBFT's default consensus params, so max_gas -1.
// A binary without v0.4.0 schedules it and halts; the binary with the real registry
// executes it. max_gas becomes the ceiling, max_bytes and every other section stay
// as they were, CometBFT is handed the result, and the module version map does not
// move.
func TestTheV040UpgradeEndsUnlimitedBlockGas(t *testing.T) {
	released := app.Upgrades // captured before chainHaltedAtUpgrade swaps the registry
	const height = int64(5)
	db, home, genesisParams := chainHaltedAtUpgrade(t, "v0.4.0", height, 0)
	require.Equal(t, int64(-1), genesisParams.Block.MaxGas, "the chain must start with unlimited block gas")

	withUpgrades(t, released)
	b := newAppWithHome(t, db, home)
	before, err := b.ConsensusKeeper.ParamsStore.Get(b.NewUncachedContext(false, cmtproto.Header{Height: height - 1}))
	require.NoError(t, err)
	versionsBefore, err := b.UpgradeKeeper.GetModuleVersionMap(b.NewUncachedContext(false, cmtproto.Header{Height: height - 1}))
	require.NoError(t, err)

	res, err := b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height})
	require.NoError(t, err, "the binary carrying v0.4.0 must execute it")
	_, err = b.Commit()
	require.NoError(t, err)

	ctx := b.NewUncachedContext(false, cmtproto.Header{Height: height})
	after, err := b.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, app.V040BlockMaxGas, after.Block.MaxGas)
	require.Equal(t, before.Block.MaxBytes, after.Block.MaxBytes, "max_bytes must be written back as stored")
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

	// What CometBFT adopts for the next height, with its own updateState checks.
	current := cmttypes.ConsensusParamsFromProto(genesisParams)
	next := current.Update(res.ConsensusParamUpdates)
	require.NoError(t, next.ValidateBasic())
	require.NoError(t, current.ValidateUpdate(res.ConsensusParamUpdates, height))
	require.Equal(t, app.V040BlockMaxGas, next.Block.MaxGas)
	require.Equal(t, current.Block.MaxBytes, next.Block.MaxBytes)

	versionsAfter, err := b.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, versionsBefore, versionsAfter, "v0.4.0 moves no module version")
	_, err = b.UpgradeKeeper.GetUpgradePlan(ctx)
	require.Error(t, err, "the applied plan must be cleared")

	// The chain continues under the new ceiling.
	res, err = b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height + 1})
	require.NoError(t, err)
	require.Equal(t, app.V040BlockMaxGas, res.ConsensusParamUpdates.Block.MaxGas)
	_, err = b.Commit()
	require.NoError(t, err)
}

// SetBlockMaxGas keeps a max_bytes that is not the default.
func TestSetBlockMaxGasKeepsTheStoredMaxBytes(t *testing.T) {
	withUpgrades(t, nil)
	a := newAppOnDB(t, memDB())
	initUpgradeGenesis(t, a)
	ctx := a.NewUncachedContext(false, cmtproto.Header{Height: 2})
	require.NoError(t, app.SetBlockParams(ctx, a.ConsensusKeeper, 2_000_000, -1))

	require.NoError(t, app.SetBlockMaxGas(ctx, a.ConsensusKeeper, 30_000_000))
	got, err := a.ConsensusKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2_000_000), got.Block.MaxBytes)
	require.Equal(t, int64(30_000_000), got.Block.MaxGas)
}
