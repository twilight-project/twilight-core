#!/usr/bin/env bash
# Pinned qualification profiles for scripts/localnet/release-upgrade-rehearsal.sh.
#
# A profile is everything that makes one rehearsal mean one specific thing: the
# published release it starts from (tag AND exact commit), the upgrade name the
# candidate must execute, the module version map the candidate must leave behind,
# and that upgrade's own proof of what it delivers. The script holds everything
# that is the same for every upgrade — topology, halt, partial rollout, straggler,
# agreement, state, convergence.
#
# Nothing here is overridable at run time. The caller names a profile and gets
# exactly these values; a different qualification is a different profile, added
# by a reviewed change. That keeps the property the hard-coded version had: a PASS
# is attributable to one release and one upgrade, and the verdict says which.
#
# Sourced by the rehearsal and by its fault suite; it defines functions and data
# only and runs nothing.

# Every profile this repository can qualify. A profile not listed here does not
# exist, even if a case below would load it.
REHEARSAL_PROFILES=(v0.1.0-to-v0.2.0 v0.3.1-to-v0.4.0)

# The assertions every profile makes, keyed by (assertion, node) as drill-assert
# records them. A profile adds its surface assertions to these; the total is the
# run's contract.
REHEARSAL_BASE_MULTISET="a_matches_published_checksum|-:1,a_reports_released_commit|-:1,a_reports_released_version|-:1,a_survived_init|-:1,b_reports_candidate_commit|-:1,b_reports_upgrade_version|-:1,binaries_differ|-:1,cometbft_validators|-:1,converged_binary_is_b|3:1,coreslot_active_slots|-:1,coreslot_snapshots_taken|-:1,coreslot_state_unchanged_across_boundary|-:1,final_agree_app_hash|0:1,final_agree_app_hash|1:1,final_agree_app_hash|2:1,final_agree_app_hash|3:1,halt_app_height|0:1,halt_app_height|1:1,halt_app_height|2:1,halt_app_height|3:1,halt_block_store_height|0:1,halt_block_store_height|1:1,halt_block_store_height|2:1,halt_block_store_height|3:1,halt_logged_upgrade_required|0:1,halt_logged_upgrade_required|1:1,halt_logged_upgrade_required|2:1,halt_logged_upgrade_required|3:1,pending_plan_height|0:1,pending_plan_height|1:1,pending_plan_height|2:1,pending_plan_height|3:1,pending_plan_name|0:1,pending_plan_name|1:1,pending_plan_name|2:1,pending_plan_name|3:1,quorum_progressed_during_stale|0:1,quorum_progressed_during_stale|1:1,quorum_progressed_during_stale|2:1,running_binary_is_a|0:1,running_binary_is_a|1:1,running_binary_is_a|2:1,running_binary_is_a|3:1,running_binary_is_b|0:1,running_binary_is_b|1:1,running_binary_is_b|2:1,schedule_tx_delivered|-:1,stale_caught_up_on_b|-:1,stale_did_not_commit_h|3:1,stale_process_after_refusal_characterization|3:1,stale_refusal_is_fresh|3:1,stale_rpc_after_refusal_characterization|3:1,stale_window_agree_app_hash|0:1,stale_window_agree_app_hash|1:1,stale_window_agree_app_hash|2:1,stale_window_agree_next_validators_hash|0:1,stale_window_agree_next_validators_hash|1:1,stale_window_agree_next_validators_hash|2:1,stale_window_agree_validators_hash|0:1,stale_window_agree_validators_hash|1:1,stale_window_agree_validators_hash|2:1,stale_window_has_common_fresh_height|-:1,upgrade_info_height|0:1,upgrade_info_height|1:1,upgrade_info_height|2:1,upgrade_info_height|3:1,upgrade_info_name|0:1,upgrade_info_name|1:1,upgrade_info_name|2:1,upgrade_info_name|3:1,upgrade_info_present|0:1,upgrade_info_present|1:1,upgrade_info_present|2:1,upgrade_info_present|3:1,upgrade_recorded_applied|-:1,upgraded_agree_app_hash|0:1,upgraded_agree_app_hash|1:1,upgraded_agree_app_hash|2:1,upgraded_agree_next_validators_hash|0:1,upgraded_agree_next_validators_hash|1:1,upgraded_agree_next_validators_hash|2:1,upgraded_agree_validators_hash|0:1,upgraded_agree_validators_hash|1:1,upgraded_agree_validators_hash|2:1,upgraded_quorum_passed_the_boundary|-:1,validator_identities_unique|-:1,validator_identity_power|0:1,validator_identity_power|1:1,validator_identity_power|2:1,validator_identity_power|3:1,validator_identity_present|0:1,validator_identity_present|1:1,validator_identity_present|2:1,validator_identity_present|3:1,validator_power_max|-:1,validator_power_min|-:1,version_map_is_expected|-:1"

