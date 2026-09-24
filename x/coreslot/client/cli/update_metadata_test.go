package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"

	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

// slotNode is a CometRPC that answers exactly one query — CoreSlot, for one slot
// — over the ABCI path the client context takes when no gRPC endpoint is set.
//
// The command under test is not handed a query client. It builds its own from
// the client context, exactly as in production, and this is what that client
// reaches; so what is proven is that the shipped command reads the record and
// sends the merge, not that a merge helper works when called directly. Every
// other RPC method is the embedded nil interface: a command that strays onto
// one panics rather than passes.
type slotNode struct {
	client.CometRPC
	cdc     codec.Codec
	slot    types.CoreSlot
	queried []uint64
}

const coreSlotQueryPath = "/twilight.coreslot.v1.Query/CoreSlot"

func (n *slotNode) ABCIQueryWithOptions(_ context.Context, path string, data cmtbytes.HexBytes, _ rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	if path != coreSlotQueryPath {
		return nil, fmt.Errorf("unexpected query %q", path)
	}
	var req types.QueryCoreSlotRequest
	if err := n.cdc.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	n.queried = append(n.queried, req.SlotId)
	if req.SlotId != n.slot.SlotId {
		return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Code: 1, Log: "slot not found"}}, nil
	}
	bz, err := n.cdc.Marshal(&types.QueryCoreSlotResponse{Slot: &n.slot})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: bz}}, nil
}

// metadataRun is one execution of the real update-metadata command under
// --generate-only: the transaction it printed, the preview it wrote, and the
// node it read from.
type metadataRun struct {
	node     *slotNode
	operator string
	txCfg    client.TxConfig
	stdout   bytes.Buffer // process stdout: the unsigned transaction, and nothing else
	stderr   bytes.Buffer // process stderr: the preview and any notice
}

func fullMetadata() *types.OperatorMetadata {
	return &types.OperatorMetadata{
		Moniker: "slot-three", Identity: "keybase:abc", Website: "https://old.example",
		SecurityContact: "sec@example", Details: `{"version":1}`,
	}
}

// runUpdateMetadata executes updateMetadataCmd() — the registered command, with
// its own flags and context handling — against a node holding slot 3 with the
// given metadata. It returns the command's error so refusals can be asserted.
func runUpdateMetadata(t *testing.T, current *types.OperatorMetadata, args ...string) (*metadataRun, error) {
	t.Helper()
	return runUpdateMetadataAgainst(t, 3, current, args...)
}

// runUpdateMetadataAgainst is runUpdateMetadata with the node holding a chosen
// slot id, so a query for slot 3 can be made to miss.
func runUpdateMetadataAgainst(t *testing.T, nodeSlot uint64, current *types.OperatorMetadata, args ...string) (*metadataRun, error) {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	run := &metadataRun{
		operator: sdk.AccAddress(append([]byte{3}, make([]byte, 19)...)).String(),
		txCfg:    authtx.NewTxConfig(cdc, authtx.DefaultSignModes),
	}
	run.node = &slotNode{cdc: cdc, slot: types.CoreSlot{SlotId: nodeSlot, Metadata: current}}
	run.node.slot.OperatorAddress = run.operator

	clientCtx := client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(registry).
		WithTxConfig(run.txCfg).
		// Present so the --keyring-backend default does not open an OS keyring;
		// under --generate-only a bech32 --from never consults it.
		WithKeyring(keyring.NewInMemory(cdc)).
		WithClient(run.node).
		WithOutput(&run.stdout)

	cmd := updateMetadataCmd()
	cmd.SetContext(context.WithValue(context.Background(), client.ClientContextKey, &clientCtx))
	// Both of the command's stdout routes — cobra's and the client context's —
	// land in the same buffer, and stderr in another, so a preview or notice
	// that strayed onto stdout would corrupt the document asserted below.
	cmd.SetOut(&run.stdout)
	cmd.SetErr(&run.stderr)
	// Cobra's usage-on-error goes to the out writer when one is set (in
	// production none is, so it goes to stderr); silenced so that an error
	// path's stdout can be asserted empty without asserting on cobra's usage.
	cmd.SilenceUsage = true
	cmd.SetArgs(append([]string{"3", "--from", run.operator, "--chain-id", "test", "--generate-only"}, args...))
	return run, cmd.Execute()
}

