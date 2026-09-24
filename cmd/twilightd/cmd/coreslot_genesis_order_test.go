package cmd_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	sdked25519 "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestCoreSlotGenesisValidateInEitherAuthoringOrder is #195, driven through the
// real root command against a real home.
//
// `coreslot-genesis add` writes a top-level validators key. The SDK's AppGenesis
// has no such field, so `add-genesis-account` run AFTER it rewrites the document
// without the key, and `coreslot-genesis validate` then failed with
// "unexpected end of JSON input" on a genesis the chain starts from. Nothing in
// the tooling fixes the order, so both must produce a document that validates.
//
// This is also the whole of #177. That issue reported the same error as validate
// ignoring --home; the reproduction happened to use a genesis written in the
// second order, and --home was never the cause. This test uses --home throughout.
func TestCoreSlotGenesisValidateInEitherAuthoringOrder(t *testing.T) {
	account := func(marker byte) string {
		raw := make([]byte, 20)
		raw[0], raw[19] = marker, marker
		return sdk.AccAddress(raw).String()
	}
	authority, emergency := account(1), account(2)
	operator, payout, settlement := account(3), account(4), account(5)
	funded := account(6)
	consensusKey := base64.StdEncoding.EncodeToString(sdked25519.GenPrivKey().PubKey().Bytes())

	addAccount := func(t *testing.T, home string) {
		t.Helper()
		_, stderr, err := runRoot(t, "add-genesis-account", funded, "1000utwlt", "--home", home)
		require.NoError(t, err, "stderr:\n%s", stderr)
	}
	addSlot := func(t *testing.T, home string) {
		t.Helper()
		_, stderr, err := runRoot(t, "coreslot-genesis", "set-authorities", authority, emergency, "--home", home)
		require.NoError(t, err, "stderr:\n%s", stderr)
		_, stderr, err = runRoot(t, "coreslot-genesis", "add", operator, payout, settlement, consensusKey, "node0", "--home", home)
		require.NoError(t, err, "stderr:\n%s", stderr)
	}

	for _, tc := range []struct {
		name           string
		steps          []func(*testing.T, string)
		validatorsKept bool
	}{
		{"accounts first, then slots", []func(*testing.T, string){addAccount, addSlot}, true},
		{"slots first, then accounts (#195)", []func(*testing.T, string){addSlot, addAccount}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			_, stderr, err := runRoot(t, "init", "node0", "--chain-id", "twilight-order-1", "--home", home)
			require.NoError(t, err, "stderr:\n%s", stderr)
			for _, step := range tc.steps {
				step(t, home)
			}

			// Prove the case under test is the one reached: in the second order the
			// SDK rewrite really has dropped the key.
			bz, err := os.ReadFile(filepath.Join(home, "config", "genesis.json"))
			require.NoError(t, err)
			var doc map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(bz, &doc))
			_, present := doc["validators"]
			require.Equal(t, tc.validatorsKept, present, "top-level validators key presence")

			stdout, stderr, err := runRoot(t, "coreslot-genesis", "validate", "--home", home)
			require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			_, stderr, err = runRoot(t, "validate", filepath.Join(home, "config", "genesis.json"), "--home", home)
			require.NoError(t, err, "stderr:\n%s", stderr)
		})
	}
}
