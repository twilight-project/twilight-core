package cmd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/app"
	"github.com/twilight-project/twilight-core/cmd/twilightd/cmd"
	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

// These tests drive `tx coreslot update-metadata` through the REAL root command
// — NewRootCmd, executed the way main() executes it — against a minimal
// CometBFT JSON-RPC endpoint that answers only the CoreSlot query.
//
// The command's own tests in x/coreslot/client/cli run the subcommand directly,
// and that is exactly what let the defect these guard against through: the
// root's PersistentPreRunE runs the SDK's InterceptConfigsPreRunHandler, whose
// bindFlags applies every viper value to the same-named flag the operator did
// not set — via pflag Set, which marks the flag Changed. `moniker` is a
// top-level key in every config.toml, and <BINARY>_<FLAG> environment variables
// reach all five metadata flags the same way. So on a real node, "Changed" no
// longer meant "typed", and every call without --moniker replaced the on-chain
// moniker with the local node's. Only the assembled root reproduces that.

// coreSlotNode serves the one RPC the command needs, over HTTP JSON-RPC, so the
// client context built by the root from --node is the real one.
func coreSlotNode(t *testing.T, slot types.CoreSlot) *httptest.Server {
	t.Helper()
	enc := app.MakeEncodingConfig()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Path string `json:"path"`
			} `json:"params"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res := coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Code: 1, Log: "unexpected " + req.Method + " " + req.Params.Path}}
		if req.Method == "abci_query" && req.Params.Path == "/twilight.coreslot.v1.Query/CoreSlot" {
			bz, err := enc.Codec.Marshal(&types.QueryCoreSlotResponse{Slot: &slot})
			require.NoError(t, err)
			res = coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: bz, Height: 10}}
		}
		out, err := cmtjson.Marshal(res)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":` + string(out) + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// homeWithMoniker writes a node home whose config.toml carries the given
// moniker, exactly as `twilightd init <moniker>` (or the SDK's auto-created
// config on first CLI use, with the hostname) leaves one.
func homeWithMoniker(t *testing.T, moniker string) string {
	t.Helper()
	home := t.TempDir()
	cfg := cmtcfg.DefaultConfig()
	cfg.Moniker = moniker
	cmtcfg.EnsureRoot(home)
	cmtcfg.WriteConfigFile(filepath.Join(home, "config", "config.toml"), cfg)
	return home
}

// runRoot executes the assembled root exactly as main() does, capturing process
// stdout (the client context prints the transaction there) and cobra's stderr.
func runRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.NewRootCmd()
	var errBuf bytes.Buffer
	root.SetErr(&errBuf)
	root.SetArgs(args)

	r, w, perr := os.Pipe()
	require.NoError(t, perr)
	orig := os.Stdout
	os.Stdout = w
	err = svrcmd.Execute(root, "TWILIGHT", app.DefaultNodeHome)
	os.Stdout = orig
	require.NoError(t, w.Close())
	out, rerr := io.ReadAll(r)
	require.NoError(t, rerr)
	return string(out), errBuf.String(), err
}

