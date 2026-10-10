package app_test

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

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

// The released v0.4.0 entry, run through the real upgrade path, on two chain shapes:
// CometBFT's default consensus params (every network so far: max_gas -1, max_bytes
// 22020096, app version 0), and a chain whose max_bytes and app version are not the
// defaults. The second is what pins the entry's two promises — max_bytes written back
// as stored, and the version section untouched — which the defaults cannot show,
// because a handler that wrote the defaults back would pass on them.
//
// A binary without v0.4.0 schedules it and halts; the binary with the real registry
// executes it. max_gas becomes the ceiling, everything else stays as it was, CometBFT
// is handed the result, and the module version map does not move.
func TestTheV040UpgradeEndsUnlimitedBlockGas(t *testing.T) {
	for _, shape := range []struct {
		name       string
		versionApp uint64
		tweaks     []func(*cmttypes.ConsensusParams)
	}{
		{name: "CometBFT defaults", versionApp: 0},
		{name: "non-default max_bytes and app version", versionApp: 7, tweaks: []func(*cmttypes.ConsensusParams){
			func(p *cmttypes.ConsensusParams) { p.Block.MaxBytes = 2_000_000 },
		}},
	} {
		t.Run(shape.name, func(t *testing.T) {
			released := app.Upgrades // captured before chainHaltedAtUpgrade swaps the registry
			const height = int64(5)
			db, home, genesisParams := chainHaltedAtUpgrade(t, "v0.4.0", height, shape.versionApp, shape.tweaks...)
			require.Equal(t, int64(-1), genesisParams.Block.MaxGas, "the chain must start with unlimited block gas")

			withUpgrades(t, released)
			b := newAppWithHome(t, db, home)
			before, err := b.ConsensusKeeper.ParamsStore.Get(b.NewUncachedContext(false, cmtproto.Header{Height: height - 1}))
			require.NoError(t, err)
			require.Equal(t, genesisParams.Block.MaxBytes, before.Block.MaxBytes)
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
			require.Equal(t, shape.versionApp, res.ConsensusParamUpdates.Version.App,
				"CometBFT copies this into later block headers; it must not be reset")
			require.Equal(t, before.Block.MaxBytes, res.ConsensusParamUpdates.Block.MaxBytes)
			current := cmttypes.ConsensusParamsFromProto(genesisParams)
			next := current.Update(res.ConsensusParamUpdates)
			require.NoError(t, next.ValidateBasic())
			require.NoError(t, current.ValidateUpdate(res.ConsensusParamUpdates, height))
			require.Equal(t, app.V040BlockMaxGas, next.Block.MaxGas)
			require.Equal(t, current.Block.MaxBytes, next.Block.MaxBytes)
			require.Equal(t, current.Version.App, next.Version.App)

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
		})
	}
}

// The upgrade block is built by CometBFT under the old params (max_gas -1), but the
// application holds its transactions to the new ceiling from the handler on. So a
// block H carrying more declared gas than 30 M must still commit: the transactions
// that cross the ceiling fail deterministically, and the block is not rejected.
// Two independent runs must produce the same app hash and the same results.
func TestTheV040UpgradeBlockHoldsItsOwnTransactionsToTheNewCeiling(t *testing.T) {
	released := app.Upgrades
	const height = int64(5)

	encoding := app.MakeEncodingConfig()
	bigTx := func(marker byte) []byte {
		builder := encoding.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(&banktypes.MsgSend{
			FromAddress: addrOf(marker).String(), ToAddress: addrOf(0xD2).String(), Amount: coins(minimum()),
		}))
		builder.SetGasLimit(15_000_000)
		pk := secp256k1.GenPrivKeyFromSecret([]byte{marker}).PubKey()
		// A 1.2 MB dummy signature: tx-size gas burns ~12 M before the ante rejects
		// it, so four of these exceed the 30 M ceiling inside block H.
		require.NoError(t, builder.SetSignatures(signing.SignatureV2{
			PubKey:   pk,
			Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: make([]byte, 1_200_000)},
			Sequence: 0,
		}))
		bz, err := encoding.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	txs := [][]byte{bigTx(0xA1), bigTx(0xA2), bigTx(0xA3), bigTx(0xA4)}

	run := func() *abci.ResponseFinalizeBlock {
		db, home, _ := chainHaltedAtUpgrade(t, "v0.4.0", height, 0)
		withUpgrades(t, released)
		b := newAppWithHome(t, db, home)
		res, err := b.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Txs: txs})
		require.NoError(t, err, "the upgrade block must not be rejected")
		_, err = b.Commit()
		require.NoError(t, err)
		return res
	}
	first, second := run(), run()
	require.Equal(t, first.AppHash, second.AppHash, "the upgrade block must be deterministic")
	for i := range first.TxResults {
		require.Equal(t, first.TxResults[i].Code, second.TxResults[i].Code)
		require.Equal(t, first.TxResults[i].Log, second.TxResults[i].Log)
	}
	require.Contains(t, first.TxResults[2].Log, "block gas meter", "the transaction crossing 30 M runs out of block gas")
	require.Contains(t, first.TxResults[3].Log, "no block gas left", "and nothing after it runs")
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
