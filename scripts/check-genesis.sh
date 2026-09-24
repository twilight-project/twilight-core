#!/usr/bin/env bash
#
# check-genesis.sh — verify a genesis file against the launch decisions before it
# is distributed to operators.
#
# This exists because most of what matters in genesis has NO tooling behind it.
# `twilightd init` writes defaults, `coreslot-genesis` writes authorities and
# slots, and everything else — every rewards parameter, every mining settlement
# parameter, every coreslot parameter, and block.max_gas — is a hand edit of the
# JSON. A hand edit is exactly the thing that needs checking back.
#
# THREE LAYERS, and they are deliberately separate:
#
#   1. NATIVE      the chain's own validators, which are authoritative
#   2. INVARIANT   rules that are true of any correct fresh genesis
#   3. DECISION    the values THIS launch chose, supplied by the caller
#
# A decision cannot be inferred from the file — checking a file against itself
# proves nothing. Required decisions must be supplied or this aborts. It never
# guesses, and it never treats "the default was left in place" as a decision.
#
# Usage:
#   GC_CHAIN_ID=twilight-testnet-1 GC_ACTIVE_SLOTS=2 \
#   GC_MAX_GAS=<ratified> GC_MIN_ACTIVE_SLOTS=2 \
#   GC_DISTRIBUTION_METHOD=DISTRIBUTION_METHOD_UNIFORM_ACTIVE_BLOCKS \
#   GC_AUTHORITY=twilight1... GC_EMERGENCY_AUTHORITY=twilight1... \
#     scripts/check-genesis.sh path/to/genesis.json --bin build/twilightd [--initchain]
#
# --bin is required for a passing verdict. Without it the chain's own validators,
# `coreslot-genesis validate`, and the module-account checks (which need the
# binary to derive bech32 addresses) cannot run, and the run exits non-zero even
# when everything it could check passed.
#
# --initchain additionally starts the binary against a throwaway home holding
# this genesis and waits for the ABCI handshake to complete, which is the point
# InitChain — every module's InitGenesis — has run. The other checks predict what
# InitGenesis will refuse; this is the only one that asks it. It costs about a
# second on a small genesis (bounded at 120s), binds only 127.0.0.1 on ephemeral
# ports, never proposes or signs (the probe's key is not in the validator set),
# and deletes the home afterwards.
#
set -euo pipefail

