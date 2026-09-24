package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/twilight-project/twilight-core/x/coreslot/types"
)

func GetTxCmd() *cobra.Command {
	// RunE is client.ValidateCmd for the same reason the `tx` and `query` parents
	// use it: cobra only reports an unknown subcommand for the ROOT command. A
	// non-root parent that carries subcommands but is not itself runnable falls
	// through to printing help and EXITING ZERO.
	//
	// That matters here because this tree replaced an AutoCLI-generated one whose
	// names it does not share. Without this, `tx coreslot register-core-slot …` —
	// the name an existing script would carry — would print help, exit 0, and
	// register nobody. A script guarded by `&& echo ok` would report success. The
	// generated names used to fail loudly; they must keep failing loudly.
	cmd := &cobra.Command{
		Use:                        "coreslot",
		Short:                      "Core-slot lifecycle transactions",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}
	cmd.AddCommand(
		registerCmd(), activateCmd(), inactivateCmd(), suspendCmd(), removeCmd(), rotateCmd(),
		updatePayoutCmd(), updateMetadataCmd(), updateSettlementCmd(), updateSelectionPolicyCmd(), updateParamsCmd(),
		scheduleUpgradeCmd(), cancelUpgradeCmd(),
		nominateAuthorityCmd(), acceptAuthorityCmd(), cancelAuthorityNominationCmd(),
	)
	return cmd
}

func broadcast(cmd *cobra.Command, msg sdk.Msg) error {
	ctx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	return clienttx.GenerateOrBroadcastTxCLI(ctx, cmd.Flags(), msg)
}

func signer(cmd *cobra.Command) (string, error) {
	ctx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return "", err
	}
	return ctx.GetFromAddress().String(), nil
}

func txCmd(use string, args cobra.PositionalArgs, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Args: args, RunE: run}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func registerCmd() *cobra.Command {
	cmd := txCmd("register [operator] [payout] [settlement] [consensus-pubkey-base64] [moniker]", cobra.ExactArgs(5), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		pk, err := txPubKeyAny(args[3])
		if err != nil {
			return err
		}
		rateBps, err := cmd.Flags().GetUint64("selection-rate-bps")
		if err != nil {
			return err
		}
		maxSelected, err := cmd.Flags().GetUint64("max-selected-participants")
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgRegisterCoreSlot{
			Authority: from, OperatorAddress: args[0], PayoutAddress: args[1], SettlementAddress: args[2],
			ConsensusPubkey: pk, Metadata: &types.OperatorMetadata{Moniker: args[4]},
			InitialSelectionPolicy: &types.InitialSelectionPolicy{
				SelectionRateBps: rateBps, MaxSelectedParticipants: maxSelected,
			},
		})
	})
	// Operator configuration, not protocol constants: convenience defaults with
	// no protocol standing, constrained only by the local §27 rule.
	cmd.Flags().Uint64("selection-rate-bps", 2_500, "initial selection rate in basis points")
	cmd.Flags().Uint64("max-selected-participants", 10, "initial per-slot maximum selected participants")
	return cmd
}

func updateSelectionPolicyCmd() *cobra.Command {
	return txCmd("update-selection-policy [slot-id] [selection-rate-bps] [max-selected-participants]", cobra.ExactArgs(3), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		rateBps, err := strconv.ParseUint(args[1], 10, 64)
		if err != nil {
			return err
		}
		maxSelected, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgUpdateSelectionPolicy{
			Operator: from, SlotId: id, SelectionRateBps: rateBps, MaxSelectedParticipants: maxSelected,
		})
	})
}

func updateSettlementCmd() *cobra.Command {
	return txCmd("update-settlement [slot-id] [settlement-address]", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgUpdateSettlementAddress{Operator: from, SlotId: id, SettlementAddress: args[1]})
	})
}

func activateCmd() *cobra.Command {
	return txCmd("activate [slot-id]", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgActivateCoreSlot{Authority: from, SlotId: id})
	})
}

func inactivateCmd() *cobra.Command {
	return txCmd("inactivate [slot-id] [reason]", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgInactivateCoreSlot{AuthorityOrOperator: from, SlotId: id, Reason: args[1]})
	})
}

func suspendCmd() *cobra.Command {
	return txCmd("suspend [slot-id] [reason] [evidence-reference]", cobra.ExactArgs(3), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgSuspendCoreSlot{Authority: from, SlotId: id, Reason: args[1], EvidenceReference: args[2]})
	})
}