# Evidence every profile must leave behind. A profile adds its own surface files.
REHEARSAL_BASE_FILES=(
  binaries.json topology.json halt.json agreement.json
  coreslot-at-H-1.json coreslot-at-H.json version-map-after.json
  assertions.jsonl summary.csv
)

# rehearsal_profile_load <name> — set the PROFILE_* values for one profile.
#
# Returns 2 when no name was given and 1 when the name is not a listed profile,
# and sets nothing in either case, so a caller cannot proceed on half a profile.
rehearsal_profile_load() { # <name>
  local name="${1:-}"
  [[ -n "$name" ]] || return 2
  local known=no p
  for p in "${REHEARSAL_PROFILES[@]}"; do [[ "$p" == "$name" ]] && known=yes; done
  [[ "$known" == yes ]] || return 1

  case "$name" in
    v0.1.0-to-v0.2.0)
      # The first state-machine change after v0.1.0: two-step authority rotation
      # takes CoreSlot from consensus version 1 to 2.
      PROFILE_NAME="$name"
      PROFILE_FROM_TAG="v0.1.0"
      PROFILE_FROM_COMMIT="b8ed78ed29f1667fceab8476f5e303c589471fa7"
      PROFILE_UPGRADE_NAME="v0.2.0"
      # Pinned rather than derived, so a module silently dropped, an unexpected one
      # appearing, or a migration that failed to bump a version all fail.
      PROFILE_VERSION_MAP_AFTER="auth:5,bank:4,consensus:1,coreslot:2,mining:1,rewards:1,runtime:omitted-zero,upgrade:2"
      PROFILE_STATE_SUMMARY="complete CoreSlot state identical; only coreslot moved 1 -> 2"
      PROFILE_SURFACE_FN=rehearsal_surface_authority_rotation
      PROFILE_SURFACE_SUMMARY="the candidate builds a real MsgNominateAuthority"
      PROFILE_SURFACE_MULTISET="nomination_builds_the_right_msg|-:1,nomination_carries_the_nominee|-:1,nomination_carries_the_role|-:1"
      PROFILE_SURFACE_FILES=(nomination-tx.json)
      ;;
    v0.3.1-to-v0.4.0)
      # Ends unlimited block gas: the handler sets block.max_gas to the ratified
      # ceiling through the consensus keeper (#170) and moves no module version.
      # Starts from v0.3.1, the release operators hold when v0.4.0 is scheduled.
      PROFILE_NAME="$name"
      PROFILE_FROM_TAG="v0.3.1"
      PROFILE_FROM_COMMIT="dd8a5e971ac80cdb3558393dd1dc7766e8ca5669"
      PROFILE_UPGRADE_NAME="v0.4.0"
      PROFILE_VERSION_MAP_AFTER="auth:5,bank:4,consensus:1,coreslot:2,mining:1,rewards:1,runtime:omitted-zero,upgrade:2"
      PROFILE_STATE_SUMMARY="complete CoreSlot state identical; no module version moved"
      PROFILE_SURFACE_FN=rehearsal_surface_block_max_gas
      PROFILE_SURFACE_SUMMARY="CometBFT runs with block.max_gas 30000000 from H+1; max_bytes, evidence, validator, version and abci unchanged"
      PROFILE_SURFACE_MULTISET="block_max_bytes_unchanged|-:1,block_max_gas_after_boundary|-:1,block_max_gas_before_boundary|-:1,consensus_params_heights|-:1,consensus_params_read|-:1,other_sections_unchanged|-:1"
      PROFILE_SURFACE_FILES=(consensus-params.json)
      ;;
    *)
      # Listed but not defined: a defect in this file, not a caller error.
      return 1
      ;;
  esac
}

# rehearsal_expected_multiset — the run's full assertion contract for the loaded
# profile, sorted exactly as drill-assert sorts what it observed (LC_ALL=C), so the
# two compare as strings.
rehearsal_expected_multiset() {
  printf '%s,%s\n' "$REHEARSAL_BASE_MULTISET" "$PROFILE_SURFACE_MULTISET" \
    | tr ',' '\n' | LC_ALL=C sort | paste -sd, -
}

# rehearsal_expected_assertions — how many assertion rows that contract implies:
# the sum of every entry's count.
rehearsal_expected_assertions() {
  rehearsal_expected_multiset | tr ',' '\n' | awk -F: '{ n += $NF } END { print n + 0 }'
}

# ---- surface proofs: what each upgrade delivers ----------------------------------
#
# Each runs in the rehearsal's context after the state phase, with BIN_B, CHAIN_ID,
# node_home, rpc_url, expect, fail and DRILL_EVID_DIR available. It must record
# exactly the assertions its profile's PROFILE_SURFACE_MULTISET lists.

