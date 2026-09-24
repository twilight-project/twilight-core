#!/usr/bin/env bash
#
# check-genesis-faults.sh — prove that check-genesis.sh actually checks.
#
# A verifier that stops verifying does not go quiet, it goes GREEN. That is the
# worst failure mode available to it, and it is not hypothetical: three separate
# bugs in the first draft of check-genesis.sh each disabled real checks while the
# script still reported success.
#
#   - `jq -e` sets exit status 1 when the OUTPUT VALUE is false, so every
#     boolean-false field read as missing.
#   - Slot status was matched as CORE_SLOT_STATUS_ACTIVE; the enum is
#     SLOT_STATUS_ACTIVE, so a correct genesis counted ZERO active slots.
#   - The consensus path was written `.consensus_params.block`; it is
#     `.consensus.params.block`, so the max_gas check read nothing at all.
#
# Every one of those fails toward a confident wrong answer. So each check gets a
# fault that must make it fire.
#
# THE CONTRACT IS AN EXACT SET COMPARISON over stable check IDs:
#
#     declared  ==  exercised  ==  targeted
#
# Prose is matched nowhere. Substring matching on labels previously let one
# check's pattern be satisfied by ANOTHER check's output — three snapshot mirror
# checks looked covered while no fault targeted them, and exact ids surfaced that
# the moment they were introduced.
#
# `declared` is a STATIC list in check-genesis.sh rather than something derived
# from a run, because a coverage mechanism built only from what executed cannot
# distinguish "no fault triggers this" from "this can no longer fire at all", and
# the second is the dangerous one.
#
# THE COVERAGE CLAIM IS ENFORCED, NOT ASSERTED. This file used to say every check
# had a fault case while 29 of them did not, and nine live checks could be deleted
# outright with it still reporting "every check fires on its own fault". The list
# of checks is now DERIVED from what the checker actually printed across every run
# below, and a check with no matching fault case fails this suite.
#
# THE ASSERTION IS SPECIFIC, DELIBERATELY. It is not enough that the checker
# exited non-zero — a mutation that broke something unrelated would satisfy that
# while the intended check stayed dead. Each case names the check it must see
# fail, and a non-zero exit without that check named is itself a failure.
#
# The base genesis is BUILT WITH THE REAL BINARY rather than committed as a
# fixture. A fixture drifts from the schema the chain actually emits, and a
# faults suite testing a stale shape is the same silent-green problem one level
# up.
#
# No chain is run. The InitChain cases (the baseline, the dry-run section, and
# the probe-lifetime cases) start the binary against a throwaway home just long
# enough for the ABCI handshake (about a second each, loopback only, ephemeral
# ports), and it never produces a block. This needs only
# the binary, jq, and a temp directory.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHECKER="$ROOT/scripts/check-genesis.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
# Built FRESH into the work directory unless a binary is named explicitly, the way
# check-cli-surface.sh does. Reusing build/twilightd would let a stale local build
# decide what the baseline genesis looks like, so the suite would be validating
# yesterday's schema while reporting on today's — the same silent-staleness this
# script exists to prevent, one level up.
BIN="${BIN:-}"

CASES=0; FAILED=0
pass() { CASES=$((CASES+1)); printf '  \033[32mok\033[0m    %s\n' "$1"; }
fail() { CASES=$((CASES+1)); FAILED=$((FAILED+1)); printf '  \033[31mBAD\033[0m   %s\n' "$1"; [[ -n "${2:-}" ]] && printf '        %s\n' "$2"; }
abort() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 2; }

command -v jq >/dev/null || abort "jq is required"
[[ -f "$CHECKER" ]] || abort "checker not found: $CHECKER"
if [[ -n "$BIN" ]]; then
  [[ -x "$BIN" ]] || abort "BIN is set but not executable: $BIN"
else
  BIN="$WORK/twilightd"
  echo "==> building a fresh binary"
  (cd "$ROOT" && go build -o "$BIN" ./cmd/twilightd) || abort "could not build the binary"
fi

CHAIN_ID="twilight-faults-1"

# ---- a genesis that must be COMPLETE and VALID ------------------------------------------
#
# Every mutation below is measured against this. If the baseline itself did not
# pass, each mutation would "fail" for a reason that has nothing to do with the
# check it claims to prove, and the whole suite would be theatre.
echo "==> building the baseline genesis with the real binary"
HOME_DIR="$WORK/home"
"$BIN" init faults-probe --chain-id "$CHAIN_ID" --home "$HOME_DIR" >/dev/null 2>&1 \
  || abort "twilightd init failed"

for n in op1 pay1 set1 op2 pay2 set2 auth eauth; do
  "$BIN" keys add "$n" --keyring-backend test --home "$HOME_DIR" >/dev/null 2>&1 \
    || abort "could not create key $n"
done
addr() { "$BIN" keys show "$1" -a --keyring-backend test --home "$HOME_DIR" 2>/dev/null; }

VK1="$WORK/vk1"; VK2="$WORK/vk2"
"$BIN" init v1 --chain-id "$CHAIN_ID" --home "$VK1" >/dev/null 2>&1
"$BIN" init v2 --chain-id "$CHAIN_ID" --home "$VK2" >/dev/null 2>&1
PK1="$("$BIN" comet show-validator --home "$VK1" | jq -r .key)"
PK2="$("$BIN" comet show-validator --home "$VK2" | jq -r .key)"
[[ -n "$PK1" && -n "$PK2" ]] || abort "could not read consensus pubkeys"

"$BIN" coreslot-genesis set-authorities "$(addr auth)" "$(addr eauth)" --home "$HOME_DIR" >/dev/null 2>&1 \
  || abort "set-authorities failed"
"$BIN" coreslot-genesis add "$(addr op1)" "$(addr pay1)" "$(addr set1)" "$PK1" node-1 --home "$HOME_DIR" >/dev/null 2>&1 \
  || abort "adding slot 1 failed"
"$BIN" coreslot-genesis add "$(addr op2)" "$(addr pay2)" "$(addr set2)" "$PK2" node-2 --home "$HOME_DIR" >/dev/null 2>&1 \
  || abort "adding slot 2 failed"