func removeCmd() *cobra.Command {
	return txCmd("remove [slot-id] [reason]", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgRemoveCoreSlot{Authority: from, SlotId: id, Reason: args[1]})
	})
}

func rotateCmd() *cobra.Command {
	return txCmd("rotate-key [slot-id] [new-consensus-pubkey-base64]", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		pk, err := txPubKeyAny(args[1])
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgRotateConsensusKey{Authority: from, SlotId: id, NewConsensusPubkey: pk})
	})
}

func updatePayoutCmd() *cobra.Command {
	return txCmd("update-payout [slot-id] [new-payout]", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgUpdatePayoutAddress{Operator: from, SlotId: id, NewPayoutAddress: args[1]})
	})
}

// metadataFields is the five OperatorMetadata fields in proto order: the flag
// each is set by, the proto name a query shows it under, and where it lives.
//
// One table drives the flags, the merge, the validation and the preview, so a
// field cannot be settable but silently dropped from the merge — which is the
// shape of the defect this command is here to fix.
var metadataFields = []struct {
	flag  string
	field string
	sel   func(*types.OperatorMetadata) *string
}{
	{"moniker", "moniker", func(m *types.OperatorMetadata) *string { return &m.Moniker }},
	{"identity", "identity", func(m *types.OperatorMetadata) *string { return &m.Identity }},
	{"website", "website", func(m *types.OperatorMetadata) *string { return &m.Website }},
	{"security-contact", "security_contact", func(m *types.OperatorMetadata) *string { return &m.SecurityContact }},
	{"details", "details", func(m *types.OperatorMetadata) *string { return &m.Details }},
}

// metadataPatch is the set of fields one update-metadata invocation NAMED, keyed
// by flag, each with the value given — including an explicit empty string, which
// is how a field is cleared. A field that is not named is absent, and absent
// means "keep what the chain has", never "set to empty".
//
// That distinction is the whole reason the type exists. The message the chain
// accepts cannot express it: proto3 strings have no "absent", and the keeper
// stores the WHOLE object it is given (#181). So it is resolved here, by reading
// the current record and sending back the merge, and the patch is the record of
// what the operator actually asked for along the way.
type metadataPatch map[string]string

// metadataPatchFromFlags collects the named fields. The positional moniker is
// the pre-#181 spelling, kept so an existing runbook still works; it is the same
// as --moniker and may not be combined with it, since two spellings of one field
// in one command is a mistake whichever value would win.
func metadataPatchFromFlags(fs *pflag.FlagSet, positional []string) (metadataPatch, error) {
	patch := metadataPatch{}
	for _, f := range metadataFields {
		if !fs.Changed(f.flag) {
			continue
		}
		v, err := fs.GetString(f.flag)
		if err != nil {
			return nil, err
		}
		patch[f.flag] = v
	}
	if len(positional) > 0 {
		if _, both := patch["moniker"]; both {
			return nil, fmt.Errorf("moniker given both as a positional argument and as --moniker; use --moniker")
		}
		patch["moniker"] = positional[0]
	}
	if len(patch) == 0 {
		names := make([]string, 0, len(metadataFields))
		for _, f := range metadataFields {
			names = append(names, "--"+f.flag)
		}
		return nil, fmt.Errorf("no metadata field given; pass at least one of %s (an explicit empty value, e.g. --website \"\", clears that field)",
			strings.Join(names, ", "))
	}
	return patch, nil
}

// validate holds the NAMED values to the limit the keeper enforces, through the
// keeper's own validator, so a value that would be refused on-chain is refused
// before any node is reached. It is the local half of the check: the merged
// record is validated again once the current one is read, because an unnamed
// field can already be over the limit on-chain (genesis does not validate
// metadata) and only the merge can see that.
func (p metadataPatch) validate() error {
	if err := types.ValidateMetadata(p.apply(nil)); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}

// apply returns current with the named fields replaced. current may be nil (a
// slot registered with no metadata), which merges as all-empty.
func (p metadataPatch) apply(current *types.OperatorMetadata) *types.OperatorMetadata {
	merged := &types.OperatorMetadata{}
	if current != nil {
		*merged = *current
	}
	for _, f := range metadataFields {
		if v, ok := p[f.flag]; ok {
			*f.sel(merged) = v
		}
	}
	return merged
}

