---
title: CLI Reference
---

# CLI Reference

Every custom-module command is reachable two ways: under a **top-level group** with
short, stable names, which this site uses throughout, and under the SDK's generated
`query <module>` / `tx <module>` tree, whose names follow the RPC names. Both reach the
same handlers.

| Top-level group | Generated equivalent | Contents |
|---|---|---|
| `coreslot-query` | `query coreslot` | 14 read-only CoreSlot queries |
| `coreslot` | `tx coreslot` (same names) | 16 CoreSlot transactions |
| `rewards-query` | `query rewards` | 15 read-only rewards queries |
| `rewards` | `tx rewards` (`pause-rewards`, `resume-rewards`, `update-rewards-params`) | 3 rewards transactions |
| `mining-query` | `query mining` (same names) | 12 read-only settlement queries |
| — | `tx mining` | 2 settlement transactions; there is no top-level group for them |

Where the generated names differ: `query coreslot active-core-slots` is
`coreslot-query active`, `core-slot` is `slot`, `core-slot-by-operator` and
`core-slot-by-consensus-address` are `by-operator` and `by-consensus`,
`pending-key-rotations` is `pending-rotations`, `reserved-consensus-address` is
`reserved`; `query rewards current-epoch-active-blocks` is `current-active-blocks`,
`slot-entitlement` is `entitlement`, `slot-entitlements-by-epoch` is
`epoch-entitlements`, `rewards-pause-state` is `pause-state`.