// message decodes the transaction the command printed, through the tx codec,
// and returns its one message. Stdout must hold that document and nothing
// else: it is what `--generate-only ... | twilightd tx sign` consumes.
func (r *metadataRun) message(t *testing.T) *types.MsgUpdateOperatorMetadata {
	t.Helper()
	doc := bytes.TrimSpace(r.stdout.Bytes())
	require.True(t, json.Valid(doc), "stdout must be exactly one JSON document, got: %q", r.stdout.String())
	tx, err := r.txCfg.TxJSONDecoder()(doc)
	require.NoError(t, err, "stdout must be the unsigned transaction document")
	msgs := tx.GetMsgs()
	require.Len(t, msgs, 1)
	msg, ok := msgs[0].(*types.MsgUpdateOperatorMetadata)
	require.True(t, ok, "expected MsgUpdateOperatorMetadata, got %T", msgs[0])
	return msg
}

// One named field replaces that field and nothing else. This is the case from
// #181: before, this exact command would have sent a record with only the named
// field set and the chain would have stored it, clearing the other four.
func TestUpdateMetadataKeepsUnnamedFields(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata(), "--website", "https://new.example")
	require.NoError(t, err)

	msg := run.message(t)
	require.Equal(t, run.operator, msg.Operator)
	require.EqualValues(t, 3, msg.SlotId)
	want := fullMetadata()
	want.Website = "https://new.example"
	require.Equal(t, want, msg.Metadata, "the message must carry the merge, not just the named field")

	require.Equal(t, []uint64{3}, run.node.queried, "the current record must be read from the node, once, for this slot")

	// The preview shows the whole record, and says which field moved — on
	// stderr, never stdout.
	preview := run.stderr.String()
	require.NotContains(t, run.stdout.String(), "metadata as this transaction will store it")
	require.Contains(t, preview, `website           "https://new.example"  (set; was "https://old.example")`)
	require.Contains(t, preview, `moniker           "slot-three"  (unchanged)`)
	require.Contains(t, preview, `details           "{\"version\":1}"  (unchanged)`)
	require.NotContains(t, preview, "deprecated", "no notice when the positional form is not used")
}

// An explicit empty value clears that one field; the other four are untouched.
// pflag reports --website "" as changed, which is the only way "clear" can be
// told apart from "not mentioned".
func TestUpdateMetadataEmptyValueClearsOnlyThatField(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata(), "--website", "")
	require.NoError(t, err)

	want := fullMetadata()
	want.Website = ""
	require.Equal(t, want, run.message(t).Metadata)
	require.Contains(t, run.stderr.String(), `website           ""  (cleared; was "https://old.example")`)
}

// No field named is refused locally. Sending the read-back record unchanged
// would be a harmless no-op today, but it is still a full replace on the wire,
// and a command that does nothing should say so rather than cost a fee.
func TestUpdateMetadataRefusesNoFields(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no metadata field given")
	require.Contains(t, err.Error(), "--security-contact", "the error must name the flags that would fix it")
	require.Empty(t, run.node.queried, "refused before any node was reached")
	require.Empty(t, run.stdout.String(), "no transaction was generated")
}

// A value over the keeper's 512-byte limit is refused locally, before the node
// is read, through the keeper's own validator.
func TestUpdateMetadataRefusesOverlongValueLocally(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata(), "--details", strings.Repeat("x", 513))
	require.Error(t, err)
	require.Contains(t, err.Error(), "details exceeds 512 bytes")
	require.Empty(t, run.node.queried, "refused before any node was reached")
	require.Empty(t, run.stdout.String())

	// Exactly at the limit is accepted, so the check is the keeper's bound and
	// not an off-by-one of its own.
	run, err = runUpdateMetadata(t, fullMetadata(), "--details", strings.Repeat("x", 512))
	require.NoError(t, err)
	require.Len(t, run.message(t).Metadata.Details, 512)
}