// printMetadataPreview shows the record as the chain will store it — all five
// fields, not just the ones named — so the operator sees exactly what a full
// replace is about to write. It goes to stderr so --generate-only's stdout stays
// a bare transaction document that scripts can pipe.
func printMetadataPreview(w io.Writer, slotID uint64, current, merged *types.OperatorMetadata, patch metadataPatch) {
	fmt.Fprintf(w, "slot %d metadata as this transaction will store it (the chain replaces the whole record):\n", slotID)
	// The "before" values are read off a merge with no patch: a copy, so current
	// is never written to, and nil (a slot with no metadata) reads as all-empty.
	before := metadataPatch{}.apply(current)
	for _, f := range metadataFields {
		after, was := *f.sel(merged), *f.sel(before)
		if _, named := patch[f.flag]; !named {
			fmt.Fprintf(w, "  %-17s %q  (unchanged)\n", f.field, after)
			continue
		}
		switch {
		case after == "" && was != "":
			fmt.Fprintf(w, "  %-17s %q  (cleared; was %q)\n", f.field, after, was)
		case after == was:
			fmt.Fprintf(w, "  %-17s %q  (unchanged)\n", f.field, after)
		default:
			fmt.Fprintf(w, "  %-17s %q  (set; was %q)\n", f.field, after, was)
		}
	}
}

// metadataArgs is cobra.RangeArgs(1, 2) with an error that says what the
// command wants, rather than "requires at least 1 arg(s)".
func metadataArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("slot-id is required: %s [slot-id] --moniker|--identity|--website|--security-contact|--details", cmd.Name())
	}
	return cobra.RangeArgs(1, 2)(cmd, args)
}

// runAncestorPersistentPreRun runs what cobra would have run had the command
// no PersistentPreRunE of its own: the nearest ancestor's, if any.
func runAncestorPersistentPreRun(c *cobra.Command, args []string) error {
	for p := c.Parent(); p != nil; p = p.Parent() {
		switch {
		case p.PersistentPreRunE != nil:
			return p.PersistentPreRunE(c, args)
		case p.PersistentPreRun != nil:
			p.PersistentPreRun(c, args)
			return nil
		}
	}
	return nil
}

