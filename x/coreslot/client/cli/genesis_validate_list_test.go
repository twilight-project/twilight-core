package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	sdked25519 "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

// TestGenesisCLIValidateHoldsAStatedListToExactlyTheActiveSlots makes the #195
// claim true: a stated list must name each active slot's key exactly once, as
// an ed25519 key, with that slot's power. With two active slots A and B, each
// of these kept the count right and passed before — [A, A] hides B, [A, X]
// read the foreign key X's missing power as a match for "" — and B's bytes
// under a secp256k1 type were accepted because only the bytes were compared.
// Checked in both places a list can appear.
func TestGenesisCLIValidateHoldsAStatedListToExactlyTheActiveSlots(t *testing.T) {
	cdc := testCodec(t)
	authority, emergency, _, _, _ := testAddresses()
	addr := func(marker byte) string {
		raw := make([]byte, 20)
		raw[0], raw[19] = marker, marker
		return sdk.AccAddress(raw).String()
	}
	keyOf := func(marker byte) string {
		key := make([]byte, sdked25519.PubKeySize)
		key[0] = marker
		return base64.StdEncoding.EncodeToString(key)
	}
	keyA, keyB, keyX := keyOf(9), keyOf(10), keyOf(11)
	// entry builds one CometBFT validator; a power of "-" omits the field.
	entry := func(keyType, value, power, name string) string {
		powerField := ""
		if power != "-" {
			powerField = `,"power":"` + power + `"`
		}
		return `{"pub_key":{"type":"` + keyType + `","value":"` + value + `"}` + powerField + `,"name":"` + name + `"}`
	}
	const ed = "tendermint/PubKeyEd25519"
	good := "[" + entry(ed, keyA, "1", "a") + "," + entry(ed, keyB, "1", "b") + "]"

	for _, where := range []string{"validators", "consensus.validators"} {
		for _, tc := range []struct {
			name    string
			list    string
			wantErr string
		}{
			{"both active slots once each", good, ""},
			{"[A, A] hides B", "[" + entry(ed, keyA, "1", "a") + "," + entry(ed, keyA, "1", "a2") + "]",
				"repeats a key already listed"},
			{"[A, X] with X carrying no power", "[" + entry(ed, keyA, "1", "a") + "," + entry(ed, keyX, "-", "x") + "]",
				"belongs to no active core slot"},
			{"[A, X] with X carrying an empty power", "[" + entry(ed, keyA, "1", "a") + "," + entry(ed, keyX, "", "x") + "]",
				"belongs to no active core slot"},
			{"B's bytes under a secp256k1 type", "[" + entry(ed, keyA, "1", "a") + "," + entry("tendermint/PubKeySecp256k1", keyB, "1", "b") + "]",
				"has key type"},
		} {
			t.Run(where+"/"+tc.name, func(t *testing.T) {
				home := t.TempDir()
				configDir := filepath.Join(home, "config")
				require.NoError(t, os.MkdirAll(configDir, 0o755))
				writeTestGenesis(t, configDir, cdc, types.DefaultGenesis(authority, emergency), `"1"`)
				require.NoError(t, runCLI(t, addGenesisSlotCmd(), home, cdc, addr(3), addr(4), addr(5), keyA, "a"))
				require.NoError(t, runCLI(t, addGenesisSlotCmd(), home, cdc, addr(6), addr(7), addr(8), keyB, "b"))

				doc, _ := readTestGenesis(t, configDir)
				if where == "validators" {
					doc["validators"] = json.RawMessage(tc.list)
				} else {
					delete(doc, "validators")
					doc["consensus"] = json.RawMessage(`{"validators":` + tc.list + `}`)
				}
				bz, err := json.Marshal(doc)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(configDir, "genesis.json"), bz, 0o600))

				err = runCLI(t, validateGenesisCmd(), home, cdc)
				if tc.wantErr == "" {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				require.Contains(t, err.Error(), where+": ")
				require.Contains(t, err.Error(), tc.wantErr)
			})
		}
	}
}