GOOD="$WORK/genesis.good.json"
jq '.consensus.params.block.max_gas="50000000" | .app_state.coreslot.params.min_active_slots="2"' \
  "$HOME_DIR/config/genesis.json" >"$GOOD" || abort "could not finish the baseline genesis"

AUTH_ADDR="$(addr auth)"; EAUTH_ADDR="$(addr eauth)"; OP1_ADDR="$(addr op1)"
[[ -n "$AUTH_ADDR" && -n "$EAUTH_ADDR" && -n "$OP1_ADDR" ]] || abort "could not read the baseline addresses"

# Module-account addresses, WRITTEN OUT rather than derived. The checker derives
# them (sha256(name)[:20], bech32 by the binary); deriving them the same way here
# would make a wrong derivation agree with itself. These are the values the chain
# itself names when it refuses them at InitChain — the --initchain case below
# re-proves that on every run for the rewards account.
REWARDS_MODULE_ADDR=twilight1245yut9zht8q4hz39sd0lzqtzkuw5us5pd3c3u
FEE_COLLECTOR_ADDR=twilight17xpfvakm2amg962yls6f84z3kell8c5ltxtf5t
AUTHORITY_MODULE_ADDR=twilight17te68tpa0etfn4cmlqryw06uqh5qc2tp2fracm
# All-zero addresses at three lengths: 20 bytes (the usual account length), 32
# and 1. The chain refuses every one of them ("address is all zero"); an earlier
# checker compared against the 20-byte spelling only. Encoded once with
# `twilightd debug addr` from 40, 64 and 2 hex zeros.
ZERO_ADDR=twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqgugkct
ZERO_ADDR_32=twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqg86095
ZERO_ADDR_1=twilight1qqkdakzw

# The checker's required inputs, each with the value the baseline satisfies. The
# abort tests below iterate this list, so a new required input that is added
# here is tested for refusing to default automatically.
REQUIRED_VARS="GC_CHAIN_ID GC_ACTIVE_SLOTS GC_MAX_GAS GC_MIN_ACTIVE_SLOTS GC_DISTRIBUTION_METHOD GC_AUTHORITY GC_EMERGENCY_AUTHORITY"
required_value() {
  case "$1" in
    GC_CHAIN_ID) printf '%s' "$CHAIN_ID" ;;
    GC_ACTIVE_SLOTS) printf '2' ;;
    GC_MAX_GAS) printf '50000000' ;;
    GC_MIN_ACTIVE_SLOTS) printf '2' ;;
    GC_DISTRIBUTION_METHOD) printf 'DISTRIBUTION_METHOD_UNIFORM_ACTIVE_BLOCKS' ;;
    GC_AUTHORITY) printf '%s' "$AUTH_ADDR" ;;
    GC_EMERGENCY_AUTHORITY) printf '%s' "$EAUTH_ADDR" ;;
    *) abort "no baseline value for required input $1" ;;
  esac
}
# All required inputs except the one named (pass "" for all of them), as
# NAME=value words for env.
required_env() {
  local v
  REQ_ENV=()
  for v in $REQUIRED_VARS; do
    if [[ "$v" != "$1" ]]; then REQ_ENV+=("$v=$(required_value "$v")"); fi
  done
}

# Extra checker flags for the case being run. A plain string so an empty value
# expands to nothing under bash 3.2's `set -u`, where an empty array does not.
#
# The InitChain dry-run is the checker's DEFAULT, and the baseline below runs
# with it. The bulk of the mutants then run with --no-initchain: each is
# aimed at a static check, which must catch it on its own, and starting a node
# for every one would double the suite's runtime for no added proof. The
# dry-run's own section re-enables it.
CHECKER_FLAGS=""

# Every label the checker has ever printed, across the baseline and every mutant.
# This is what the coverage assertion at the end is derived from, so a check added
# to check-genesis.sh with no fault case here is DETECTED rather than assumed.
LABELS="$WORK/labels.all"
WANTS="$WORK/wants.all"
: >"$LABELS"; : >"$WANTS"

# Collect the stable IDs the checker emitted, not its prose. Prose is reworded;
# ids are the contract.
record_labels() {
  sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" 2>/dev/null \
    | awk '/^  (PASS|FAIL)  \[/{ sub(/^  (PASS|FAIL)  \[/,""); sub(/\].*$/,""); print }' \
    >>"$LABELS" || true
}

run_checker() { # run_checker <genesis> [extra env assignments...] -> writes $WORK/out, returns exit code
  local g="$1"; shift
  local rc=0
  required_env ""
  # shellcheck disable=SC2086 # CHECKER_FLAGS is deliberately word-split
  env "${REQ_ENV[@]}" "$@" \
    "$CHECKER" "$g" --bin "$BIN" $CHECKER_FLAGS >"$WORK/out" 2>&1 || rc=$?
  record_labels
  return $rc
}

echo
echo "==> baseline must PASS (nothing below means anything otherwise)"
if run_checker "$GOOD" && sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -q "PASS  \[native.initchain\]"; then
  pass "a complete genesis passes, InitChain dry-run included (the default)"
else
  printf '\033[31mBASELINE FAILED — the suite cannot run\033[0m\n'
  tail -25 "$WORK/out"
  exit 2
fi
CHECKER_FLAGS="--no-initchain"

# ---- each check gets a fault that must make IT fire --------------------------------------
#
# mutate <label> <jq-mutation> <check-name-that-must-fail> [extra env...]
mutate() {
  local label="$1" expr="$2" want="$3"; shift 3
  jq "$expr" "$GOOD" >"$WORK/mutant.json" 2>/dev/null || { fail "$label" "the mutation itself could not be applied"; return; }
  check_mutant "$label" "$want" "$@"
}

# mutate_raw <label> <awk-program> <check-name-that-must-fail> [extra env...]
#
# For faults jq cannot express, because jq's own parser erases them: a duplicate
# key. The awk program rewrites the pretty-printed baseline text.
mutate_raw() {
  local label="$1" prog="$2" want="$3"; shift 3
  awk "$prog" "$GOOD" >"$WORK/mutant.json" 2>/dev/null || { fail "$label" "the mutation itself could not be applied"; return; }
  check_mutant "$label" "$want" "$@"
}