// updateMetadataCmd is a read-modify-write, not a plain message wrapper.
//
// MsgUpdateOperatorMetadata carries the whole OperatorMetadata and the keeper
// stores it whole, so a message built from only the fields an operator typed
// clears the rest. The previous form of this command did exactly that: it took a
// moniker and nothing else, so every call wiped identity, website,
// security_contact and details (#181). Now the current record is read from the
// node first, the named fields are laid over it, and the merge is what is sent.
//
// The chain-side semantics are unchanged by this: any other client still has to
// send all five fields. Changing the message to a merge is a consensus change
// and is deferred to the v0.4.0 upgrade.
func updateMetadataCmd() *cobra.Command {
	cmd := txCmd("update-metadata [slot-id]", metadataArgs, func(cmd *cobra.Command, args []string) error {
		id, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return err
		}
		// Everything local is checked before anything remote is reached, so a
		// missing flag or an over-long value costs no round trip.
		patch, err := metadataPatchFromFlags(cmd.Flags(), args[1:])
		if err != nil {
			return err
		}
		if err := patch.validate(); err != nil {
			return err
		}
		if len(args) == 2 {
			fmt.Fprintln(cmd.ErrOrStderr(), "note: the positional moniker is deprecated; use --moniker")
		}
		clientCtx, err := client.GetClientTxContext(cmd)
		if err != nil {
			return err
		}
		if clientCtx.Offline {
			return fmt.Errorf("update-metadata reads the slot's current metadata from a node, so it cannot run with --offline")
		}
		// The read goes through the same generated client every query command
		// uses, against --node (the transaction flag set carries no gRPC
		// endpoint), so a --generate-only run reads the same record a broadcast
		// would.
		resp, err := types.NewQueryClient(clientCtx).CoreSlot(cmd.Context(), &types.QueryCoreSlotRequest{SlotId: id})
		if err != nil {
			return fmt.Errorf("read current metadata of slot %d: %w", id, err)
		}
		if resp.Slot == nil {
			return fmt.Errorf("read current metadata of slot %d: empty response", id)
		}
		// The keeper refuses a signer other than the slot's operator; with the
		// record in hand that is known here, and refusing now is the difference
		// between a local error and a broadcast that fails in the block while the
		// command exits 0.
		from := clientCtx.GetFromAddress().String()
		if from != resp.Slot.OperatorAddress {
			return fmt.Errorf("slot %d is operated by %s; --from is %s, which the chain would refuse", id, resp.Slot.OperatorAddress, from)
		}
		merged := patch.apply(resp.Slot.Metadata)
		// The MERGED record is what the chain validates, and it is not enough that
		// the named values pass: genesis authoring does not validate metadata, so a
		// slot can carry an over-long field the keeper would refuse the moment it
		// is written back. Naming that field replaces it; leaving it unnamed
		// sends a record the chain rejects, which is refused here instead.
		if err := types.ValidateMetadata(merged); err != nil {
			return fmt.Errorf("the record as it would be stored is invalid (%v); a field not named here already exceeds the limit on-chain, so name it to replace it", err)
		}
		printMetadataPreview(cmd.ErrOrStderr(), id, resp.Slot.Metadata, merged, patch)
		return broadcast(cmd, &types.MsgUpdateOperatorMetadata{Operator: from, SlotId: id, Metadata: merged})
	})
	// Before the root's pre-run: which of the five flags the operator TYPED.
	//
	// The root's PersistentPreRunE runs the SDK's InterceptConfigsPreRunHandler,
	// whose bindFlags applies every viper value to the flag of the same name
	// that the operator did not set — through pflag Set, which marks the flag
	// Changed. `moniker` is a top-level key in every config.toml (`twilightd
	// init` writes the node's, and the SDK writes the hostname on the first CLI
	// run against a new home), and <BINARY>_<FLAG> environment variables reach
	// all five the same way. So after the pre-run, Changed no longer means
	// "typed": without this hook every call that omitted --moniker replaced the
	// on-chain moniker with the LOCAL NODE's, which is the wipe of #181 in a new
	// shape, and the positional form failed as a double moniker on every home
	// that had a config.toml.
	//
	// Cobra runs only the nearest PersistentPreRunE, so this one delegates to
	// the ancestor's exactly as cobra would have, then restores the five flags to
	// what the operator typed. The flag names stay as they are: renaming would
	// dodge the config.toml collision but not the environment path, and the
	// flags should be named for the fields they set.
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		typed := make(map[string]bool, len(metadataFields))
		for _, f := range metadataFields {
			typed[f.flag] = c.Flags().Changed(f.flag)
		}
		if err := runAncestorPersistentPreRun(c, args); err != nil {
			return err
		}
		for _, f := range metadataFields {
			if typed[f.flag] {
				continue
			}
			flag := c.Flags().Lookup(f.flag)
			if err := flag.Value.Set(flag.DefValue); err != nil {
				return err
			}
			flag.Changed = false
		}
		return nil
	}
	cmd.Short = "Update an operator's slot metadata, keeping the fields not named"
	cmd.Long = `Update one or more of a slot's metadata fields.

The command reads the slot's current metadata from the node, replaces only the
fields named by flag, and sends the merged record. Fields not named keep their
current value. An explicit empty value clears a field:

  update-metadata 3 --website https://example.org          # only website changes
  update-metadata 3 --website ""                           # only website is cleared
  update-metadata 3 --moniker ops --security-contact a@b.c # two fields change

At least one field must be named. Each value is limited to 512 bytes, the same
limit the chain enforces. The record as it will be stored is printed to stderr
before the transaction is generated or broadcast, and --from must be the slot's
operator.

Because the current record is read from --node, --offline is not supported and
--generate-only still needs --node. A --generate-only document is a snapshot of
that read: broadcasting it later, after the record changed, writes the snapshot
back over the newer record.

The older form "update-metadata [slot-id] [moniker]" is still accepted and now
means --moniker; it is deprecated.`
	for _, f := range metadataFields {
		cmd.Flags().String(f.flag, "", "new value of the "+f.field+" field; pass \"\" to clear it")
	}
	return cmd
}

func updateParamsCmd() *cobra.Command {
	return txCmd("update-params [params-json-file]", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		bz, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		params, err := decodeParams(cmd, bz)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgUpdateParams{Authority: from, Params: params})
	})
}

