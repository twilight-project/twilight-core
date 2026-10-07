---
title: Genesis Reference
---

# Genesis Reference

What a Twilight genesis file contains, what the binary writes by default, what a fresh
genesis must satisfy, and how a launch genesis is built and checked. For the module
initialization order see [Chain → Genesis](../chain/genesis.md).

`twilightd init` writes an `app_state` with six sections: `auth`, `bank`, `upgrade`
(empty), and the three Twilight modules below. `consensus` has no genesis section.

## `coreslot`

| Field | Type | Fresh genesis |
|---|---|---|
| `params` | `Params` | See [Parameters → CoreSlot](../rewards/params.md#coreslot). `twilightd init` writes the keyless `coreslot-authority` and `coreslot-emergency` module accounts as both authorities; a launch must replace them (below) |
| `slots` | `CoreSlot[]` | Only `PENDING` or `ACTIVE`; the ACTIVE count must lie within `[min_active_slots, max_active_slots]`, so a launch needs at least `min_active_slots` active slots |
| `next_slot_id` | uint64 | Must exceed the highest slot id present |
| `reward_weights` | `OperatorRewardWeight[]` | One per slot (metadata; not read by rewards) |
| `selection_policies` | `SelectionPolicyVersion[]` | One version per slot |
| `pending_key_rotations` | `PendingKeyRotation[]` | Must be empty |
| `reserved_consensus_addresses` | `ReservedConsensusAddress[]` | Consensus keys under the reuse lockout |
| `last_applied_validators` | `LastAppliedValidator[]` | The validator set as last emitted |
| `pending_authority_transfers` | `PendingAuthorityTransferEntry[]` | Nominations in flight, by role |

## `rewards`

| Field | Type | Fresh genesis |
|---|---|---|
| `params` | `Params` | See [Parameters → Rewards](../rewards/params.md#rewards); `native_denom` must be `utwlt` |
| `state` | `RewardsState` | `current_epoch = 1`, `current_epoch_start_height = 1`, `cumulative_emitted = "0"`, `carry_forward_remainder = "0"` |
| `current_epoch_config` | `EpochConfigSnapshot` | The open epoch's frozen configuration (mirrors version 1 below) |
| `epoch_config_versions` | `EpochConfigVersion[]` | Exactly one, version 1 effective from epoch 1: the epoch length (360 to 720) |
| `scheduled_epoch_configs` | `ScheduledEpochConfig[]` | Must be empty |
| `reward_config_versions` | `RewardConfigVersion[]` | Exactly one, version 1 effective from epoch 1: the subsidy, treasury share (at most 5000 bps) and treasury address |
| `scheduled_reward_configs` | `ScheduledRewardConfig[]` | Must be empty |
| `pause_state` | `RewardsPauseState` | `current_paused`, and no pending transition |
| `open_reward_enabled_blocks` | uint64 | Must be 0 |
| `has_pending_params`, `pending_params` | bool, `Params` | A queued params update, if any |
| `finalized_epochs` | `EpochReward[]` | Must be empty |
| `slot_entitlements` | `SlotEntitlement[]` | Must be empty |
| `outstanding_entitlement_liability` | string (int) | Must be `"0"` |

`cumulative_emitted` can never exceed `max_supply`, and the default genesis has no
premine: total supply starts at zero and rises only through emission.

## `mining`

| Field | Type | Fresh genesis |
|---|---|---|
| `distribution_mode_versions` | `MiningDistributionModeVersion[]` | Exactly one, version 1 valid from epoch 1 with no end: `MINING_DISTRIBUTION_MODE_TRUSTED_AS_DISTRIBUTION` |
| `settlement_params_versions` | `SettlementParamsVersion[]` | Exactly one, version 1 effective from epoch 1: see [Settlement → Parameters](../rewards/settlement.md#parameters) |
| `selection_params_versions` | `SelectionParamsVersion[]` | Exactly one, version 1 (for the protocol-selection mode, which has no producer in this version) |
| `scheduled_distribution_modes`, `scheduled_settlement_params`, `scheduled_selection_params` | lists | Must be empty |
| `settlement_clock` | uint64 | Must be 0 (counts settlement-enabled blocks, not a height) |
| `last_processed_reward_epoch` | uint64 | Must be 0 |
| `settlement_epoch_anchors` | `SettlementEpochAnchor[]` | Must be empty |
| `settlements` | `Settlement[]` | Must be empty |

## Building a launch genesis

`twilightd init` writes defaults; two CoreSlot commands and one standard command fill in
what a launch decides; everything else (rewards economics, settlement parameters,
CoreSlot parameters, `block.max_gas`) is a hand edit of the JSON. This is the sequence
the localnet uses:

```bash
twilightd init <moniker> --chain-id <chain-id> --home <home>

# the two authorities (the default genesis names keyless module accounts)
twilightd coreslot-genesis set-authorities <authority> <emergency-authority> --home <home>

# fund the accounts that must exist on chain (fees are zero; the balance only needs to exist)
twilightd add-genesis-account <authority> <amount>utwlt --home <home>

# one ACTIVE slot per validator: operator, payout and settlement addresses, the
# base64 consensus public key, a moniker; writes the slot, its reward weight and
# its selection policy, and advances next_slot_id
twilightd coreslot-genesis add <operator> <payout> <settlement> <consensus-pubkey-base64> <moniker> --home <home>

# the module's own genesis validation, plus "at least one ACTIVE slot"
twilightd coreslot-genesis validate --home <home>
```

Every operator then receives the same `genesis.json`. The settlement address has no
default: it is mandatory protocol state, and it decides who may sign that slot's
participant payouts ([Settlement](../rewards/settlement.md#chunks)).

## Checking a genesis: `make check-genesis`

```bash
GC_CHAIN_ID=<chain-id> GC_ACTIVE_SLOTS=<n> GC_MIN_ACTIVE_SLOTS=<n> GC_MAX_GAS=<n> \
GC_DISTRIBUTION_METHOD=DISTRIBUTION_METHOD_UNIFORM_ACTIVE_BLOCKS \
GC_AUTHORITY=<address> GC_EMERGENCY_AUTHORITY=<address> \
  make check-genesis GENESIS=path/to/genesis.json
```

It runs seven layers, deliberately separate: the document shape; the chain's own
validators (authoritative, including `coreslot-genesis validate`); the fresh-genesis
invariants above; the consistency of the `params` mirrors with version 1 of each history
and the epoch snapshot; the immutable bounds from `app/params/bounds.go`; known traps;
and the **launch decisions**, which must be supplied (`GC_*`) rather than inferred — a
file checked against itself proves nothing, and a default left in place is not a
decision. Optional decisions cover the denom, max supply, subsidy, epoch length, treasury
share and address, block time and `allow_emergency_below_min_active`. With the binary it
also starts a throwaway node against the file until InitChain has run, which is the only
check that asks every module's `InitGenesis` rather than predicting it. It ends with an
emission projection that is informational, not a genesis value.

## Exported state

`twilightd export` writes every module's state in the same shape, so an export of a
running chain carries what a fresh genesis forbids: finalized epochs, slot entitlements,
a non-zero liability and open-reward-enabled block count, settlements and their anchors, a
non-zero settlement clock and cursor, pending key rotations and authority transfers.
What a re-import must preserve is on
[Upgrade & Export/Import](../operators/upgrade-and-export-import.md#exporting-state).