if [[ "${1:-}" == "--list-checks" ]]; then LIST_ONLY=1; else LIST_ONLY=0; fi
GENESIS="${1:-}"
BIN=""
INITCHAIN=0
shift || true
while (( $# )); do
  case "$1" in
    --bin)
      [[ $# -ge 2 ]] || { echo "--bin needs a path to the twilightd binary" >&2; exit 2; }
      BIN="$2"; shift 2 ;;
    --initchain) INITCHAIN=1; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

(( LIST_ONLY )) || [[ -n "$GENESIS" ]] || { echo "usage: $0 <genesis.json> --bin <twilightd> [--initchain]" >&2; exit 2; }
(( LIST_ONLY )) || (( ! INITCHAIN )) || [[ -n "$BIN" ]] || { echo "--initchain needs --bin: it starts that binary" >&2; exit 2; }
(( LIST_ONLY )) || [[ -f "$GENESIS" ]] || { echo "no such genesis file: $GENESIS" >&2; exit 2; }
(( LIST_ONLY )) || command -v jq >/dev/null || { echo "jq is required" >&2; exit 2; }

# THE DECLARED CHECK SET.
#
# Every check this script can emit, listed STATICALLY. This is the contract the
# fault suite holds the script to, and it is static precisely so that a check
# which has become UNREACHABLE still appears here: a coverage mechanism derived
# only from what ran cannot tell "no fault triggers this" from "this can no
# longer fire at all", and the second is the dangerous one.
#
# Three sets must be equal, and check-genesis-faults.sh asserts it:
#
#   declared (this list)  ==  exercised (emitted across all runs)  ==  targeted (fault cases)
#
# Adding a check without adding it here is a hard error at runtime. Adding it here
# without a fault case fails the suite. Making one unreachable fails the suite.
CHECK_IDS=(
  native.validate native.coreslot_genesis native.initchain
  fresh.current_epoch fresh.cumulative_emitted fresh.carry_forward_remainder
  fresh.open_reward_blocks fresh.entitlement_liability fresh.has_pending_params
  fresh.paused fresh.pause_pending
  fresh.epoch_versions_count fresh.reward_versions_count fresh.settlement_versions_count
  fresh.sched_epoch_empty fresh.sched_reward_empty fresh.sched_settlement_empty
  fresh.sched_distmode_empty fresh.sched_selection_empty
  fresh.finalized_epochs_empty fresh.slot_entitlements_empty fresh.settlements_empty
  fresh.pending_authority_transfers_empty fresh.reserved_consensus_addresses_empty
  fresh.pending_key_rotations_empty
  mirror.epoch_length mirror.subsidy mirror.emission_share mirror.treasury_address
  mirror.epoch_anchor
  snapshot.epoch_length_blocks snapshot.initial_block_subsidy snapshot.treasury_address
  snapshot.emission_treasury_share_bps snapshot.distribution_method snapshot.halving_mode
  snapshot.remainder_policy snapshot.fee_denom snapshot.fee_treasury_share_bps
  bound.epoch_length bound.emission_share bound.recipients_per_chunk
  bound.chunks_per_settlement bound.min_payout bound.settlement_window
  bound.max_active_slots bound.selection_cooldown
  trap.treasury_address_for_share trap.slot_status_known trap.active_within_bounds
  trap.native_denom trap.fee_denom trap.bank_denoms_native trap.supply_within_max
  trap.max_gas_finite
  trap.payout_not_module_account trap.settlement_not_module_account
  trap.treasury_not_module_account
  decision.chain_id decision.max_gas decision.active_slots decision.min_active_slots
  decision.epoch_length decision.max_supply decision.subsidy decision.distribution_method
  decision.emission_share decision.treasury_address decision.allow_self_registration
  decision.target_block_time decision.authority decision.emergency_authority
  decision.authority_shape decision.emergency_authority_shape decision.authorities_distinct
)

# Every module account the app declares (app/config.go moduleAccountPermissions,
# via app.ModuleAccountNames()). None of them may receive protocol value: the
# chain's economic-address rule (internal/economicaddress) refuses each one as a
# payout, settlement or treasury address at InitGenesis, which is a panic at
# start-up rather than a validation error. The bank module's blocked set is this
# same list — bank is wired with no blocked-accounts override — so this also
# covers "bank-blocked".
#
# This is a copy of an app-side list, which is only acceptable because it cannot
# drift silently: app/check_genesis_script_test.go parses this line and fails
# when it differs from app.ModuleAccountNames(), or when the bank blocked set
# contains anything it does not cover.
MODULE_ACCOUNT_NAMES=(fee_collector coreslot-authority coreslot-emergency rewards rewards_fee_pool)

# LIST_ONLY is captured before the argument shift above; re-testing $1 here would
# read an already-consumed argument.
if (( LIST_ONLY )); then
  printf '%s\n' "${CHECK_IDS[@]}"
  exit 0
fi

# An id emitted but not declared is a bug in THIS script, not in the genesis, so
# it aborts rather than being reported as a check result.
declared_id() {
  local want="$1" have
  for have in "${CHECK_IDS[@]}"; do [[ "$have" == "$want" ]] && return 0; done
  return 1
}


# ---- decisions the caller must state ------------------------------------------------------
#
# THE RULE FOR WHAT MAY BE DEFAULTED, stated so it can be argued with:
#
#   Default only to a value the CODEBASE ITSELF establishes. Require everything
#   else.
#
# Defaulting to "you did not change what the binary ships" is verifiable from
# source, and a reader can check it. Defaulting to a number chosen in a discussion
# is this script inventing a launch decision — which is exactly what it exists to
# stop, and the failure would be silent: a run that passes because the caller
# omitted a variable reads identically to one that passes because the value was
# ratified.
#
# Required, no default, because the codebase establishes no value for them:
#
#   GC_CHAIN_ID        no default exists anywhere.
#   GC_ACTIVE_SLOTS    a genesis cannot state how many slots were INTENDED.
#   GC_MAX_GAS         `twilightd init` writes -1 (cometbft types/params.go).
#                      Any finite value is a ratification decision, and as of
#                      #160, #107 and #167 no value is ratified. This script must
#                      never be the thing that supplies one.
#   GC_MIN_ACTIVE_SLOTS  the shipped default is 1 (coreslot validation.go);
#                      anything else is a deployment choice about liveness.
#   GC_DISTRIBUTION_METHOD  the shipped default IS the likely answer, but the value
#                      is FROZEN at genesis — uniform can never become weighted
#                      without an upgrade — so accepting it must be a statement
#                      rather than a silence.
#   GC_AUTHORITY       the primary authority admits validators, sets parameters
#   GC_EMERGENCY_AUTHORITY  and schedules upgrades; the emergency authority can
#                      pause rewards. Both are whoever the file says they are, so
#                      checking only that they are well formed and distinct
#                      accepts a genesis in which both were replaced by keys
#                      nobody agreed to. There is no default: `twilightd init`
#                      writes the coreslot-authority and coreslot-emergency
#                      MODULE ACCOUNT addresses, which no key controls.
: "${GC_CHAIN_ID:?set GC_CHAIN_ID to the chain-id this launch decided on}"
: "${GC_ACTIVE_SLOTS:?set GC_ACTIVE_SLOTS to the number of slots that must be ACTIVE at genesis}"
: "${GC_MAX_GAS:?set GC_MAX_GAS to the ratified block max_gas for this launch — twilightd init writes -1 and no value is ratified in-repo}"
: "${GC_MIN_ACTIVE_SLOTS:?set GC_MIN_ACTIVE_SLOTS to the active-slot floor this launch decided on — the shipped default is 1}"
: "${GC_DISTRIBUTION_METHOD:?set GC_DISTRIBUTION_METHOD — it is frozen at genesis, so it must be stated even when the shipped default is the answer}"
: "${GC_AUTHORITY:?set GC_AUTHORITY to the primary authority address this launch decided on}"
: "${GC_EMERGENCY_AUTHORITY:?set GC_EMERGENCY_AUTHORITY to the emergency authority address this launch decided on}"

# SHIPPED-DEFAULT EXPECTATIONS — not caller launch decisions.
#
# Each asserts "this genesis still carries what the binary ships", and each cites
# the constant it mirrors so the claim is checkable rather than asserted. A PASS
# here means the shipped value was not changed. It does NOT mean anyone decided
# it. Anything a deployment actually chooses belongs in the required block above.
GC_EPOCH_LENGTH_BLOCKS="${GC_EPOCH_LENGTH_BLOCKS:-360}"                                  # rewards DefaultEpochLengthBlocks
GC_MAX_SUPPLY="${GC_MAX_SUPPLY:-21000000000000}"                                         # rewards DefaultMaxSupply
GC_INITIAL_BLOCK_SUBSIDY="${GC_INITIAL_BLOCK_SUBSIDY:-416190}"                           # rewards DefaultInitialBlockSubsidy
GC_TREASURY_ADDRESS="${GC_TREASURY_ADDRESS:-}"                                           # rewards DefaultParams (empty)
GC_EMISSION_TREASURY_SHARE_BPS="${GC_EMISSION_TREASURY_SHARE_BPS:-0}"                    # rewards DefaultParams
GC_NATIVE_DENOM="${GC_NATIVE_DENOM:-utwlt}"                                              # appparams.NativeBaseDenom
# Not a genesis value. Used only to project the emission schedule, because the
# schedule depends on real block time and genesis cannot record it.
GC_BLOCK_TIME_SECONDS="${GC_BLOCK_TIME_SECONDS:-5}"

PASS=0; FAIL=0; SECTION=""

section() { SECTION="$1"; printf '\n\033[1m%s\033[0m\n' "$1"; }
# Results carry a STABLE ID. The label is prose and may be reworded; the id is the
# contract the fault suite matches on, exactly — substring matching on prose let a
# short pattern be satisfied by a different check's output.
ok() { # ok <id> <label>
  declared_id "$1" || { printf 'undeclared check id: %s\n' "$1" >&2; exit 3; }
  PASS=$((PASS+1)); printf '  \033[32mPASS\033[0m  [%s] %s\n' "$1" "$2"
  return 0
}
bad() { # bad <id> <label> [detail]
  declared_id "$1" || { printf 'undeclared check id: %s\n' "$1" >&2; exit 3; }
  FAIL=$((FAIL+1)); printf '  \033[31mFAIL\033[0m  [%s] %s\n' "$1" "$2"
  # An `x && printf` tail would return 1 whenever the detail is empty, and every
  # call site is the tail of a compound command, so `set -e` would abort the run
  # with no message at all. Explicit return.
  if [[ -n "${3:-}" ]]; then printf '        %s\n' "$3"; fi
  return 0
}
note() { printf '  \033[36mnote\033[0m  %s\n' "$1"; }

# jq read that fails closed: a missing path becomes __MISSING__ rather than an
# empty string that would silently compare equal to an empty expectation.
#
# Deliberately NOT `jq -e`. That flag sets exit status 1 when the OUTPUT VALUE is
# false or null, so a legitimate `false` — allow_self_registration, every pause
# flag — reads as missing. `//` is wrong for the same reason: it fires on false
# as well as null. So the status is used only for evaluation errors, and a JSON
# null is mapped explicitly.
j() {
  local out rc
  out="$(jq -r "$1" "$GENESIS" 2>/dev/null)"; rc=$?
  if (( rc != 0 )) || [[ "$out" == "null" ]]; then
    echo "__MISSING__"
    return 0
  fi
  printf '%s' "$out"
}

# A value that could not be read is never equal to anything and never numeric.
# [[ -ge ]] evaluates its operands ARITHMETICALLY, so a non-numeric string is
# treated as a variable name and `set -u` kills the run mid-way — silently
# skipping every check below it, including the max_gas check this exists for.
is_num() { [[ "$1" =~ ^-?[0-9]+$ ]]; }

# ARBITRARY-PRECISION decimal arithmetic for token amounts. Amounts are JSON
# strings precisely because they exceed what a double — jq's only number type —
# holds exactly, and bash arithmetic is signed 64-bit and wraps silently. A
# supply check that is off by one at 2^53 is not a supply check. Callers pass
# strings already validated as ^[0-9]+$.
dec_norm() { # strip leading zeros; sets DEC_OUT
  DEC_OUT="$1"
  while [[ ${#DEC_OUT} -gt 1 && "${DEC_OUT:0:1}" == "0" ]]; do DEC_OUT="${DEC_OUT:1}"; done
  return 0
}
dec_add() { # dec_add <a> <b>; sets DEC_OUT = a + b
  local a="$1" b="$2" out="" carry=0 x y s
  # 15-digit limbs: two limbs plus a carry stay far below 2^63.
  while [[ -n "$a" || -n "$b" ]]; do
    if (( ${#a} > 15 )); then x="${a:${#a}-15}"; a="${a:0:${#a}-15}"; else x="${a:-0}"; a=""; fi
    if (( ${#b} > 15 )); then y="${b:${#b}-15}"; b="${b:0:${#b}-15}"; else y="${b:-0}"; b=""; fi
    s=$(( 10#$x + 10#$y + carry ))
    carry=$(( s / 1000000000000000 ))
    out="$(printf '%015d' $(( s % 1000000000000000 )))$out"
  done
  (( carry == 0 )) || out="$carry$out"
  dec_norm "$out"
}
dec_le() { # dec_le <a> <b>; true when a <= b
  local a b i x y
  dec_norm "$1"; a="$DEC_OUT"
  dec_norm "$2"; b="$DEC_OUT"
  # Explicit returns throughout: a bare failing (( )) as a function's last word
  # is an exit under `set -e` whenever the caller is not a conditional.
  if (( ${#a} < ${#b} )); then return 0; fi
  if (( ${#a} > ${#b} )); then return 1; fi
  # Equal lengths: compare digit by digit rather than with [[ < ]], whose
  # ordering is locale collation, not numeric.
  for (( i = 0; i < ${#a}; i++ )); do
    x="${a:i:1}"; y="${b:i:1}"
    if (( x < y )); then return 0; fi
    if (( x > y )); then return 1; fi
  done
  return 0
}

eq() { # eq <id> <label> <expected> <actual>
  if [[ "$4" == "__MISSING__" ]]; then
    bad "$1" "$2" "not present in the genesis file (expected: $3)"
  elif [[ "$3" == "$4" ]]; then ok "$1" "$2 = $4"
  else bad "$1" "$2" "expected: $3
        actual:   $4"; fi
  return 0
}

# Both sides are read from the file, so two ABSENT values must not agree. A
# mirror check that passes because neither copy exists is not doing its job.
mirror() { # mirror <id> <label> <a> <b>
  if [[ "$3" == "__MISSING__" || "$4" == "__MISSING__" ]]; then
    bad "$1" "$2" "one or both copies are absent: '$3' vs '$4'"
  elif [[ "$3" == "$4" ]]; then ok "$1" "$2 = $4"
  else bad "$1" "$2" "the two copies disagree:
        params-side:  $3
        canonical:    $4"; fi
  return 0
}

# Compared against `true` EXPLICITLY. `jq -e` exits 0 for any output that is not
# false or null — including 0, "", [] and {} — so a bound check accidentally
# reduced to a value-producing expression would pass for every input.
truthy() { # truthy <id> <label> <jq-boolean-expression> <detail-on-fail>
  if jq -e "($3) == true" "$GENESIS" >/dev/null 2>&1; then ok "$1" "$2"
  else bad "$1" "$2" "${4:-}"; fi
  return 0
}

printf '\033[1mcheck-genesis\033[0m  %s\n' "$GENESIS"

# ---- 1. native validation ------------------------------------------------------------------
#
# The chain's own answer. Everything below is a cross-check that produces a better
# message; nothing below overrides this.
section "1. native validation (authoritative)"
WORKDIR=""; IC_PID=""
cleanup() {
  if [[ -n "$IC_PID" ]]; then kill "$IC_PID" 2>/dev/null || true; wait "$IC_PID" 2>/dev/null || true; fi
  if [[ -n "$WORKDIR" ]]; then rm -rf "$WORKDIR"; fi
  return 0
}
trap cleanup EXIT
trap 'exit 130' INT TERM
if [[ -n "$BIN" ]]; then
  [[ -x "$BIN" ]] || { echo "binary not executable: $BIN" >&2; exit 2; }
  WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/check-genesis.XXXXXX")"
  VLOG="$WORKDIR/validate.log"
  # Every binary call below that takes a home gets a throwaway one. The CLI's
  # pre-run writes a client.toml into whatever home it resolves, and the default
  # is the operator's real ~/.twilightd.
  CLIHOME="$WORKDIR/cli-home"
  if "$BIN" validate "$GENESIS" >"$VLOG" 2>&1; then
    ok native.validate "twilightd validate"
  else
    bad native.validate "twilightd validate" "$(tail -3 "$VLOG")"
  fi

  # `twilightd validate` is TYPES-level: each module's ValidateGenesis on its own
  # state. It does not see the document around the state, so a genesis whose
  # slots were activated at height 1 passes it with initial_height=5 and then
  # panics at InitChain. The binary's own coreslot-genesis validate reads the
  # document's initial_height and its CometBFT validator list as well. It takes
  # only a home, so it is handed one containing nothing but this file.
  CSHOME="$WORKDIR/coreslot-home"
  mkdir -p "$CSHOME/config"
  cp "$GENESIS" "$CSHOME/config/genesis.json"
  if "$BIN" coreslot-genesis validate --home "$CSHOME" >"$VLOG" 2>&1; then
    ok native.coreslot_genesis "twilightd coreslot-genesis validate"
  else
    # cobra prints the usage block before the error, so the error is the last line.
    bad native.coreslot_genesis "twilightd coreslot-genesis validate" "$(tail -1 "$VLOG")"
  fi

  if (( INITCHAIN )); then
    # InitChain is where each keeper's InitGenesis runs, and it refuses things no
    # ValidateGenesis sees — a module account as a payout, settlement or treasury
    # address is one. So start the real binary on this genesis and wait for the
    # ABCI handshake, which completes only after InitChain has returned. A fresh
    # home always runs InitChain; a panic in it exits the process.
    ICHOME="$WORKDIR/initchain-home"
    ICLOG="$WORKDIR/initchain.log"
    if ! "$BIN" init initchain-probe --chain-id "$GC_CHAIN_ID" --home "$ICHOME" >"$ICLOG" 2>&1; then
      bad native.initchain "InitChain dry-run" "could not create a probe home: $(tail -1 "$ICLOG")"
    else
      cp "$GENESIS" "$ICHOME/config/genesis.json"
      # Loopback only, OS-assigned ports, and every optional server off, so the
      # probe can neither collide with a node on this host nor be reached from
      # outside it. Its generated key is not in the validator set, so it never
      # proposes or signs.
      "$BIN" start --home "$ICHOME" \
        --p2p.laddr tcp://127.0.0.1:0 --rpc.laddr tcp://127.0.0.1:0 --rpc.pprof_laddr "" \
        --p2p.pex=false --p2p.seeds "" --p2p.persistent_peers "" \
        --grpc.enable=false --grpc-web.enable=false --api.enable=false \
        --log_level info --log_no_color >"$ICLOG" 2>&1 &
      IC_PID=$!
      IC_RESULT=timeout
      i=0
      while (( i < 600 )); do # 600 x 0.2s = 120s
        if grep -q "Completed ABCI Handshake" "$ICLOG"; then IC_RESULT=ok; break; fi
        if ! kill -0 "$IC_PID" 2>/dev/null; then IC_RESULT=exited; break; fi
        sleep 0.2
        i=$((i + 1))
      done
      kill "$IC_PID" 2>/dev/null || true
      wait "$IC_PID" 2>/dev/null || true
      IC_PID=""
      # A process that exited just after logging the handshake still completed it.
      if [[ "$IC_RESULT" == "exited" ]] && grep -q "Completed ABCI Handshake" "$ICLOG"; then IC_RESULT=ok; fi
      # The handshake line alone is not proof that InitChain ran — a home with
      # existing state would replay instead — so its own log line must be there too.
      if [[ "$IC_RESULT" == "ok" ]] && ! grep -q "InitChain" "$ICLOG"; then IC_RESULT=no-initchain; fi
      case "$IC_RESULT" in
        ok) ok native.initchain "InitChain dry-run (every module's InitGenesis accepted this genesis)" ;;
        exited)
          # A keeper panic, or an error the handshake returned. Either way the
          # process then prints cobra's usage block, so the tail is not the reason.
          IC_WHY="$(grep -m1 -E '^panic: |error during handshake|^Error: ' "$ICLOG" || true)"
          [[ -n "$IC_WHY" ]] || IC_WHY="$(tail -1 "$ICLOG")"
          bad native.initchain "InitChain dry-run" "the node exited before InitChain completed: $IC_WHY" ;;
        timeout) bad native.initchain "InitChain dry-run" "no ABCI handshake within 120s: $(tail -1 "$ICLOG")" ;;
        *) bad native.initchain "InitChain dry-run" "handshake logged but no InitChain line — this proves nothing about InitGenesis" ;;
      esac
    fi
  else
    note "InitChain was not dry-run. Add --initchain to start the binary on this genesis (about a second)."
  fi
else
  note "no --bin given; the chain's own validators were NOT run and the module-account"
  note "checks cannot derive addresses. Supply --bin build/twilightd."
fi

# ---- 2. fresh-genesis invariants ------------------------------------------------------------
#
# A fresh genesis is not merely a valid one: it must carry no history. These are the
# rules x/rewards and x/mining enforce at InitGenesis, checked here so a violation
# is named rather than surfacing as a start-up failure.
section "2. fresh-genesis invariants"
eq fresh.current_epoch "rewards.state.current_epoch"              "1" "$(j '.app_state.rewards.state.current_epoch')"
eq fresh.cumulative_emitted "rewards.state.cumulative_emitted"         "0" "$(j '.app_state.rewards.state.cumulative_emitted')"
eq fresh.carry_forward_remainder "rewards.state.carry_forward_remainder"    "0" "$(j '.app_state.rewards.state.carry_forward_remainder')"
eq fresh.open_reward_blocks "rewards.open_reward_enabled_blocks"       "0" "$(j '.app_state.rewards.open_reward_enabled_blocks')"
eq fresh.entitlement_liability "rewards.outstanding_entitlement_liability" "0" "$(j '.app_state.rewards.outstanding_entitlement_liability')"
eq fresh.has_pending_params "rewards.has_pending_params"           "false" "$(j '.app_state.rewards.has_pending_params')"
eq fresh.paused "rewards.pause_state.current_paused"   "false" "$(j '.app_state.rewards.pause_state.current_paused')"
eq fresh.pause_pending "rewards.pause_state.has_pending"      "false" "$(j '.app_state.rewards.pause_state.has_pending')"

eq fresh.epoch_versions_count "rewards.epoch_config_versions count"      "1" "$(j '.app_state.rewards.epoch_config_versions | length')"
eq fresh.reward_versions_count "rewards.reward_config_versions count"     "1" "$(j '.app_state.rewards.reward_config_versions | length')"
eq fresh.settlement_versions_count "mining.settlement_params_versions count"  "1" "$(j '.app_state.mining.settlement_params_versions | length')"

for pair in \
  "fresh.sched_epoch_empty:rewards.scheduled_epoch_configs" \
  "fresh.sched_reward_empty:rewards.scheduled_reward_configs" \
  "fresh.sched_settlement_empty:mining.scheduled_settlement_params" \
  "fresh.sched_distmode_empty:mining.scheduled_distribution_modes" \
  "fresh.sched_selection_empty:mining.scheduled_selection_params"; do
  cid="${pair%%:*}"; cpath="${pair#*:}"
  eq "$cid" "$cpath is empty" "0" "$(j ".app_state.${cpath} | length")"
done
eq fresh.finalized_epochs_empty "rewards.finalized_epochs is empty" "0" "$(j '.app_state.rewards.finalized_epochs | length')"
eq fresh.slot_entitlements_empty "rewards.slot_entitlements is empty" "0" "$(j '.app_state.rewards.slot_entitlements | length')"
eq fresh.settlements_empty "mining.settlements is empty"        "0" "$(j '.app_state.mining.settlements | length')"

# In-flight coreslot operations. The native ValidateGenesis ACCEPTS each of these,
# deliberately: an export must carry them or a restart would silently drop a
# rotation mid-flight. In a genesis being cut for a new chain they are a hand-over
# nobody signed. A pending authority nomination was reproduced end to end — the
# nominee's accept-authority succeeded after launch and took the primary role
# without the incumbent ever signing. A pending key rotation swaps a validator's
# consensus key the same way, and a reserved consensus address locks a key out of
# the set before anyone has used it.
eq fresh.pending_authority_transfers_empty "coreslot.pending_authority_transfers is empty" \
   "0" "$(j '.app_state.coreslot.pending_authority_transfers | length')"
eq fresh.reserved_consensus_addresses_empty "coreslot.reserved_consensus_addresses is empty" \
   "0" "$(j '.app_state.coreslot.reserved_consensus_addresses | length')"
eq fresh.pending_key_rotations_empty "coreslot.pending_key_rotations is empty" \
   "0" "$(j '.app_state.coreslot.pending_key_rotations | length')"

# ---- 3. mirror consistency -------------------------------------------------------------------
#
# THE hand-edit trap. Three economic values and the epoch length each live in TWO
# places: the params document, and the canonical version-1 history entry that
# actually governs. Editing one and not the other is the single most likely
# mistake in a hand-cut genesis, and the version entry is the one that wins.
section "3. mirror consistency (params vs canonical version 1 vs the snapshot)"
mirror mirror.epoch_length "epoch_length_blocks mirrors" \
   "$(j '.app_state.rewards.params.epoch_length_blocks')" \
   "$(j '.app_state.rewards.epoch_config_versions[0].epoch_length_blocks')"
mirror mirror.subsidy "initial_block_subsidy mirrors" \
   "$(j '.app_state.rewards.params.initial_block_subsidy')" \
   "$(j '.app_state.rewards.reward_config_versions[0].initial_block_subsidy')"
mirror mirror.emission_share "emission_treasury_share_bps mirrors" \
   "$(j '.app_state.rewards.params.emission_treasury_share_bps')" \
   "$(j '.app_state.rewards.reward_config_versions[0].emission_treasury_share_bps')"
mirror mirror.treasury_address "treasury_address mirrors" \
   "$(j '.app_state.rewards.params.treasury_address')" \
   "$(j '.app_state.rewards.reward_config_versions[0].treasury_address')"
mirror mirror.epoch_anchor "epoch anchor starts at initial_height" \
   "$(j '.initial_height')" \
   "$(j '.app_state.rewards.epoch_config_versions[0].effective_start_height')"

# THE THIRD COPY. current_epoch_config (EpochConfigSnapshot, params.proto:78) is a
# full snapshot carrying its own epoch_length_blocks, subsidy, treasury address and
# share, distribution method, halving mode and remainder policy. An operator who
# edits the two above and stops has a genesis the chain still refuses, and the
# refusal names none of them. Every field it duplicates is checked here.
for f in epoch_length_blocks initial_block_subsidy treasury_address \
         emission_treasury_share_bps distribution_method halving_mode \
         remainder_policy fee_denom fee_treasury_share_bps; do
  mirror "snapshot.$f" "current_epoch_config.$f mirrors params" \
     "$(j ".app_state.rewards.params.${f}")" \
     "$(j ".app_state.rewards.current_epoch_config.${f}")"
done

# ---- 4. immutable bounds ---------------------------------------------------------------------
#
# Ratified in app/params/bounds.go. Genesis is the last point these can be chosen;
# afterwards they need an upgrade, so a value outside the interval must be caught
# here rather than at first block.
section "4. immutable bounds (app/params/bounds.go)"
truthy bound.epoch_length "epoch_length_blocks within [360,720]" \
  '(.app_state.rewards.params.epoch_length_blocks|tonumber) as $v | $v >= 360 and $v <= 720' \
  "found $(j '.app_state.rewards.params.epoch_length_blocks')"
truthy bound.emission_share "emission_treasury_share_bps <= 5000" \
  '(.app_state.rewards.params.emission_treasury_share_bps|tonumber) <= 5000' \
  "found $(j '.app_state.rewards.params.emission_treasury_share_bps')"
truthy bound.recipients_per_chunk "max_recipients_per_chunk within [1,32]" \
  '(.app_state.mining.settlement_params_versions[0].max_recipients_per_chunk|tonumber) as $v | $v >= 1 and $v <= 32' \
  "found $(j '.app_state.mining.settlement_params_versions[0].max_recipients_per_chunk')"
truthy bound.chunks_per_settlement "max_chunks_per_settlement within [1,4]" \
  '(.app_state.mining.settlement_params_versions[0].max_chunks_per_settlement|tonumber) as $v | $v >= 1 and $v <= 4' \
  "found $(j '.app_state.mining.settlement_params_versions[0].max_chunks_per_settlement')"
truthy bound.min_payout "min_recipient_payout_amount >= 10000" \
  '(.app_state.mining.settlement_params_versions[0].min_recipient_payout_amount|tonumber) >= 10000' \
  "found $(j '.app_state.mining.settlement_params_versions[0].min_recipient_payout_amount')"
truthy bound.settlement_window "settlement_window_epochs >= 1" \
  '(.app_state.mining.settlement_params_versions[0].settlement_window_epochs|tonumber) >= 1' \
  "found $(j '.app_state.mining.settlement_params_versions[0].settlement_window_epochs')"
truthy bound.max_active_slots "max_active_slots <= 100" \
  '(.app_state.coreslot.params.max_active_slots|tonumber) <= 100' \
  "found $(j '.app_state.coreslot.params.max_active_slots')"
truthy bound.selection_cooldown "selection_policy_update_cooldown_blocks >= 360" \
  '(.app_state.coreslot.params.selection_policy_update_cooldown_blocks|tonumber) >= 360' \
  "found $(j '.app_state.coreslot.params.selection_policy_update_cooldown_blocks')"

# ---- 5. the traps ------------------------------------------------------------------------------
section "5. traps"

# The treasury trap. An empty address is only legal while both shares are zero, and
# BOTH the share and the address are frozen after genesis. Launching at zero with no
# address means no treasury share can ever be introduced without an upgrade.
TREAS_ADDR="$(j '.app_state.rewards.params.treasury_address')"
EMIS_BPS="$(j '.app_state.rewards.params.emission_treasury_share_bps')"
FEE_BPS="$(j '.app_state.rewards.params.fee_treasury_share_bps')"
if [[ "$EMIS_BPS" != "0" || "$FEE_BPS" != "0" ]]; then
  if [[ "$TREAS_ADDR" =~ ^twilight1[0-9a-z]{38,58}$ ]]; then
    ok trap.treasury_address_for_share "treasury address present for a non-zero share"
  else
    bad trap.treasury_address_for_share "treasury address present for a non-zero share" "shares are non-zero but treasury_address is '$TREAS_ADDR'"
  fi
elif [[ -z "$TREAS_ADDR" ]]; then
  note "treasury_address is EMPTY with zero shares. Legal — but the address and the share are"
  note "both frozen at genesis, so no treasury share can be introduced later without an upgrade."
else
  # A note, not a PASS. Nothing is being verified here — the address is legal
  # either way at a zero share — and counting a status report as a passing check
  # both inflates the total and creates a "check" no fault could ever make fire.
  note "treasury_address is set ahead of a zero share, which keeps the option open."
fi

# The restart trap. Genesis requires active >= min_active_slots. UpdateParams does
# NOT check this, so a running chain can be pushed into a state its own export
# cannot restart from. Checked here because this file may itself be an export.
# Status is matched against the real enum (proto SlotStatus). An UNRECOGNISED
# status is a hard failure rather than a slot that quietly counts as inactive:
# guessing the spelling here once produced a zero active count on a genesis that
# was in fact correct, which is the most dangerous possible direction for this
# check to be wrong in.
UNKNOWN_STATUS="$(jq -r '[.app_state.coreslot.slots[]?.status
  | select(. != "SLOT_STATUS_UNSPECIFIED" and . != "SLOT_STATUS_PENDING"
       and . != "SLOT_STATUS_ACTIVE"      and . != "SLOT_STATUS_INACTIVE"
       and . != "SLOT_STATUS_SUSPENDED"   and . != "SLOT_STATUS_REMOVED")]
  | unique | join(",")' "$GENESIS" 2>/dev/null || echo "__ERR__")"
if [[ -n "$UNKNOWN_STATUS" ]]; then
  bad trap.slot_status_known "every slot status is a known SlotStatus value" "unrecognised: $UNKNOWN_STATUS"
else
  ok trap.slot_status_known "every slot status is a known SlotStatus value"
fi
ACTIVE="$(jq -r '[.app_state.coreslot.slots[]? | select(.status=="SLOT_STATUS_ACTIVE")] | length' "$GENESIS" 2>/dev/null || echo 0)"
MINA="$(j '.app_state.coreslot.params.min_active_slots')"
MAXA="$(j '.app_state.coreslot.params.max_active_slots')"
if ! is_num "$ACTIVE" || ! is_num "$MINA" || ! is_num "$MAXA"; then
  bad trap.active_within_bounds "active slot count within [min,max]" \
      "unreadable bound — active=$ACTIVE min=$MINA max=$MAXA (a missing or non-numeric params entry)"
elif (( ACTIVE >= MINA && ACTIVE <= MAXA )); then
  ok trap.active_within_bounds "active slot count within [min,max] ($ACTIVE in [$MINA,$MAXA])"
else
  bad trap.active_within_bounds "active slot count within [min,max]" "active=$ACTIVE min=$MINA max=$MAXA — this genesis will be REFUSED at start-up"
fi

# Invariant 5: utwlt is the only accounting denom; no display denom may leak into
# an amount.
eq trap.native_denom "rewards native_denom" "$GC_NATIVE_DENOM" "$(j '.app_state.rewards.params.native_denom')"
eq trap.fee_denom "rewards fee_denom"    "$GC_NATIVE_DENOM" "$(j '.app_state.rewards.params.fee_denom')"
#
# Stated positively — every bank denom IS the native denom — rather than as a
# denylist of display spellings. A denylist of "twlt" and "TWLT" let "utwtl",
# "Twlt" and "uTWLT" through, and each of those is a token the chain never
# accounts for. A jq failure is reported, never read as "nothing found".
if BAD_DENOMS="$(jq -r --arg d "$GC_NATIVE_DENOM" \
    '[.app_state.bank.supply[]?.denom, .app_state.bank.balances[]?.coins[]?.denom]
     | map(select(. != $d) | tostring) | unique | join(",")' "$GENESIS" 2>/dev/null)"; then
  if [[ -z "$BAD_DENOMS" ]]; then
    ok trap.bank_denoms_native "every bank supply and balance denom is exactly $GC_NATIVE_DENOM"
  else
    bad trap.bank_denoms_native "every bank supply and balance denom is exactly $GC_NATIVE_DENOM" "found: $BAD_DENOMS"
  fi
else
  bad trap.bank_denoms_native "every bank supply and balance denom is exactly $GC_NATIVE_DENOM" "bank denoms could not be read"
fi

# The rewards supply-cap invariant: the native supply never exceeds max_supply.
# Emission is capped against it, but nothing at genesis is — a starting supply
# above the cap passes every validator and breaks the invariant from block 1.
# Both the stated supply and the sum of balances are checked: bank validates that
# they agree only when a supply is stated at all, and derives it from the
# balances when it is not. Exact decimal arithmetic throughout (see dec_add).
SUPPLY_WHY=""
MAXSUP_FILE="$(j '.app_state.rewards.params.max_supply')"
[[ "$MAXSUP_FILE" =~ ^[0-9]+$ ]] || SUPPLY_WHY="max_supply is unreadable: '$MAXSUP_FILE'"
if [[ -z "$SUPPLY_WHY" ]]; then
  if STATED="$(jq -r --arg d "$GC_NATIVE_DENOM" \
      '.app_state.bank.supply[]? | select(.denom == $d) | .amount | tostring' "$GENESIS" 2>/dev/null)" \
     && BAL_AMTS="$(jq -r --arg d "$GC_NATIVE_DENOM" \
      '.app_state.bank.balances[]?.coins[]? | select(.denom == $d) | .amount | tostring' "$GENESIS" 2>/dev/null)"; then
    # Summed, not taken as one entry: a duplicated supply denom is refused by
    # native validation, and summing keeps this check from depending on that.
    STATED_SUM=0
    while IFS= read -r amt; do
      if [[ -z "$amt" ]]; then continue; fi
      if [[ "$amt" =~ ^[0-9]+$ ]]; then dec_add "$STATED_SUM" "$amt"; STATED_SUM="$DEC_OUT"
      else SUPPLY_WHY="unreadable supply amount '$amt'"; fi
    done <<<"$STATED"
    BAL_SUM=0
    while IFS= read -r amt; do
      if [[ -z "$amt" ]]; then continue; fi
      if [[ "$amt" =~ ^[0-9]+$ ]]; then dec_add "$BAL_SUM" "$amt"; BAL_SUM="$DEC_OUT"
      else SUPPLY_WHY="unreadable balance amount '$amt'"; fi
    done <<<"$BAL_AMTS"
  else
    SUPPLY_WHY="bank supply or balances could not be read"
  fi
fi
if [[ -n "$SUPPLY_WHY" ]]; then
  bad trap.supply_within_max "starting $GC_NATIVE_DENOM supply <= max_supply" "$SUPPLY_WHY"
elif dec_le "$STATED_SUM" "$MAXSUP_FILE" && dec_le "$BAL_SUM" "$MAXSUP_FILE"; then
  ok trap.supply_within_max "starting $GC_NATIVE_DENOM supply <= max_supply (stated $STATED_SUM, balances $BAL_SUM, max $MAXSUP_FILE)"
else
  bad trap.supply_within_max "starting $GC_NATIVE_DENOM supply <= max_supply" \
      "stated supply $STATED_SUM, sum of balances $BAL_SUM, max_supply $MAXSUP_FILE — the supply-cap invariant is broken at genesis"
fi

# Block gas must be a working, finite ceiling. This is TW-004 and nothing else
# writes it. Stated positively, as a positive integer: -1 is unlimited, 0 admits
# no transaction with any gas at all, any other negative is not a ceiling, and an
# absent key is not a decision. Testing only `!= -1` passed all four.
MAXGAS="$(j '.consensus.params.block.max_gas')"
if [[ "$MAXGAS" =~ ^[1-9][0-9]*$ ]]; then
  ok trap.max_gas_finite "block.max_gas is a positive finite ceiling ($MAXGAS)"
elif [[ "$MAXGAS" == "-1" ]]; then
  bad trap.max_gas_finite "block.max_gas is a positive finite ceiling" "max_gas is -1 (unlimited) — TW-004 is NOT addressed in this genesis"
else
  bad trap.max_gas_finite "block.max_gas is a positive finite ceiling" "max_gas is '$MAXGAS' — it must be a positive integer"
fi

# The economic-address trap. A payout, settlement or treasury address that is a
# module account (or the all-zero address) passes `twilightd validate` and then
# PANICS the chain at InitChain: the keepers apply internal/economicaddress there,
# and no ValidateGenesis does. Reproduced for all three fields.
#
# Module addresses are derived the standard Cosmos way — authtypes.NewModuleAddress
# (name) is the first 20 bytes of sha256(name) — and bech32-encoded by the
# binary's own `debug addr`, so the prefix is the chain's rather than this
# script's. Addresses are compared lower-cased, because bech32 is
# case-insensitive and an upper-case spelling of a module account is the same
# account.
sha256_hex() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum | cut -c1-64
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 | cut -c1-64
  else return 1; fi
}
bech32_of_hex() { # prints the account bech32 for 20-byte hex, or nothing
  "$BIN" debug addr "$1" --home "$CLIHOME" 2>/dev/null | sed -n 's/^Bech32 Acc: //p'
}
if [[ -n "$BIN" ]]; then
  FORBIDDEN=""; FORBIDDEN_ERR=""
  for name in "${MODULE_ACCOUNT_NAMES[@]}" "(zero address)"; do
    if [[ "$name" == "(zero address)" ]]; then hex="0000000000000000000000000000000000000000"
    else hex="$(printf '%s' "$name" | sha256_hex | cut -c1-40)" || hex=""; fi
    b32=""
    if [[ "$hex" =~ ^[0-9a-f]{40}$ ]]; then b32="$(bech32_of_hex "$hex" || true)"; fi
    if [[ "$b32" =~ ^twilight1[0-9a-z]{38,58}$ ]]; then
      FORBIDDEN="${FORBIDDEN}${b32} ${name}"$'\n'
    else
      FORBIDDEN_ERR="${FORBIDDEN_ERR} ${name}"
    fi
  done

  forbidden_name() { # prints the module name when $1 is a forbidden destination
    local line
    while IFS= read -r line; do
      if [[ -n "$line" && "${line%% *}" == "$1" ]]; then printf '%s' "${line#* }"; return 0; fi
    done <<<"$FORBIDDEN"
    return 0
  }
  economic_check() { # economic_check <id> <label> <jq producing one address per line>
    local addrs a n hits=""
    if [[ -n "$FORBIDDEN_ERR" ]]; then
      bad "$1" "$2" "could not derive the address of:$FORBIDDEN_ERR"
      return 0
    fi
    if ! addrs="$(jq -r "$3" "$GENESIS" 2>/dev/null)"; then
      bad "$1" "$2" "the addresses could not be read"
      return 0
    fi
    while IFS= read -r a; do
      if [[ -z "$a" ]]; then continue; fi
      n="$(forbidden_name "$a")"
      if [[ -n "$n" ]]; then hits="${hits} ${a} (${n})"; fi
    done <<<"$addrs"
    if [[ -z "$hits" ]]; then ok "$1" "$2"
    else bad "$1" "$2" "refused at InitChain:${hits}"; fi
    return 0
  }
  economic_check trap.payout_not_module_account \
    "no slot payout address is a module account or the zero address" \
    '.app_state.coreslot.slots[]? | .payout_address | ascii_downcase'
  economic_check trap.settlement_not_module_account \
    "no slot settlement address is a module account or the zero address" \
    '.app_state.coreslot.slots[]? | .settlement_address | ascii_downcase'
  # All three copies. The mirror checks hold them equal, but this must not depend
  # on another check having passed.
  economic_check trap.treasury_not_module_account \
    "treasury address is not a module account or the zero address" \
    '[.app_state.rewards.params.treasury_address,
      .app_state.rewards.reward_config_versions[]?.treasury_address,
      .app_state.rewards.current_epoch_config.treasury_address]
     | .[] | select(. != null and . != "") | ascii_downcase'
else
  note "module-account checks skipped: they need --bin to derive the addresses."
fi

# ---- 6. launch decisions -----------------------------------------------------------------------
section "6. launch decisions (supplied, not inferred)"
eq decision.chain_id "chain_id"                    "$GC_CHAIN_ID"                    "$(j '.chain_id')"
eq decision.max_gas "block.max_gas"               "$GC_MAX_GAS"                     "$MAXGAS"
eq decision.active_slots "active slots"                "$GC_ACTIVE_SLOTS"                "$ACTIVE"
eq decision.min_active_slots "min_active_slots"            "$GC_MIN_ACTIVE_SLOTS"            "$MINA"
eq decision.epoch_length "epoch_length_blocks"         "$GC_EPOCH_LENGTH_BLOCKS"         "$(j '.app_state.rewards.params.epoch_length_blocks')"
eq decision.max_supply "max_supply"                  "$GC_MAX_SUPPLY"                  "$(j '.app_state.rewards.params.max_supply')"
eq decision.subsidy "initial_block_subsidy"       "$GC_INITIAL_BLOCK_SUBSIDY"       "$(j '.app_state.rewards.params.initial_block_subsidy')"
eq decision.distribution_method "distribution_method"         "$GC_DISTRIBUTION_METHOD"         "$(j '.app_state.rewards.params.distribution_method')"
eq decision.emission_share "emission_treasury_share_bps" "$GC_EMISSION_TREASURY_SHARE_BPS" "$EMIS_BPS"
eq decision.treasury_address "treasury_address"            "$GC_TREASURY_ADDRESS"            "$TREAS_ADDR"
eq decision.allow_self_registration "allow_self_registration"     "false"                           "$(j '.app_state.coreslot.params.allow_self_registration')"
# target_block_time_seconds drives NO computation — it is validated non-zero and
# then unused — so nothing in the chain notices when it disagrees with the pacing
# operators actually run. The node's own default is derived from the Go constant,
# not from this genesis field, so a genesis declaring 10 while nodes pace at 5
# passes every other check ever written. It is compared here because this is the
# only place the two can be brought together.
eq decision.target_block_time "target_block_time_seconds"   "$GC_BLOCK_TIME_SECONDS"          "$(j '.app_state.rewards.params.target_block_time_seconds')"

AUTH="$(j '.app_state.coreslot.params.authority')"
EAUTH="$(j '.app_state.coreslot.params.emergency_authority')"
# Checked against the stated decision first. Shape and distinctness below are
# properties any pair of addresses can have; only this says they are the RIGHT
# pair.
eq decision.authority "authority"                   "$GC_AUTHORITY"                   "$AUTH"
eq decision.emergency_authority "emergency_authority" "$GC_EMERGENCY_AUTHORITY"     "$EAUTH"
if [[ "$AUTH" =~ ^twilight1[0-9a-z]{38,58}$ ]]; then ok decision.authority_shape "authority has a well-formed address"
else bad decision.authority_shape "authority has a well-formed address" "found '$AUTH'"; fi
if [[ "$EAUTH" =~ ^twilight1[0-9a-z]{38,58}$ ]]; then ok decision.emergency_authority_shape "emergency_authority has a well-formed address"
else bad decision.emergency_authority_shape "emergency_authority has a well-formed address" "found '$EAUTH'"; fi
if [[ "$AUTH" != "$EAUTH" ]]; then ok decision.authorities_distinct "authority and emergency_authority are distinct"
else bad decision.authorities_distinct "authority and emergency_authority are distinct" "both are $AUTH — one compromised key reaches both roles"; fi
note "address checks are SHAPE only; this script does not verify a bech32 checksum."
note "Run with --bin so the chain's own validator does."

# ---- 7. what genesis cannot record --------------------------------------------------------------
section "7. emission projection — NOT a genesis value"
SUBSIDY="$(j '.app_state.rewards.params.initial_block_subsidy')"
SUPPLY="$(j '.app_state.rewards.params.max_supply')"
EPOCHLEN="$(j '.app_state.rewards.params.epoch_length_blocks')"
# A projection, not a check — and it must never be able to abort the run. A
# missing max_supply previously reached awk as 0 and killed the script with
# "division by zero" AFTER its own FAIL was recorded, so the summary and the
# verdict were never printed and a real verification failure exited 2, the code
# this script otherwise reserves for usage errors.
# The rate below is PRE-HALVING and is NOT the year's emission unless the first
# halving falls outside the year. Emission halves at 50% of max supply, so at fast
# pacing the threshold is crossed mid-year and the year's total is lower than
# rate x 1 year: at one-second blocks the shipped parameters halve near 9.6 months,
# making year one ~56% where the bare rate reads 62.5%. Printing that rate as
# "year-one emission" was simply wrong, so the two are now separate lines and the
# year's total walks whatever tiers the year actually crosses.
if is_num "$SUBSIDY" && is_num "$SUPPLY" && (( SUPPLY > 0 )) && is_num "$GC_BLOCK_TIME_SECONDS" && (( GC_BLOCK_TIME_SECONDS > 0 )); then
  awk -v s="$SUBSIDY" -v m="$SUPPLY" -v bt="$GC_BLOCK_TIME_SECONDS" 'BEGIN {
    spy = 31557600; bpy = spy / bt; rate = s * bpy;
    halve_yr = ((m/2)/s)*bt/spy;
    printf "  at %s-second blocks:\n", bt;
    printf "    pre-halving rate    %.0f utwlt/year  (%.2f%% of max supply)\n", rate, 100*rate/m;
    printf "    first halving       %.2f years  (at 50%% of max supply)\n", halve_yr;
    if (halve_yr >= 1) {
      printf "    year-one emission   %.2f%% of max supply  (no halving inside year one)\n", 100*rate/m;
    } else {
      # `sub` is an awk builtin, so the per-block subsidy is `cur`.
      emitted = 0; cur = s; left = bpy; target = m/2; step = m/4;
      while (left > 0 && emitted < m) {
        need = (target - emitted) / cur;
        if (need > left) { emitted += left * cur; left = 0; }
        else { emitted = target; left -= need; cur = cur/2; target += step; step = step/2; }
      }
      printf "    year-one emission   %.2f%% of max supply  (BELOW the rate: a halving lands inside year one)\n", 100*emitted/m;
    }
  }'
  if is_num "$EPOCHLEN"; then
    awk -v e="$EPOCHLEN" -v bt="$GC_BLOCK_TIME_SECONDS" \
      'BEGIN { printf "    epoch length        %.1f minutes\n", e*bt/60 }'
  fi
else
  note "projection skipped: subsidy='$SUBSIDY' max_supply='$SUPPLY' block_time='$GC_BLOCK_TIME_SECONDS'"
fi
note "Block time is timeout_commit in each node's config.toml. It is NOT in genesis."

# ---- summary ---------------------------------------------------------------------------------
printf '\n\033[1msummary\033[0m  %d passed, %d failed\n' "$PASS" "$FAIL"
if (( FAIL > 0 )); then
  printf '\033[31mGENESIS NOT READY\033[0m — %d check(s) failed.\n' "$FAIL"
  exit 1
fi
printf '\033[32mall checks passed\033[0m\n'
[[ -n "$BIN" ]] || { printf '\033[33mbut the chain'"'"'s own validator was not run — re-run with --bin\033[0m\n'; exit 1; }
exit 0