// scheduleUpgradeCmd schedules a coordinated halt.
//
// It lives under `coreslot` rather than a top-level `upgrade` command because that
// is where the authority is. The x/upgrade module's own messages are unreachable
// on this chain by design — its authority is a module address with no private key —
// so this is the only route to a plan. See ADR-0003.
func scheduleUpgradeCmd() *cobra.Command {
	return txCmd("schedule-upgrade [name] [height] [info]", cobra.RangeArgs(2, 3), func(cmd *cobra.Command, args []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		height, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("height must be an integer: %w", err)
		}
		// The chain refuses a non-future height on its own authority; this refuses
		// an obviously impossible one so a typo costs no round trip.
		if height <= 0 {
			return fmt.Errorf("height must be positive")
		}
		info := ""
		if len(args) == 3 {
			info = args[2]
		}
		return broadcast(cmd, &types.MsgScheduleUpgrade{
			Authority: from, Name: args[0], Height: height, Info: info,
		})
	})
}

func cancelUpgradeCmd() *cobra.Command {
	return txCmd("cancel-upgrade", cobra.NoArgs, func(cmd *cobra.Command, _ []string) error {
		from, err := signer(cmd)
		if err != nil {
			return err
		}
		return broadcast(cmd, &types.MsgCancelUpgrade{Authority: from})
	})
}

// parseAuthorityRole accepts the two operational roles by short name.
//
// The unspecified zero value is unreachable from the CLI by construction: an
// unrecognized word is an error rather than a default, so a mistyped role cannot
// silently become a nomination for the primary authority — the more
// consequential of the two.
func parseAuthorityRole(value string) (types.AuthorityRole, error) {
	switch value {
	case "primary":
		return types.AuthorityRole_AUTHORITY_ROLE_PRIMARY, nil
	case "emergency":
		return types.AuthorityRole_AUTHORITY_ROLE_EMERGENCY, nil
	default:
		return types.AuthorityRole_AUTHORITY_ROLE_UNSPECIFIED,
			fmt.Errorf("role must be %q or %q, got %q", "primary", "emergency", value)
	}
}

func nominateAuthorityCmd() *cobra.Command {
	return txCmd("nominate-authority [primary|emergency] [nominee]", cobra.ExactArgs(2),
		func(cmd *cobra.Command, args []string) error {
			from, err := signer(cmd)
			if err != nil {
				return err
			}
			role, err := parseAuthorityRole(args[0])
			if err != nil {
				return err
			}
			return broadcast(cmd, &types.MsgNominateAuthority{Authority: from, Role: role, Nominee: args[1]})
		})
}

func acceptAuthorityCmd() *cobra.Command {
	return txCmd("accept-authority [primary|emergency]", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			// Signed by the nominee, not the incumbent — this signature IS the proof
			// that the destination key is controlled.
			from, err := signer(cmd)
			if err != nil {
				return err
			}
			role, err := parseAuthorityRole(args[0])
			if err != nil {
				return err
			}
			return broadcast(cmd, &types.MsgAcceptAuthority{Nominee: from, Role: role})
		})
}

func cancelAuthorityNominationCmd() *cobra.Command {
	return txCmd("cancel-authority-nomination [primary|emergency]", cobra.ExactArgs(1),
		func(cmd *cobra.Command, args []string) error {
			from, err := signer(cmd)
			if err != nil {
				return err
			}
			role, err := parseAuthorityRole(args[0])
			if err != nil {
				return err
			}
			return broadcast(cmd, &types.MsgCancelAuthorityNomination{Authority: from, Role: role})
		})
}

// decodeParams reads a Params document through the chain's own JSON codec.
//
// That codec is the SINGLE parser, deliberately. It already accepts both numeric
// spellings an operator can hold — gogoproto's jsonpb strips the quotes from a
// 64-bit integer before parsing it, so proto3 output like "max_active_slots":"100"
// and a hand-written 100 both decode — so query output round-trips without a
// second parser contract existing alongside it.
//
// An earlier version fell back to encoding/json on the belief that the codec
// would refuse bare numbers. It does not, so the fallback bought no
// compatibility, and its only real effect was to accept documents the codec
// correctly REJECTS. encoding/json ignores unknown fields, so a typo such as
// key_rotation_delay_block decoded to the zero value instead of failing — and
// because update-params writes the WHOLE struct and Params.Validate permits zero
// there, an authorized update would silently persist a parameter nobody chose.
//
// Rejecting an unrecognized field is the property worth keeping: in a
// whole-struct write, a field the parser does not understand is far more likely
// to be a mistake than an intention.

func decodeParams(cmd *cobra.Command, bz []byte) (*types.Params, error) {
	ctx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return nil, err
	}
	var params types.Params
	if err := ctx.Codec.UnmarshalJSON(bz, &params); err != nil {
		return nil, fmt.Errorf("decode params: %w", err)
	}
	return &params, nil
}