The standard commands are the usual Cosmos ones: `init`, `start`, `export`, `keys`,
`validate`, `snapshots`, `prune`, `rollback`, `add-genesis-account`, `tx bank send`,
`query bank balances`, `query auth …`, `query upgrade …`, plus the genesis helpers
`coreslot-genesis add | set-authorities | validate`
([Genesis Reference](genesis-reference.md#building-a-launch-genesis)).

The 41 query commands are a **pinned surface**: a hand-written table in the repository
(`internal/queryapi/contract.go`) names every command, the request it builds and the RPC
it must reach, and CI holds the command tree and the dispatch against it, so a query
cannot be added, removed or rewired without a deliberate change to that table.

## Flags

| Flag | Where | Meaning |
|---|---|---|
| `--node <rpc>` | every command that talks to a node | CometBFT RPC endpoint (default `tcp://localhost:26657`) |
| `--output json` (`-o json`) | queries | Machine-readable output; the default is YAML-style text |
| `--height <h>` | queries | Read state at a past height (a pruning node may refuse) |
| `--from <key>`, `--chain-id`, `--gas`, `--fees 0utwlt`, `--broadcast-mode sync\|async`, `-y` | transactions | Standard Cosmos transaction flags; the chain requires no fee |
| `--limit`, `--offset`, `--page`, `--page-key`, `--reverse`, `--count-total` | the paginated queries below | Standard pagination; follow `pagination.next_key` until it is empty. `open-settlements` refuses `--count-total` (counting a canonical collection is unbounded work) |

The same queries are served over REST on the API server (default port `1317`,
`[api] enable = true`; routes below, Swagger at `/swagger/` when `api.swagger` is on)
and over gRPC (`9090`), which is canonical.

## Queries

### `coreslot-query`

| Command | Args | Returns | REST (`/twilight/coreslot/v1`) |
|---|---|---|---|
| `params` | — | the CoreSlot `Params` record ([Parameters](../rewards/params.md#coreslot)) | `/params` |
| `slot` | `[slot-id]` | one slot | `/slots/{slot_id}` |
| `slots` | — | every slot, any status | `/slots` (REST adds a `status` filter and pagination) |
| `active` | — | the ACTIVE slots | `/active-slots` |
| `by-operator` | `[address]` | the slot an operator address owns | `/operators/{operator_address}` |
| `by-consensus` | `[hex-address]` | the slot behind a consensus address (hex) | `/consensus/{consensus_address}` |
| `pending-rotations` | — | consensus-key rotations queued and not yet applied | `/pending-key-rotations` |
| `last-applied` | — | the validator set as last emitted to consensus | `/last-applied-validators` |
| `reserved` | `[hex-address]` | the reuse-lockout reservation on a consensus address | `/reserved-consensus-address/{consensus_address}` |
| `reward-weight` | `[slot-id]` | the slot's reward-weight metadata (never read for allocation) | `/slots/{slot_id}/reward-weight` |
| `selection-policy` | `[slot-id]` | the slot's current selection policy | `/slots/{slot_id}/selection-policy` |
| `selection-policy-version` | `[slot-id] [policy-version]` | one version of it | `/slots/{slot_id}/selection-policy/version/{policy_version}` |
| `selection-policy-at-height` | `[slot-id] [height]` | the version in force at a height | `/slots/{slot_id}/selection-policy/height/{at_height}` |
| `pending-authority-transfers` | — | nominations awaiting acceptance, both roles; `"transfers": []` when none, a success | `/pending-authority-transfers` |

### `rewards-query`

| Command | Args | Returns | REST (`/twilight/rewards/v1`) |
|---|---|---|---|
| `params` | — | the rewards `Params` record ([Parameters](../rewards/params.md#rewards)) | `/params` |
| `epoch-info` | — | the open epoch: `state`, `current_epoch_config`, `current_epoch_start_height`, `current_epoch_end_height`, `current_epoch_length_blocks`, `open_reward_enabled_blocks`, `has_pending_params`, `pending_params` | `/epoch-info` |
| `epoch-boundaries` | `[epoch]` | an epoch's `start_height`, `end_height`, `epoch_length_blocks` | `/epochs/{epoch_number}/boundaries` |
| `epoch-reward` | `[epoch]` | the finalized aggregate (`minted_emission`, `carry_in`, `treasury_amount`, `reward_pool`, `allocated_amount`, `carry_out`, `cumulative_emitted_after_epoch`; `rewards[]` is always empty); `NotFound` until the epoch is finalized | `/epochs/{epoch_number}` |
| `next-halving` | — | `current_tier`, `current_block_subsidy`, `next_threshold`, `remaining_until_next_halving`, `has_next_halving` | `/next-halving` |
| `cumulative-emitted` | — | `cumulative_emitted`, `max_supply` | `/cumulative-emitted` |
| `supply-schedule` | — | `params` plus the next-halving view | `/supply-schedule` |
| `current-active-blocks` | — (paginated) | the open epoch's active-block counters, ascending slot id | `/current-epoch/active-blocks` |
| `module-balances` | — | `denom`, `rewards_balance`, `fee_pool_balance`, `outstanding_entitlement_liability`, `carry_forward_remainder` | `/module-balances` |
| `entitlement` | `[slot-id] [epoch]` | one slot entitlement: amount, released amount, snapshotted payout address | `/slots/{slot_id}/entitlements/{epoch}` |
| `epoch-entitlements` | `[epoch]` (paginated) | an epoch's entitlements, ascending slot id | `/epochs/{epoch}/entitlements` |
| `reward-config-versions` | — (paginated) | the reward-configuration history, plus any scheduled entry | `/reward-config-versions` |
| `reward-config-version` | `[version]` or `epoch:N` | one version by number, or the version that binds epoch N | `/reward-config-version?version=` or `?effective_epoch=` |
| `epoch-config-versions` | — (paginated) | the epoch-configuration history, plus any scheduled entry | `/epoch-config-versions` |
| `pause-state` | — | `pause_state` (`current_paused`, `has_pending`, `pending_value`, `pending_effective_height`) and `release_enabled` | `/pause-state` |

### `mining-query`

| Command | Args | Returns | REST (`/twilight/mining/v1`) |
|---|---|---|---|
| `settlement` | `[slot-id] [epoch]` | the settlement row plus its `entitlement_amount`, `released_amount`, `remaining_amount`, `payout_address`, `participant_distribution_ceiling`, `created_settlement_clock`, `deadline_clock`, `current_settlement_clock`, `permissionless_finalization_now` | `/settlements/{slot_id}/{epoch}` |
| `open-settlements` | `[slot-id]` (paginated) | the slot's open settlements, ascending epoch, read from canonical rows | `/slots/{slot_id}/open-settlements` |
| `settlement-clock` | — | the settlement clock (ticks on release-enabled blocks; not a height) | `/settlement-clock` |
| `settlement-params-for-epoch` | `[epoch]` | the settlement-parameter version an epoch binds, the `binding_epoch`, and whether it is a `bootstrap` target | `/settlement-params-for-epoch/{epoch}` |
| `settlement-params-version` | `[version]` | one settlement-parameter version | `/settlement-params-versions/{version}` |
| `settlement-params-versions` | — (paginated) | the history | `/settlement-params-versions` |
| `distribution-mode-version` | `[version]` | one distribution-mode version | `/distribution-mode-versions/{version}` |
| `distribution-mode-versions` | — (paginated) | the history | `/distribution-mode-versions` |
| `selection-params-version` | `[version]` | one selection-parameter version | `/selection-params-versions/{version}` |
| `selection-params-versions` | — (paginated) | the history | `/selection-params-versions` |
| `target-epoch-interpretation` | `[target-epoch]` | the chain's reading of a target epoch: its `binding_epoch`, the governing `distribution_mode_version`, `selection_applicable` | `/target-epochs/{target_epoch}` |
| `validate-economic-address` | `[address]` | `admissible`, `rejection_reason`, `canonical_address` — the rule settlement applies to recipients and addresses | `/economic-address?address=` |

### Errors

Every query classifies its failures the same way, on gRPC and the CLI:

- a missing, zero or malformed argument, or an offset and a page key supplied together →
  `InvalidArgument`;
- a record that does not exist (an epoch not yet finalized, a slot id or address with no
  slot, a version that was never created) → `NotFound`; an empty list is a success, not a
  `NotFound`;
- stored state that exists but cannot be read — a record that will not decode, an index
  naming a missing record — → `Internal`, never `NotFound` and never `Unknown`: a client
  that sees `Internal` is looking at a damaged node, not an absent object.

REST maps `InvalidArgument` to 400, `NotFound` to 404 and `Internal` to 500.

## Transactions

Every transaction is signed with `--from <key>`; the message server checks that the
signer holds the role, and the CLI never infers or substitutes one. Who may sign what,
and when it takes effect, is on the
[Authority & Emergency Guide](../operators/authority-and-emergency-guide.md#who-can-do-what).

### `coreslot`

| Command | Args | Signer |
|---|---|---|
| `register` | `[operator] [payout] [settlement] [consensus-pubkey-base64] [moniker]` | authority |
| `activate` | `[slot-id]` | authority |
| `inactivate` | `[slot-id] [reason]` | authority, or the slot's operator |
| `suspend` | `[slot-id] [reason] [evidence-reference]` | authority or emergency authority |
| `remove` | `[slot-id] [reason]` | authority |
| `rotate-key` | `[slot-id] [new-consensus-pubkey-base64]` | authority |
| `update-payout` | `[slot-id] [new-payout]` | the slot's operator |
| `update-settlement` | `[slot-id] [settlement-address]` | the slot's operator |
| `update-metadata` | `[slot-id]` with `--moniker`, `--identity`, `--website`, `--security-contact`, `--details` (unnamed fields keep their values) | the slot's operator |
| `update-selection-policy` | `[slot-id] [selection-rate-bps] [max-selected-participants]` | the slot's operator, subject to the cooldown |
| `update-params` | `[params-json-file]` | authority |
| `nominate-authority` | `[primary\|emergency] [nominee]` | the role's current holder |
| `accept-authority` | `[primary\|emergency]` | the nominee |
| `cancel-authority-nomination` | `[primary\|emergency]` | the role's current holder |
| `schedule-upgrade` | `[name] [height] [info]` | authority |
| `cancel-upgrade` | — | authority |

### `rewards`

| Command | Args | Signer |
|---|---|---|
| `update-params` | `[params-json-file]` (the bare `Params` record; see [Transactions](../rewards/transactions.md#update-params)) | authority |
| `pause` | — | emergency authority |
| `resume` | — | emergency authority |

### `tx mining`

| Command | Flags | Signer |
|---|---|---|
| `submit-settlement-chunk` | `--slot-id`, `--epoch`, `--chunk-index`, `--payouts <json>` (one `{"recipient","amount"}` object per line; repeatable) | the slot's settlement address |
| `finalize-settlement` | `--slot-id`, `--epoch` | the settlement address before the deadline; any account from the deadline on ([Settlement](../rewards/settlement.md#finalization)) |
