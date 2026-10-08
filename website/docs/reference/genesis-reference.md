---
title: Genesis
---

# Genesis

What a Twilight genesis file contains, what the binary writes by default, what a fresh
genesis must satisfy, and how a launch genesis is built and checked.

A launch genesis is expected to be zero-premine: total supply rises only through epoch
emission. The localnet fixtures fund accounts and are not a production genesis (see
[Localnet fixtures](#localnet-fixtures-not-production)).

## Module init order

`InitGenesis` runs in this order (set in `app/config.go`):

```text
upgrade → auth → bank → consensus → coreslot → rewards → mining
```

`upgrade` goes first; it carries no state in the genesis file and only records each
module's consensus version, which a later upgrade handler migrates from. CoreSlot
precedes rewards, and mining comes last: it consumes both custom modules. Neither
rewards nor mining `InitGenesis` mints or sends; they only write state. Mining also
rebuilds its derived indexes (the open-settlement index and the version indexes) from
the rows it imported rather than importing them. Only `fee_collector` exists as a module
account after InitGenesis; the others are created on first use (see
[Module Accounts](module-accounts.md)).

## Inspecting the default genesis

```bash
twilightd init <moniker> --chain-id <chain-id>
jq '.app_state.coreslot' ~/.twilightd/config/genesis.json
jq '.app_state.rewards' ~/.twilightd/config/genesis.json
jq '.app_state.mining' ~/.twilightd/config/genesis.json
```

A default genesis is not a launch genesis: it names keyless module accounts as both
authorities and holds no slots. [Building a launch genesis](#building-a-launch-genesis)
turns it into one.

`twilightd init` writes an `app_state` with six sections: `auth`, `bank`, `upgrade`
(empty), and the three Twilight modules below. `consensus` has no `app_state` section;
the consensus parameters, including `block.max_gas`, are the file's top-level
`consensus.params`.

The "Validate" column is what the module's own genesis validation enforces on a fresh
genesis. `make check-genesis` (below) requires more of a launch file; those extra
requirements are listed after the tables.

## `coreslot`

| Field | Type | Validate |
|---|---|---|
| `params` | `Params` | See [Parameters → CoreSlot](../rewards/params.md#coreslot). `twilightd init` writes the keyless `coreslot-authority` module account as `authority` and `coreslot-emergency` as `emergency_authority`; a launch replaces both (below) |
| `slots` | `CoreSlot[]` | Only `PENDING` or `ACTIVE`; the ACTIVE count must lie within `[min_active_slots, max_active_slots]`, so a launch needs at least `min_active_slots` active slots |
| `next_slot_id` | uint64 | Must exceed the highest slot id present |
| `reward_weights` | `OperatorRewardWeight[]` | Each must name an existing slot; `coreslot-genesis add` writes one per slot (metadata; not read by rewards) |
| `selection_policies` | `SelectionPolicyVersion[]` | One version per slot, written by `coreslot-genesis add` |
| `pending_key_rotations` | `PendingKeyRotation[]` | Must be empty |
| `reserved_consensus_addresses` | `ReservedConsensusAddress[]` | Consensus keys under the reuse lockout; accepted |
| `last_applied_validators` | `LastAppliedValidator[]` | The validator set as last emitted; accepted |
| `pending_authority_transfers` | `PendingAuthorityTransferEntry[]` | Nominations in flight, by role; accepted |

## `rewards`

| Field | Type | Validate |
|---|---|---|
| `params` | `Params` | See [Parameters → Rewards](../rewards/params.md#rewards); `native_denom` must be `utwlt` |
| `state` | `RewardsState` | `current_epoch = 1` and `current_epoch_start_height = 1`; `cumulative_emitted` and `carry_forward_remainder` must parse as integers, with `cumulative_emitted ≤ max_supply` |
| `current_epoch_config` | `EpochConfigSnapshot` | The open epoch's frozen configuration (mirrors version 1 below) |
| `epoch_config_versions` | `EpochConfigVersion[]` | Exactly one, version 1 effective from epoch 1: the epoch length (360 to 720) |
| `scheduled_epoch_configs` | `ScheduledEpochConfig[]` | Must be empty |
| `reward_config_versions` | `RewardConfigVersion[]` | Exactly one, version 1 effective from epoch 1: the subsidy, treasury share (at most 5000 bps) and treasury address |
| `scheduled_reward_configs` | `ScheduledRewardConfig[]` | Must be empty |
| `pause_state` | `RewardsPauseState` | No pending transition; `current_paused` is accepted either way |
| `open_reward_enabled_blocks` | uint64 | Must be 0 |
| `has_pending_params`, `pending_params` | bool, `Params` | A queued params update; accepted |
| `finalized_epochs` | `EpochReward[]` | Must be empty |
| `slot_entitlements` | `SlotEntitlement[]` | Must be empty |
| `outstanding_entitlement_liability` | string (int) | Must be `"0"` |

The default genesis has no premine: `cumulative_emitted` is `"0"`, it adds no balances,
and total supply rises only through emission. It holds no pending params, no finalized
epochs and no entitlements, and `current_epoch_config` is the snapshot built from
`params`.

## `mining`

| Field | Type | Validate |
|---|---|---|
| `distribution_mode_versions` | `MiningDistributionModeVersion[]` | Exactly one, version 1 valid from epoch 1 with no end: `MINING_DISTRIBUTION_MODE_TRUSTED_AS_DISTRIBUTION` |
| `settlement_params_versions` | `SettlementParamsVersion[]` | Exactly one, version 1 effective from epoch 1: see [Settlement → Parameters](../rewards/settlement.md#parameters) |
| `selection_params_versions` | `SelectionParamsVersion[]` | Exactly one, version 1. No selection runs in this version, but InitChain checks every ACTIVE slot's selection policy against it (rate at most `max_selection_rate_bps`, selected participants at most `max_selected_participants_per_selection`) |
| `scheduled_distribution_modes`, `scheduled_settlement_params`, `scheduled_selection_params` | lists | Must be empty |
| `settlement_clock` | uint64 | Must be 0 (counts settlement-enabled blocks, not a height) |
| `last_processed_reward_epoch` | uint64 | Must be 0 |
| `settlement_epoch_anchors` | `SettlementEpochAnchor[]` | Must be empty |
| `settlements` | `Settlement[]` | Must be empty |

The default mining genesis describes a trusted-distribution chain whose first epoch is
1: one version in each history, effective from epoch 1 — the distribution mode
(`TRUSTED_AS_DISTRIBUTION`), the settlement parameters (window 2 epochs, 32 recipients per
chunk, 4 chunks per settlement, minimum payout `10000utwlt`) and the selection
parameters — with nothing scheduled and the clock and cursor at zero.

## What `make check-genesis` requires beyond `Validate`

A launch file must also have `cumulative_emitted` and `carry_forward_remainder` of
`"0"`, `has_pending_params` false, `current_paused` false, and empty
`pending_authority_transfers` and `reserved_consensus_addresses`. The chain accepts each
of those non-empty; a launch does not want them.

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

# the module's own genesis validation (which already needs min_active_slots ACTIVE
# slots), plus: activation heights and policies pinned to the file's initial_height,
# and any CometBFT validator list the file states must match the ACTIVE slots
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

It runs eight numbered sections in three layers that are deliberately kept apart: the
chain's own validation (authoritative, including `coreslot-genesis validate`), rules
true of any correct launch file (the invariants above, the consistency of the `params`
mirrors with version 1 of each history and the epoch snapshot, the immutable bounds from
`app/params/bounds.go`, known traps), and the **launch decisions**, which must be
supplied as `GC_*` rather than inferred — a file checked against itself proves nothing,
and a default left in place is not a decision. The seven variables shown are required.
The remaining `GC_*` variables (max supply, subsidy, epoch length, treasury share and
address, block time, `allow_emergency_below_min_active`) are shipped-default
expectations: leave one out and the file must equal the shipped default for it. With
the binary it also starts a throwaway node against the file until InitChain has run,
which is the only check that asks every module's `InitGenesis` rather than predicting
it. It ends with an emission projection that is informational, not a genesis value.

## Exported state

`twilightd export` writes every module's state in the same shape as genesis, and it is
complete: every monetary fact of the chain is in it. But **this binary cannot re-import
an export of a running chain** — every Twilight module's importer accepts only a fresh genesis,
and a continuation importer is deferred. An export is a record, not a restore path. It
carries what a launch file must not: finalized epochs, slot entitlements, a non-zero
liability and open-reward-enabled block count, settlements and their anchors, a non-zero
settlement clock and cursor, pending key rotations; and pending authority transfers,
which `Validate` accepts but `make check-genesis` refuses. See
[Upgrade & Export/Import](../operators/upgrade-and-export-import.md#exporting-state).

## Localnet fixtures (not production)

The localnet `init.sh` funds the authority and emergency accounts with
`1,000,000,000,000utwlt` each and registers four active CoreSlots. That is a funded
development fixture, not the zero-premine genesis a launch uses. See
[Localnet & Drills](../development/localnet-drills.md).