# check_mutant <label> <check-name-that-must-fail> [extra env...] — judges $WORK/mutant.json
check_mutant() {
  local label="$1" want="$2"; shift 2
  local g="$WORK/mutant.json" rc=0
  if cmp -s "$g" "$GOOD"; then
    fail "$label" "the mutation changed nothing — it would pass for the wrong reason"
    return
  fi
  printf '%s\n' "$want" >>"$WANTS"
  run_checker "$g" "$@" || rc=$?
  if (( rc == 0 )); then
    fail "$label" "checker still exited 0"
    return
  fi
  # EXACT id match, anchored. Substring matching on prose previously let a short
  # pattern be satisfied by a different check's output, so a mutation could look
  # proven while the check it named stayed dead.
  if sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -q "FAIL  \[${want}\]"; then
    pass "$label"
  else
    fail "$label" "checker failed, but not on [${want}] — the intended check may be dead"
  fi
}

echo
echo "==> traps"
mutate "unlimited block gas is caught" \
  '.consensus.params.block.max_gas="-1"' trap.max_gas_finite
mutate "a wrong-but-finite max_gas is caught" \
  '.consensus.params.block.max_gas="40000000"' decision.max_gas
mutate "zero block gas is caught (it admits no transaction)" \
  '.consensus.params.block.max_gas="0"' trap.max_gas_finite GC_MAX_GAS=0
mutate "a negative block gas other than -1 is caught" \
  '.consensus.params.block.max_gas="-5"' trap.max_gas_finite GC_MAX_GAS=-5
mutate "an absent max_gas is caught, not read as finite" \
  'del(.consensus.params.block.max_gas)' trap.max_gas_finite
mutate "a display denom in a bank amount is caught" \
  '.app_state.bank.supply=[{denom:"twlt",amount:"1"}]' trap.bank_denoms_native
# The old check was a denylist of the two display spellings; each of these got
# past it. The check is now "every denom IS utwlt".
mutate "a near-miss denom (utwtl) in a balance is caught" \
  ".app_state.bank.balances=[{address:\"$OP1_ADDR\",coins:[{denom:\"utwtl\",amount:\"1\"}]}]" \
  trap.bank_denoms_native
mutate "a mixed-case display denom (Twlt) is caught" \
  '.app_state.bank.supply=[{denom:"Twlt",amount:"1"}]' trap.bank_denoms_native
mutate "an upper-cased base denom (uTWLT) is caught" \
  '.app_state.bank.supply=[{denom:"uTWLT",amount:"1"}]' trap.bank_denoms_native