func metadataFromDocument(t *testing.T, stdout string) types.OperatorMetadata {
	t.Helper()
	var doc struct {
		Body struct {
			Messages []struct {
				Metadata types.OperatorMetadata `json:"metadata"`
			} `json:"messages"`
		} `json:"body"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "stdout must be the bare tx JSON, got: %q", stdout)
	require.Len(t, doc.Body.Messages, 1)
	return doc.Body.Messages[0].Metadata
}

func updateMetadataArgs(operator, home, node string, extra ...string) []string {
	return append(append([]string{"tx", "coreslot", "update-metadata", "3"}, extra...),
		"--from", operator, "--chain-id", "test", "--generate-only",
		"--home", home, "--node", node, "--keyring-backend", "test")
}

func testOperator() string {
	return sdk.AccAddress(append([]byte{3}, make([]byte, 19)...)).String()
}

// A home with a config.toml — every home — must not turn its node moniker into
// the slot's. Before the fix this produced "local-node-config-moniker" here.
func TestUpdateMetadataThroughRootKeepsOnChainMonikerDespiteConfigToml(t *testing.T) {
	operator := testOperator()
	onChain := &types.OperatorMetadata{
		Moniker: "chain-moniker", Identity: "id", Website: "https://old.example",
		SecurityContact: "sec@example", Details: "d",
	}
	node := coreSlotNode(t, types.CoreSlot{SlotId: 3, OperatorAddress: operator, Metadata: onChain})
	home := homeWithMoniker(t, "local-node-config-moniker")

	stdout, stderr, err := runRoot(t, updateMetadataArgs(operator, home, node.URL, "--website", "https://new.example")...)
	require.NoError(t, err, "stderr:\n%s", stderr)

	got := metadataFromDocument(t, stdout)
	want := *onChain
	want.Website = "https://new.example"
	require.Equal(t, want, got,
		"--moniker was not typed, so the on-chain moniker must be kept, not replaced by config.toml's")
	require.Contains(t, stderr, `moniker           "chain-moniker"  (unchanged)`)
}

// The deprecated positional form must work on a home that already has a
// config.toml — which is every home after the first run. Before the fix it
// failed there as "moniker given both as a positional argument and as
// --moniker", because config.toml had supplied the flag.
func TestUpdateMetadataThroughRootPositionalMonikerWorksWithConfigToml(t *testing.T) {
	operator := testOperator()
	node := coreSlotNode(t, types.CoreSlot{SlotId: 3, OperatorAddress: operator,
		Metadata: &types.OperatorMetadata{Moniker: "old", Website: "https://keep.example"}})
	home := homeWithMoniker(t, "local-node-config-moniker")

	stdout, stderr, err := runRoot(t, updateMetadataArgs(operator, home, node.URL, "renamed")...)
	require.NoError(t, err, "the deprecated positional form is documented as still working; stderr:\n%s", stderr)
	require.Equal(t, types.OperatorMetadata{Moniker: "renamed", Website: "https://keep.example"}, metadataFromDocument(t, stdout))
	require.Contains(t, stderr, "positional moniker is deprecated")
}

// An environment variable the SDK binds to a flag (<BINARY>_<FLAG>, where
// BINARY is the executable's basename) must not become a named field either.
// Before the fix TWILIGHTD_DETAILS=from-env silently set details.
func TestUpdateMetadataThroughRootIgnoresEnvironmentValues(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	// The SDK derives the prefix from os.Executable at run time, so under `go
	// test` it is the test binary's name, not "twilightd".
	t.Setenv(path.Base(exe)+"_DETAILS", "from-env")

	operator := testOperator()
	onChain := &types.OperatorMetadata{Moniker: "chain-moniker", Details: "on-chain details"}
	node := coreSlotNode(t, types.CoreSlot{SlotId: 3, OperatorAddress: operator, Metadata: onChain})
	home := homeWithMoniker(t, "local-node-config-moniker")

	stdout, stderr, err := runRoot(t, updateMetadataArgs(operator, home, node.URL, "--website", "https://new.example")...)
	require.NoError(t, err, "stderr:\n%s", stderr)
	got := metadataFromDocument(t, stdout)
	require.Equal(t, "on-chain details", got.Details, "an environment value is not a typed flag")
	require.Equal(t, "chain-moniker", got.Moniker)
	require.Equal(t, "https://new.example", got.Website)
}

// The value an operator DOES type still wins through the same path, so the
// hook restores only what was not typed.
func TestUpdateMetadataThroughRootTypedMonikerStillApplies(t *testing.T) {
	operator := testOperator()
	node := coreSlotNode(t, types.CoreSlot{SlotId: 3, OperatorAddress: operator,
		Metadata: &types.OperatorMetadata{Moniker: "chain-moniker", Details: "d"}})
	home := homeWithMoniker(t, "local-node-config-moniker")

	stdout, stderr, err := runRoot(t, updateMetadataArgs(operator, home, node.URL, "--moniker", "typed")...)
	require.NoError(t, err, "stderr:\n%s", stderr)
	require.Equal(t, types.OperatorMetadata{Moniker: "typed", Details: "d"}, metadataFromDocument(t, stdout))
}