// A slot registered with no metadata at all merges as all-empty, so the first
// update-metadata against it works rather than dereferencing nil.
func TestUpdateMetadataOnSlotWithoutMetadata(t *testing.T) {
	run, err := runUpdateMetadata(t, nil, "--moniker", "first")
	require.NoError(t, err)
	require.Equal(t, &types.OperatorMetadata{Moniker: "first"}, run.message(t).Metadata)
}

// The pre-#181 spelling still works and now means --moniker: an existing runbook
// keeps running, and stops being destructive. It is announced as deprecated, and
// may not be combined with --moniker.
func TestUpdateMetadataPositionalMonikerIsDeprecatedAlias(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata(), "renamed")
	require.NoError(t, err)
	want := fullMetadata()
	want.Moniker = "renamed"
	require.Equal(t, want, run.message(t).Metadata, "the positional form must keep the other four fields too")
	require.Contains(t, run.stderr.String(), "positional moniker is deprecated")
	require.NotContains(t, run.stdout.String(), "deprecated", "the notice must not corrupt the document on stdout")

	run, err = runUpdateMetadata(t, fullMetadata(), "renamed", "--moniker", "other")
	require.Error(t, err)
	require.Contains(t, err.Error(), "both as a positional argument and as --moniker")
	require.Empty(t, run.node.queried)
}

// --offline cannot read the current record, so it is refused with a reason
// rather than failing inside the query with a generic no-node error.
func TestUpdateMetadataRefusesOffline(t *testing.T) {
	run, err := runUpdateMetadata(t, fullMetadata(), "--website", "x", "--offline")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--offline")
	require.Empty(t, run.node.queried)
}

// A node that cannot return the slot must stop the command: without the current
// record there is nothing safe to merge onto, and sending the patch alone is
// exactly the wipe this command exists to prevent.
func TestUpdateMetadataStopsWhenSlotIsUnreadable(t *testing.T) {
	run, err := runUpdateMetadataAgainst(t, 4, fullMetadata(), "--website", "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "read current metadata of slot 3")
	require.Equal(t, []uint64{3}, run.node.queried)
	require.Empty(t, run.stdout.String(), "no transaction may be generated from a patch alone")
}

// An unnamed field can already be over the limit on-chain — genesis authoring
// does not validate metadata — and the named values passing says nothing about
// it. The MERGED record is what the keeper validates, so the CLI validates that
// too, and says which field to name.
func TestUpdateMetadataValidatesTheMergedRecord(t *testing.T) {
	current := fullMetadata()
	current.Moniker = strings.Repeat("m", 600)

	run, err := runUpdateMetadata(t, current, "--website", "https://new.example")
	require.Error(t, err, "the chain would refuse this record; the CLI must not build it")
	require.Contains(t, err.Error(), "moniker exceeds 512 bytes")
	require.Contains(t, err.Error(), "name it to replace it")
	require.Empty(t, run.stdout.String())

	// Naming the over-long field replaces it, and the record is valid again.
	run, err = runUpdateMetadata(t, current, "--website", "https://new.example", "--moniker", "fixed")
	require.NoError(t, err)
	require.NoError(t, types.ValidateMetadata(run.message(t).Metadata))
}

// The keeper refuses a signer other than the slot's operator. The record is in
// hand before anything is sent, so that is a local error here rather than a
// broadcast that fails in the block while the command exits 0.
func TestUpdateMetadataRefusesNonOperatorSigner(t *testing.T) {
	stranger := sdk.AccAddress(append([]byte{9}, make([]byte, 19)...)).String()
	run, err := runUpdateMetadata(t, fullMetadata(), "--website", "x", "--from", stranger)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is operated by "+run.operator)
	require.Contains(t, err.Error(), "--from is "+stranger)
	require.Equal(t, []uint64{3}, run.node.queried, "the operator is known from the record that was read")
	require.Empty(t, run.stdout.String(), "nothing may be generated for a signer the chain would refuse")
}

// With no arguments the error says what the command wants, not "requires at
// least 1 arg(s)".
func TestUpdateMetadataNoArgsNamesTheSlotIDAndFlags(t *testing.T) {
	cmd := updateMetadataCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--website", "x"})
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "slot-id is required")
	require.Contains(t, err.Error(), "--security-contact")
}