# v0.2.0: the candidate's CLI builds a real two-step authority nomination.
#
# `--help` returning zero proves nothing: cobra exits zero for a parent's help even
# when the child is absent. This builds an actual transaction and inspects the
# message it produced. --generate-only, so nothing is broadcast and the state the
# previous phase compared is untouched.
rehearsal_surface_authority_rotation() {
  local auth_addr nominee
  auth_addr="$("$BIN_B" keys show operator0 -a --keyring-backend test --home "$(node_home 0)" 2>/dev/null)"
  nominee="$("$BIN_B" keys show operator1 -a --keyring-backend test --home "$(node_home 1)" 2>/dev/null)"
  if [[ -z "$auth_addr" || -z "$nominee" ]]; then
    fail "could not resolve the authority or nominee address"
    return
  fi
  "$BIN_B" tx coreslot nominate-authority primary "$nominee" \
    --from operator0 --keyring-backend test --home "$(node_home 0)" \
    --chain-id "$CHAIN_ID" --node "$(rpc_url 1)" --generate-only --output json \
    >"$DRILL_EVID_DIR/nomination-tx.json" 2>/dev/null
  expect "nomination_builds_the_right_msg" "/twilight.coreslot.v1.MsgNominateAuthority" \
    "$(jq -r '.body.messages[0]."@type" // "MISSING"' "$DRILL_EVID_DIR/nomination-tx.json" 2>/dev/null)"
  expect "nomination_carries_the_role" "AUTHORITY_ROLE_PRIMARY" \
    "$(jq -r '.body.messages[0].role // "MISSING"' "$DRILL_EVID_DIR/nomination-tx.json" 2>/dev/null)"
  expect "nomination_carries_the_nominee" "$nominee" \
    "$(jq -r '.body.messages[0].nominee // "MISSING"' "$DRILL_EVID_DIR/nomination-tx.json" 2>/dev/null)"
}

# v0.4.0: CometBFT itself runs with the new block gas ceiling.
#
# Read from CometBFT's /consensus_params on an upgraded node, which is what CometBFT
# adopted, not what the application wrote: the application could store a value
# CometBFT never received. CometBFT saves the params a block's response carries
# under the NEXT height, so /consensus_params?height=H is the last set it built
# under (the old one, which built the upgrade block itself) and H+1 is the first
# built under the new one.
#
# Fail closed. Every reader turns an empty, null or unparsable value into a marker
# that differs between the two sides, so two unreadable responses can never compare
# as equal; and the heights CometBFT answered for are checked, so a node that
# served some other height cannot stand in for the boundary.
_rehearsal_cp_field() { # <json> <jq path> <marker>
  local v
  v="$(jq -r "$2 // empty" <<<"$1" 2>/dev/null)"
  if [[ -n "$v" ]]; then echo "$v"; else echo "$3"; fi
}
_rehearsal_cp_sections() { # <json> <marker> — evidence, validator, version, abci; none may be null
  local v
  v="$(jq -ce '.result.consensus_params | [.evidence, .validator, .version, .abci]
               | if any(.[]; . == null) then error("a section is missing") else . end' <<<"$1" 2>/dev/null)"
  if [[ -n "$v" ]]; then echo "$v"; else echo "$2"; fi
}
rehearsal_surface_block_max_gas() {
  local before after readable=yes r
  before="$(rpc_get 1 "/consensus_params?height=$UPGRADE_HEIGHT" 2>/dev/null)" || before=""
  after="$(rpc_get 1 "/consensus_params?height=$((UPGRADE_HEIGHT + 1))" 2>/dev/null)" || after=""
  for r in "$before" "$after"; do
    jq -e '.result.consensus_params | (.block and .evidence and .validator and .version and .abci)' \
      <<<"$r" >/dev/null 2>&1 || readable=no
  done
  expect "consensus_params_read" "yes" "$readable"
  jq -n --arg b "$before" --arg a "$after" '{before: ($b | fromjson? // $b), after: ($a | fromjson? // $a)}' \
    >"$DRILL_EVID_DIR/consensus-params.json" 2>/dev/null
  expect "consensus_params_heights" "$UPGRADE_HEIGHT,$((UPGRADE_HEIGHT + 1))" \
    "$(_rehearsal_cp_field "$before" '.result.block_height' MISSING),$(_rehearsal_cp_field "$after" '.result.block_height' MISSING)"
  expect "block_max_gas_before_boundary" "-1" \
    "$(_rehearsal_cp_field "$before" '.result.consensus_params.block.max_gas' MISSING)"
  expect "block_max_gas_after_boundary" "30000000" \
    "$(_rehearsal_cp_field "$after" '.result.consensus_params.block.max_gas' MISSING)"
  expect "block_max_bytes_unchanged" \
    "$(_rehearsal_cp_field "$before" '.result.consensus_params.block.max_bytes' MISSING-BEFORE)" \
    "$(_rehearsal_cp_field "$after" '.result.consensus_params.block.max_bytes' MISSING-AFTER)"
  expect "other_sections_unchanged" \
    "$(_rehearsal_cp_sections "$before" MISSING-BEFORE)" "$(_rehearsal_cp_sections "$after" MISSING-AFTER)"
}