# One over the cap, stated and in the balances. (21000000000001 is below 2^53, so
# a double would get this one right too; the two cases after it are the ones a
# double or a dropped carry gets wrong.)
mutate "a starting supply one above max_supply is caught" \
  ".app_state.bank.balances=[{address:\"$OP1_ADDR\",coins:[{denom:\"utwlt\",amount:\"21000000000001\"}]}]
   | .app_state.bank.supply=[{denom:\"utwlt\",amount:\"21000000000001\"}]" \
  trap.supply_within_max
mutate "balances above max_supply with no stated supply are caught" \
  ".app_state.bank.balances=[{address:\"$OP1_ADDR\",coins:[{denom:\"utwlt\",amount:\"30000000000000\"}]}]
   | .app_state.bank.supply=[]" \
  trap.supply_within_max
mutate "a module account as a slot payout address is caught" \
  ".app_state.coreslot.slots[0].payout_address=\"$FEE_COLLECTOR_ADDR\"" \
  trap.payout_not_module_account
mutate "an upper-cased module account as a payout address is caught" \
  ".app_state.coreslot.slots[1].payout_address=\"$(printf '%s' "$FEE_COLLECTOR_ADDR" | tr '[:lower:]' '[:upper:]')\"" \
  trap.payout_not_module_account
mutate "the zero address as a slot payout address is caught" \
  ".app_state.coreslot.slots[0].payout_address=\"$ZERO_ADDR\"" \
  trap.payout_not_module_account
mutate "a 32-byte all-zero payout address is caught" \
  ".app_state.coreslot.slots[0].payout_address=\"$ZERO_ADDR_32\"" \
  trap.payout_not_module_account
mutate "a 1-byte all-zero settlement address is caught" \
  ".app_state.coreslot.slots[0].settlement_address=\"$ZERO_ADDR_1\"" \
  trap.settlement_not_module_account
# Where exact arithmetic is the only thing that gets the answer right. Balances
# of 2^63, 2^63 and 1 sum to 2^64 + 1, over a max_supply of 2^64 — but as
# doubles 2^64 + 1 IS 2^64, so a floating-point sum (jq's `tonumber | add`)
# passes it. No stated supply, so only the balance sum can catch it.
mutate "balances one over a 2^64 cap are caught (a double sum would pass them)" \
  ".app_state.bank.balances=[
     {address:\"$OP1_ADDR\",coins:[{denom:\"utwlt\",amount:\"9223372036854775808\"}]},
     {address:\"$AUTH_ADDR\",coins:[{denom:\"utwlt\",amount:\"9223372036854775808\"}]},
     {address:\"$EAUTH_ADDR\",coins:[{denom:\"utwlt\",amount:\"1\"}]}]
   | .app_state.bank.supply=[]
   | .app_state.rewards.params.max_supply=\"18446744073709551616\"" \
  trap.supply_within_max GC_MAX_SUPPLY=18446744073709551616
# 20999999999999999 + 1 carries out of the low 15-digit limb. An adder that drops
# the carry gets 20000000000000000, under the cap.
mutate "a sum that carries across a limb is caught (a dropped carry would pass it)" \
  ".app_state.bank.balances=[
     {address:\"$OP1_ADDR\",coins:[{denom:\"utwlt\",amount:\"20999999999999999\"}]},
     {address:\"$AUTH_ADDR\",coins:[{denom:\"utwlt\",amount:\"1\"}]}]
   | .app_state.bank.supply=[]
   | .app_state.rewards.params.max_supply=\"20999999999999999\"" \
  trap.supply_within_max GC_MAX_SUPPLY=20999999999999999
mutate "denom metadata whose base is the display denom is caught" \
  '.app_state.bank.denom_metadata=[{base:"twlt",display:"utwlt",
     denom_units:[{denom:"twlt",exponent:0},{denom:"utwlt",exponent:6}]}]' \
  trap.denom_metadata_base
mutate "a module account as a slot settlement address is caught" \
  ".app_state.coreslot.slots[0].settlement_address=\"$REWARDS_MODULE_ADDR\"" \
  trap.settlement_not_module_account
# Decided as the treasury, so the decision check agrees and only the
# module-account rule can be what refuses it.
mutate "a module account as the treasury is caught" \
  ".app_state.rewards.params.treasury_address=\"$AUTHORITY_MODULE_ADDR\"
   | .app_state.rewards.reward_config_versions[0].treasury_address=\"$AUTHORITY_MODULE_ADDR\"
   | .app_state.rewards.current_epoch_config.treasury_address=\"$AUTHORITY_MODULE_ADDR\"" \
  trap.treasury_not_module_account GC_TREASURY_ADDRESS="$AUTHORITY_MODULE_ADDR"
mutate "a treasury share with no address is caught" \
  '.app_state.rewards.params.emission_treasury_share_bps="100"
   | .app_state.rewards.reward_config_versions[0].emission_treasury_share_bps="100"' \
  trap.treasury_address_for_share GC_EMISSION_TREASURY_SHARE_BPS=100
mutate "min_active_slots above the active count is caught" \
  '.app_state.coreslot.params.min_active_slots="3"' trap.active_within_bounds GC_MIN_ACTIVE_SLOTS=3

echo
echo "==> mirror consistency (params vs the canonical version that governs)"
mutate "subsidy drift between params and version 1" \
  '.app_state.rewards.params.initial_block_subsidy="500000"' mirror.subsidy \
  GC_INITIAL_BLOCK_SUBSIDY=500000
mutate "treasury-address drift" \
  '.app_state.rewards.reward_config_versions[0].treasury_address="twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"' \
  mirror.treasury_address
mutate "epoch-length drift" \
  '.app_state.rewards.params.epoch_length_blocks="400"' mirror.epoch_length \
  GC_EPOCH_LENGTH_BLOCKS=400
mutate "epoch anchor not at initial_height" \
  '.app_state.rewards.epoch_config_versions[0].effective_start_height="7"' \
  mirror.epoch_anchor

echo
echo "==> immutable bounds"
mutate "epoch length below the floor" \
  '.app_state.rewards.params.epoch_length_blocks="100"
   | .app_state.rewards.epoch_config_versions[0].epoch_length_blocks="100"' \
  bound.epoch_length GC_EPOCH_LENGTH_BLOCKS=100
mutate "treasury share above the ceiling" \
  '.app_state.rewards.params.emission_treasury_share_bps="6000"
   | .app_state.rewards.reward_config_versions[0].emission_treasury_share_bps="6000"' \
  bound.emission_share GC_EMISSION_TREASURY_SHARE_BPS=6000
mutate "recipients per chunk above the ceiling" \
  '.app_state.mining.settlement_params_versions[0].max_recipients_per_chunk="33"' \
  bound.recipients_per_chunk
mutate "chunks per settlement above the ceiling" \
  '.app_state.mining.settlement_params_versions[0].max_chunks_per_settlement="5"' \
  bound.chunks_per_settlement
mutate "payout floor below the ratified minimum" \
  '.app_state.mining.settlement_params_versions[0].min_recipient_payout_amount="9999"' \
  bound.min_payout
mutate "max_active_slots above the immutable ceiling" \
  '.app_state.coreslot.params.max_active_slots="101"' bound.max_active_slots

echo
echo "==> fresh-genesis invariants"
mutate "a chain that has already emitted" \
  '.app_state.rewards.state.cumulative_emitted="1"' fresh.cumulative_emitted
mutate "a non-empty epoch schedule" \
  '.app_state.rewards.scheduled_epoch_configs=[{effective_epoch:"5",epoch_length_blocks:"720"}]' \
  fresh.sched_epoch_empty
mutate "a second settlement params version" \
  '.app_state.mining.settlement_params_versions += [.app_state.mining.settlement_params_versions[0]]' \
  fresh.settlement_versions_count
mutate "rewards already paused at genesis" \
  '.app_state.rewards.pause_state.current_paused=true' fresh.paused

echo
echo "==> slot status and decisions"
mutate "an inactive slot reduces the active count" \
  '.app_state.coreslot.slots[0].status="SLOT_STATUS_INACTIVE"' trap.active_within_bounds
mutate "an unrecognised slot status is refused, not ignored" \
  '.app_state.coreslot.slots[0].status="SLOT_STATUS_SOMETHING_NEW"' \
  trap.slot_status_known
mutate "the wrong chain-id" '.chain_id="twilight-wrong-1"' decision.chain_id
mutate "self-registration enabled" \
  '.app_state.coreslot.params.allow_self_registration=true' decision.allow_self_registration
mutate "one key holding both authority roles" \
  '.app_state.coreslot.params.emergency_authority=.app_state.coreslot.params.authority' \
  decision.authorities_distinct
mutate "a changed native denom" \
  '.app_state.rewards.params.native_denom="uother"' trap.native_denom

echo
echo "==> fresh-genesis invariants, one fault each"
mutate "an epoch other than the first" \
  '.app_state.rewards.state.current_epoch="2"' fresh.current_epoch
mutate "a carried remainder at genesis" \
  '.app_state.rewards.state.carry_forward_remainder="1"' fresh.carry_forward_remainder
mutate "reward-enabled blocks already accrued" \
  '.app_state.rewards.open_reward_enabled_blocks="5"' fresh.open_reward_blocks
mutate "outstanding entitlement liability at genesis" \
  '.app_state.rewards.outstanding_entitlement_liability="1"' fresh.entitlement_liability
mutate "a params update already queued" \
  '.app_state.rewards.has_pending_params=true' fresh.has_pending_params
mutate "a pause already scheduled" \
  '.app_state.rewards.pause_state.has_pending=true' fresh.pause_pending
mutate "a second epoch config version" \
  '.app_state.rewards.epoch_config_versions += [.app_state.rewards.epoch_config_versions[0]]' \
  fresh.epoch_versions_count
mutate "a second reward config version" \
  '.app_state.rewards.reward_config_versions += [.app_state.rewards.reward_config_versions[0]]' \
  fresh.reward_versions_count
mutate "a non-empty reward schedule" \
  '.app_state.rewards.scheduled_reward_configs=[{effective_epoch:"5"}]' \
  fresh.sched_reward_empty
mutate "a non-empty settlement schedule" \
  '.app_state.mining.scheduled_settlement_params=[{effective_epoch:"5"}]' \
  fresh.sched_settlement_empty
mutate "a non-empty distribution-mode schedule" \
  '.app_state.mining.scheduled_distribution_modes=[{effective_epoch:"5"}]' \
  fresh.sched_distmode_empty
mutate "a non-empty selection-params schedule" \
  '.app_state.mining.scheduled_selection_params=[{effective_epoch:"5"}]' \
  fresh.sched_selection_empty
mutate "a finalized epoch at genesis" \
  '.app_state.rewards.finalized_epochs=[{epoch_number:"1"}]' fresh.finalized_epochs_empty
mutate "an entitlement at genesis" \
  '.app_state.rewards.slot_entitlements=[{slot_id:"1"}]' fresh.slot_entitlements_empty
mutate "a settlement at genesis" \
  '.app_state.mining.settlements=[{slot_id:"1"}]' fresh.settlements_empty
# The reproduced hand-over: native validation accepts this, the node starts, and
# the nominee's accept-authority takes the primary role after launch.
mutate "a pending authority nomination at genesis" \
  ".app_state.coreslot.pending_authority_transfers=[{role:\"AUTHORITY_ROLE_PRIMARY\",
     transfer:{nominee:\"$OP1_ADDR\",nominated_height:\"1\"}}]" \
  fresh.pending_authority_transfers_empty
mutate "a reserved consensus address at genesis" \
  '.app_state.coreslot.reserved_consensus_addresses=[{cons_address:"AAAAAAAAAAAAAAAAAAAAAAAAAAA=",
     slot_id:"1",reserved_until:"100000",reason:"lockout"}]' \
  fresh.reserved_consensus_addresses_empty
mutate "a pending key rotation at genesis" \
  '.app_state.coreslot.pending_key_rotations=[{slot_id:"1",requested_height:"1",effective_height:"2"}]' \
  fresh.pending_key_rotations_empty

echo
echo "==> the remaining immutable bounds"
mutate "a zero settlement window" \
  '.app_state.mining.settlement_params_versions[0].settlement_window_epochs="0"' \
  bound.settlement_window
mutate "a selection cooldown below the floor" \
  '.app_state.coreslot.params.selection_policy_update_cooldown_blocks="100"' \
  bound.selection_cooldown

echo
echo "==> the third copy: current_epoch_config"
# These three were previously masked: under substring matching, the params-vs-version
# mirror's pattern also matched the snapshot label, so they looked covered while no
# fault targeted them. Exact ids surfaced it.
mutate "snapshot epoch_length drift" \
  '.app_state.rewards.current_epoch_config.epoch_length_blocks="720"' \
  snapshot.epoch_length_blocks
mutate "snapshot subsidy drift" \
  '.app_state.rewards.current_epoch_config.initial_block_subsidy="500000"' \
  snapshot.initial_block_subsidy
mutate "snapshot treasury-address drift" \
  '.app_state.rewards.current_epoch_config.treasury_address="twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"' \
  snapshot.treasury_address
mutate "snapshot distribution_method drift" \
  '.app_state.rewards.current_epoch_config.distribution_method="DISTRIBUTION_METHOD_SNAPSHOT_UNIFORM"' \
  snapshot.distribution_method
mutate "snapshot treasury-share drift" \
  '.app_state.rewards.current_epoch_config.emission_treasury_share_bps="100"' \
  snapshot.emission_treasury_share_bps
mutate "snapshot fee_denom drift" \
  '.app_state.rewards.current_epoch_config.fee_denom="uother"' \
  snapshot.fee_denom
mutate "snapshot fee-treasury-share drift" \
  '.app_state.rewards.current_epoch_config.fee_treasury_share_bps="100"' \
  snapshot.fee_treasury_share_bps
mutate "snapshot halving_mode drift" \
  '.app_state.rewards.current_epoch_config.halving_mode="HALVING_MODE_UNSPECIFIED"' \
  snapshot.halving_mode
mutate "snapshot remainder_policy drift" \
  '.app_state.rewards.current_epoch_config.remainder_policy="REMAINDER_POLICY_BURN"' \
  snapshot.remainder_policy
mutate "treasury-share drift against the canonical version" \
  '.app_state.rewards.reward_config_versions[0].emission_treasury_share_bps="100"' \
  mirror.emission_share

echo
echo "==> the remaining launch decisions"
mutate "a different active slot count" \
  '.app_state.coreslot.slots += [.app_state.coreslot.slots[0] | .slot_id="3"]' decision.active_slots
mutate "a min_active_slots other than the one decided" \
  '.app_state.coreslot.params.min_active_slots="1"' decision.min_active_slots
mutate "an epoch length other than the one decided" \
  '.app_state.rewards.params.epoch_length_blocks="720"
   | .app_state.rewards.epoch_config_versions[0].epoch_length_blocks="720"
   | .app_state.rewards.current_epoch_config.epoch_length_blocks="720"' decision.epoch_length
mutate "a different max supply" \
  '.app_state.rewards.params.max_supply="42000000000000"' decision.max_supply
mutate "a subsidy other than the one decided" \
  '.app_state.rewards.params.initial_block_subsidy="500000"
   | .app_state.rewards.reward_config_versions[0].initial_block_subsidy="500000"
   | .app_state.rewards.current_epoch_config.initial_block_subsidy="500000"' decision.subsidy
mutate "a distribution method other than the one decided" \
  '.app_state.rewards.params.distribution_method="DISTRIBUTION_METHOD_WEIGHTED_ACTIVE_BLOCKS"
   | .app_state.rewards.current_epoch_config.distribution_method="DISTRIBUTION_METHOD_WEIGHTED_ACTIVE_BLOCKS"' \
  decision.distribution_method
mutate "a treasury share other than the one decided" \
  '.app_state.rewards.params.emission_treasury_share_bps="200"
   | .app_state.rewards.reward_config_versions[0].emission_treasury_share_bps="200"
   | .app_state.rewards.current_epoch_config.emission_treasury_share_bps="200"' \
  decision.emission_share
mutate "a treasury address other than the one decided" \
  '.app_state.rewards.params.treasury_address="twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
   | .app_state.rewards.reward_config_versions[0].treasury_address="twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"
   | .app_state.rewards.current_epoch_config.treasury_address="twilight1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq"' \
  decision.treasury_address
mutate "a declared block time that nothing else enforces" \
  '.app_state.rewards.params.target_block_time_seconds="10"' decision.target_block_time
mutate "a changed fee denom" \
  '.app_state.rewards.params.fee_denom="uother"
   | .app_state.rewards.current_epoch_config.fee_denom="uother"' trap.fee_denom
mutate "a malformed authority address" \
  '.app_state.coreslot.params.authority="not-an-address"' decision.authority_shape
mutate "a malformed emergency authority address" \
  '.app_state.coreslot.params.emergency_authority="not-an-address"' \
  decision.emergency_authority_shape
mutate "something only the chain itself rejects" \
  '.app_state.coreslot.slots[0].slot_id="0"' native.validate
# Replaced by a well-formed, distinct, fundable address: shape and distinctness
# both still pass, so only the stated decision can catch it.
mutate "an authority other than the one decided" \
  ".app_state.coreslot.params.authority=\"$OP1_ADDR\"" decision.authority
mutate "an emergency authority other than the one decided" \
  ".app_state.coreslot.params.emergency_authority=\"$OP1_ADDR\"" decision.emergency_authority
# Types-level validation accepts this; the slots were activated at height 1 and
# the chain now starts at 5, which panics at InitChain. The binary's
# coreslot-genesis validate reads the document's initial_height and refuses it.
mutate "an initial_height the slots were not activated at" \
  '.initial_height=5' native.coreslot_genesis
mutate "the emergency key allowed below min_active_slots, undecided" \
  '.app_state.coreslot.params.allow_emergency_below_min_active=true' \
  decision.allow_emergency_below_min

# ---- document shape: the chain must read what the checker reads -------------------------------
#
# The six genesis files the #198 review built, each keeping every agreed
# snake_case value in place for jq while a node started on it ran the attacker's
# (reproduced end to end, and each passed the checker with 0 failed before
# section 0 existed). Plus the cases each shape rule needs on its own.
echo
echo "==> document shape (the chain must read what the checker reads)"
ATT="$OP1_ADDR"
mutate "B1: a camelCase emergencyAuthority beside the agreed snake_case one" \
  ".app_state.coreslot.params.emergencyAuthority=\"$ATT\"" shape.lowercase_ascii_keys
mutate "B1: a camelCase pendingAuthorityTransfers nomination beside an empty snake_case list" \
  ".app_state.coreslot.pendingAuthorityTransfers=[{role:\"AUTHORITY_ROLE_PRIMARY\",
     transfer:{nominee:\"$ATT\",nominated_height:\"1\"}}]" shape.lowercase_ascii_keys
mutate "B1: a camelCase treasury address and share in all three copies" \
  ".app_state.rewards.params.treasuryAddress=\"$ATT\" | .app_state.rewards.params.emissionTreasuryShareBps=\"5000\"
   | .app_state.rewards.reward_config_versions[0].treasuryAddress=\"$ATT\"
   | .app_state.rewards.reward_config_versions[0].emissionTreasuryShareBps=\"5000\"
   | .app_state.rewards.current_epoch_config.treasuryAddress=\"$ATT\"
   | .app_state.rewards.current_epoch_config.emissionTreasuryShareBps=\"5000\"" \
  shape.lowercase_ascii_keys
mutate "B1: a second top-level App_State with new authorities and a premine" \
  ". + {\"App_State\": (.app_state | .coreslot.params.authority=\"$ATT\" | .coreslot.params.emergency_authority=\"$ATT\"
     | .bank.balances=[{address:\"$ATT\",coins:[{denom:\"utwlt\",amount:\"20000000000000\"}]}]
     | .bank.supply=[{denom:\"utwlt\",amount:\"20000000000000\"}])}" shape.case_distinct_keys
mutate "B1: a second top-level CONSENSUS with unlimited gas" \
  '. + {"CONSENSUS": (.consensus | .params.block.max_gas="-1")}' shape.top_level_keys
mutate "B1: a string initial_height plus a CometBFT consensus_params with unlimited gas" \
  '.initial_height="1" | .consensus_params=(.consensus.params | .block.max_gas="-1")' shape.sdk_form
mutate "a string initial_height on its own (the SDK would fall back to the CometBFT form)" \
  '.initial_height="1"' shape.initial_height_number
mutate "a JSON-RPC /genesis response instead of a genesis document" \
  '{jsonrpc:"2.0",id:-1,result:{genesis:.}}' shape.sdk_form
mutate "an unknown top-level key" \
  '. + {genesis_note:"hello"}' shape.top_level_keys
# U+017F (long s) folds to "s" in Go's decoder: "app_ſtate" is read AS app_state,
# and as the later key it wins. Verified against encoding/json directly.
mutate "a key that only Unicode case folding maps onto app_state" \
  '. + {"app_\u017ftate": (.app_state | .coreslot.params.authority="'"$ATT"'")}' \
  shape.lowercase_ascii_keys
# Exact duplicates are erased by every parser that reads them, jq included, so
# these are written into the text. The attacker's copy goes FIRST so jq, which
# keeps the last, still sees the agreed value — only the duplicate rule can fire.
mutate_raw "an exact duplicate chain_id key" \
  '!done && /^  "chain_id": / { print "  \"chain_id\": \"twilight-other-1\","; done=1 } { print }' \
  shape.unique_keys
mutate_raw "an exact duplicate authority key inside coreslot params" \
  '!done && /^ *"authority": / { l=$0; sub(/"authority": ".*"/, "\"authority\": \"'"$ATT"'\"", l); print l; done=1 } { print }' \
  shape.unique_keys
# Inside an ARRAY element: the first payout_address in the text belongs to
# slots[0]. A duplicate check that only walked object-under-object paths would
# miss it, and a payout address is where the value goes.
mutate_raw "an exact duplicate payout_address inside slots[0]" \
  '!done && /^ *"payout_address": / { l=$0; sub(/"payout_address": ".*"/, "\"payout_address\": \"'"$ATT"'\"", l); print l; done=1 } { print }' \
  shape.unique_keys

# ---- the InitChain dry-run -------------------------------------------------------------------
#
# Runs the node, so it is kept to the two cases that prove it: the baseline must
# come through it, and a genesis every static validator accepts must not. The
# static module-account check fires on this mutant too; the point is that the
# CHAIN refuses it, which is also what makes the hard-coded module address above
# a checked value rather than an assumed one.
echo
echo "==> InitChain dry-run (on by default)"
CHECKER_FLAGS=""
if run_checker "$GOOD"; then
  if sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -q "PASS  \[native.initchain\]"; then
    pass "the baseline genesis completes InitChain"
  else
    fail "the baseline genesis completes InitChain" "the checker passed without running the dry-run"
  fi
else
  fail "the baseline genesis completes InitChain" "$(sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -A2 'FAIL' | head -6)"
fi
mutate "a module-account settlement address panics InitChain" \
  ".app_state.coreslot.slots[1].settlement_address=\"$REWARDS_MODULE_ADDR\"" \
  native.initchain
if sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -A1 "FAIL  \[native.initchain\]" | grep -q "module account: $REWARDS_MODULE_ADDR"; then
  pass "  and the chain names the same module account the checker does"
else
  fail "  and the chain names the same module account the checker does" \
    "the InitChain refusal did not name $REWARDS_MODULE_ADDR as a module account"
fi
CHECKER_FLAGS="--no-initchain"

rc=0
required_env ""
env "${REQ_ENV[@]}" "$CHECKER" "$GOOD" --initchain >"$WORK/out" 2>&1 || rc=$?
if (( rc == 2 )) && grep -q -- "--initchain needs --bin" "$WORK/out"; then
  pass "--initchain without --bin is a usage error"
else
  fail "--initchain without --bin is a usage error" "exit $rc: $(head -2 "$WORK/out")"
fi

# ---- the probe cannot outlive the checker -------------------------------------------------
#
# The dry-run starts a real process, so the checker owns its lifetime. Two ways it
# used not to: a probe that ignored SIGTERM blocked the checker's `wait` past its
# own 120s cap, and a checker killed with SIGKILL — which no trap can see — left
# the probe running forever with its home on disk. A stub stands in for
# `twilightd start` (everything else goes to the real binary) and ignores
# SIGTERM, INT and HUP, so only SIGKILL can stop it.
echo
echo "==> the InitChain probe cannot outlive the checker"
STUB="$WORK/stub-twilightd"
cat >"$STUB" <<'STUBEOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "start" ]]; then
  echo $$ >"$STUB_PIDFILE"
  trap '' TERM INT HUP
  if [[ "$STUB_MODE" == "handshake" ]]; then
    echo "INF InitChain chainID=stub"
    echo "INF Completed ABCI Handshake - stub"
  fi
  while :; do sleep 1; done
fi
exec "$STUB_REAL_BIN" "$@"
STUBEOF
chmod +x "$STUB"
STUB_TMP="$WORK/stub-tmp"
mkdir -p "$STUB_TMP"
alive() { kill -0 "$1" 2>/dev/null; }
leftovers() { find "$STUB_TMP" -mindepth 1 -maxdepth 1 -name 'check-genesis.*' | head -1; }

rm -f "$WORK/stub.pid"
required_env ""
t0="$(date +%s)"; rc=0
env "${REQ_ENV[@]}" STUB_PIDFILE="$WORK/stub.pid" STUB_MODE=handshake STUB_REAL_BIN="$BIN" TMPDIR="$STUB_TMP" \
  "$CHECKER" "$GOOD" --bin "$STUB" >"$WORK/out" 2>&1 || rc=$?
t1="$(date +%s)"
probe="$(cat "$WORK/stub.pid" 2>/dev/null || true)"
if [[ -z "$probe" ]]; then
  fail "a probe that ignores SIGTERM is killed, not waited on" "the stub probe never started (exit $rc)"
elif alive "$probe"; then
  kill -KILL "$probe" 2>/dev/null || true
  fail "a probe that ignores SIGTERM is killed, not waited on" "the probe was still running after the checker exited"
elif (( t1 - t0 > 30 )); then
  fail "a probe that ignores SIGTERM is killed, not waited on" "the checker took $((t1 - t0))s — it waited on the probe"
elif ! sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" | grep -q "PASS  \[native.initchain\]"; then
  fail "a probe that ignores SIGTERM is killed, not waited on" "the dry-run did not complete: $(tail -2 "$WORK/out")"
elif [[ -n "$(leftovers)" ]]; then
  fail "a probe that ignores SIGTERM is killed, not waited on" "the probe home was left behind: $(leftovers)"
else
  pass "a probe that ignores SIGTERM is killed, not waited on ($((t1 - t0))s)"
fi

# SIGKILL the checker while the probe is still waiting for a handshake.
rm -f "$WORK/stub.pid"
env "${REQ_ENV[@]}" STUB_PIDFILE="$WORK/stub.pid" STUB_MODE=hang STUB_REAL_BIN="$BIN" TMPDIR="$STUB_TMP" \
  "$CHECKER" "$GOOD" --bin "$STUB" >"$WORK/out" 2>&1 &
checker_pid=$!
n=0
while [[ ! -s "$WORK/stub.pid" ]] && (( n < 300 )); do sleep 0.2; n=$((n + 1)); done
probe="$(cat "$WORK/stub.pid" 2>/dev/null || true)"
kill -KILL "$checker_pid" 2>/dev/null || true
wait "$checker_pid" 2>/dev/null || true
if [[ -z "$probe" ]]; then
  fail "a checker killed with SIGKILL does not orphan its probe" "the stub probe never started"
else
  # The watchdog polls once a second and allows the probe a five-second grace.
  n=0
  while { alive "$probe" || [[ -n "$(leftovers)" ]]; } && (( n < 75 )); do sleep 0.2; n=$((n + 1)); done
  if alive "$probe"; then
    kill -KILL "$probe" 2>/dev/null || true
    fail "a checker killed with SIGKILL does not orphan its probe" "the probe was still running 15s later"
  elif [[ -n "$(leftovers)" ]]; then
    fail "a checker killed with SIGKILL does not orphan its probe" "the probe home was left behind: $(leftovers)"
  else
    pass "a checker killed with SIGKILL does not orphan its probe (stopped, home deleted)"
  fi
fi

# ---- the checker must refuse to guess ------------------------------------------------------
#
# A decision it invented is a decision nobody made, so an unset required input has
# to abort rather than default.
echo
echo "==> required decisions must abort, not default"
# Every required input, each omitted in turn while ALL the others are supplied
# with the values the baseline satisfies.
#
# Both halves of that matter, and an earlier version of this loop had neither. It
# omitted GC_DISTRIBUTION_METHOD from every invocation, so each run aborted on
# THAT variable before reaching the one under test, and it asserted only a
# non-zero exit. A mutant defaulting GC_MAX_GAS, or GC_DISTRIBUTION_METHOD,
# passed the whole suite. So the abort must now name the variable under test —
# bash's `${VAR:?}` reports "line N: VAR: ..." — and with the other inputs
# correct, a checker that defaulted the omitted one would instead run to a pass.
#
# GC_MAX_GAS matters most here. It has no shipped default — `twilightd init` writes
# -1 — so a default would be this script inventing a ratification decision that
# #160, #107 and #167 all say has not been made. A run passing because the caller
# forgot the variable is indistinguishable from one passing because the value was
# ratified, which is the exact confusion this tool exists to prevent.
for var in $REQUIRED_VARS; do
  rc=0
  required_env "$var"
  env -u "$var" "${REQ_ENV[@]}" "$CHECKER" "$GOOD" --bin "$BIN" >"$WORK/out" 2>&1 || rc=$?
  if (( rc == 0 )); then
    fail "unset $var aborts" "checker ran anyway and exited 0"
  elif grep -Eq "line [0-9]+: ${var}: " "$WORK/out"; then
    pass "unset $var aborts, naming $var"
  else
    fail "unset $var aborts, naming $var" "exit $rc, but not the refusal for $var: $(head -2 "$WORK/out")"
  fi
done

# Running without the chain's own validator must not be reported as a clean pass.
# Every input is supplied and the genesis is the passing baseline, so the ONLY
# thing that can make this exit non-zero is the missing --bin: every check that
# runs must pass, and the run must end in the refusal itself.
rc=0
required_env ""
env "${REQ_ENV[@]}" "$CHECKER" "$GOOD" >"$WORK/out" 2>&1 || rc=$?
sed -e 's/\x1b\[[0-9;]*m//g' "$WORK/out" >"$WORK/out.plain"
if (( rc == 0 )); then
  fail "omitting --bin is not a clean pass" "checker exited 0 without running twilightd validate"
elif grep -q "FAIL  \[" "$WORK/out.plain"; then
  fail "omitting --bin is not a clean pass" "the run failed a check, so it proves nothing about --bin: $(grep 'FAIL  \[' "$WORK/out.plain" | head -3)"
elif ! grep -q "summary  [0-9]* passed, 0 failed" "$WORK/out.plain"; then
  fail "omitting --bin is not a clean pass" "the run never reached its summary (exit $rc): $(tail -2 "$WORK/out.plain")"
elif tail -1 "$WORK/out.plain" | grep -q "re-run with --bin"; then
  pass "omitting --bin is not a clean pass (every check passed, and it still refused)"
else
  fail "omitting --bin is not a clean pass" "exit $rc without the --bin refusal: $(tail -1 "$WORK/out.plain")"
fi

# ---- the coverage contract: declared == exercised == targeted -----------------------------
#
# Three sets, compared exactly. Each catches something the others cannot:
#
#   DECLARED    the static list in check-genesis.sh, via --list-checks.
#   EXERCISED   ids the checker actually emitted across every run above.
#   TARGETED    ids named by a fault case here.
#
#   declared \ exercised  a check that can no longer fire — UNREACHABLE. A
#                         coverage mechanism derived only from what ran cannot
#                         see this at all, which is why declared is static.
#   exercised \ declared  an id emitted but not declared (the checker also
#                         aborts on this itself).
#   declared \ targeted   a check nobody wrote a fault for.
#   targeted \ declared   a fault case naming a check that no longer exists.
echo
echo "==> coverage contract: declared == exercised == targeted"
"$CHECKER" --list-checks | LC_ALL=C sort -u >"$WORK/declared"
LC_ALL=C sort -u "$LABELS" >"$WORK/exercised"
LC_ALL=C sort -u "$WANTS"  >"$WORK/targeted"

report_diff() { # report_diff <label> <only-in-A-file> <A> <B>
  local n; n="$(grep -c . "$2" || true)"
  if [[ "$n" == "0" ]]; then return 0; fi
  printf '  \033[31mBAD\033[0m   %s (%s):\n' "$1" "$n"
  sed 's/^/          /' "$2"
  return 1
}
COVER_OK=1
comm -23 "$WORK/declared"  "$WORK/exercised" >"$WORK/d_not_e"
comm -13 "$WORK/declared"  "$WORK/exercised" >"$WORK/e_not_d"
comm -23 "$WORK/declared"  "$WORK/targeted"  >"$WORK/d_not_t"
comm -13 "$WORK/declared"  "$WORK/targeted"  >"$WORK/t_not_d"
report_diff "declared but never exercised — UNREACHABLE check" "$WORK/d_not_e" || COVER_OK=0
report_diff "emitted but not declared" "$WORK/e_not_d" || COVER_OK=0
report_diff "declared but no fault case targets it" "$WORK/d_not_t" || COVER_OK=0
report_diff "fault case targets a check that does not exist" "$WORK/t_not_d" || COVER_OK=0
if (( COVER_OK )); then
  pass "all $(grep -c . "$WORK/declared") declared checks are exercised and targeted"
else
  CASES=$((CASES+1)); FAILED=$((FAILED+1))
fi

# ---- summary ---------------------------------------------------------------------------------
echo
printf '\033[1mcheck-genesis-faults\033[0m  %d cases, %d failed\n' "$CASES" "$FAILED"
if (( FAILED > 0 )); then
  printf '\033[31mthe verifier has dead checks\033[0m\n'
  exit 1
fi
printf '\033[32mevery check fires on its own fault\033[0m\n'
