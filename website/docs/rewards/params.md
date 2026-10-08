---
title: Parameters
---

# Parameters

Three Twilight modules carry parameters (the SDK's `auth`, `bank` and `consensus`
parameters are fixed at genesis: their authority is a keyless module address). What can
change, who can change it and when it takes effect differ per field — and most of the
rewards fields cannot be changed by any transaction at all.

| Module | Stored as | Changed by | Takes effect |
|---|---|---|---|
| `x/coreslot` | one `Params` record | `coreslot update-params`, signed by the authority; the two authority fields only by nomination and acceptance | immediately: later transactions in the same block see it |
| `x/rewards` | one `Params` record, plus versioned histories for the economics and the epoch geometry | `rewards update-params`, signed by the authority, queued; the histories only by genesis or an upgrade handler | the next epoch boundary |
| `x/mining` | versioned histories | genesis or an upgrade handler only | a version governs epochs from two after the one it becomes effective at |

Who holds the two authorities, and what else they can do, is on
[Security & Failure Modes](security-and-failure-modes.md#authority-model).

## Rewards

Rewards stores no authority of its own; both authorities are read from CoreSlot.
`rewards update-params` must be signed by the CoreSlot **authority**, is **queued**, and
activates at the next epoch boundary (the latest queued update wins). It takes a full
`Params` document, but almost every field must keep its current value:

| Field | Class | Notes |
|---|---|---|
| `native_denom` | Immutable | `utwlt`. Rejected if changed. |
| `max_supply` | Immutable | The hard supply cap. Rejected if changed. |
| `initial_block_subsidy` | Reward-configuration history | Rejected if changed by `update-params`. |
| `emission_treasury_share_bps` | Reward-configuration history | At most 5000, an immutable ceiling. Rejected if changed by `update-params`. |
| `treasury_address` | Reward-configuration history | Rejected if changed by `update-params`. |
| `epoch_length_blocks` | Epoch-configuration history | 360 to 720, immutable bounds. Rejected if changed by `update-params`; the copy in `Params` is frozen genesis data. |
| `halving_mode` | Genesis-fixed | `HALVING_MODE_SUPPLY_THRESHOLD`. Rejected if changed. |
| `distribution_method` | Genesis-fixed | `DISTRIBUTION_METHOD_UNIFORM_ACTIVE_BLOCKS`. Rejected if changed. |
| `remainder_policy` | Genesis-fixed | `REMAINDER_POLICY_CARRY_FORWARD`. Rejected if changed. |
| `fee_treasury_share_bps` | Genesis-fixed | Rejected if changed. |
| `emissions_enabled`, `epoch_settlement_enabled`, `claims_enabled` | Deprecated | Carry no authority; the canonical pause state replaced them, and on the default genesis they read `true` even while paused. `update-params` rejects any change. |
| `fee_collection_enabled`, `fee_distribution_enabled`, `weighted_rewards_enabled` | Must stay `false` | Validation rejects `true`. |
| `fee_denom` | Must equal `native_denom` | Validation rejects anything else. |
| `fee_distribution_mode` | Must stay `FEE_DISTRIBUTION_MODE_NONE` | Validation rejects anything else. |
| `target_block_time_seconds` | **Changeable** | Informational; nothing in the state machine reads it. Must be nonzero. |
| `max_claim_epochs_per_tx` | **Changeable**, deprecated | Gated a claim path that no longer exists. Must be nonzero. |

So `update-params` can change exactly two fields today, and neither affects a block.
The monetary and geometry values live in **versioned histories** — the reward
configuration (subsidy, treasury share and address) and the epoch configuration (epoch
length) — each version effective from an epoch boundary. A reward-configuration version
binds the epochs two after the one it takes effect at; an epoch-length version governs
the epoch it takes effect at. No transaction writes those histories: they come from genesis,
and a future upgrade handler is the only way a version could be added. Read them with
`rewards-query reward-config-versions` and `rewards-query epoch-config-versions`.

### What `update-params` rejects

- a signer other than the CoreSlot authority;
- any change to the immutable, history-governed, genesis-fixed or deprecated-flag fields
  above;
- a document that fails validation: a non-`utwlt` denom, a fee denom other than the
  native denom, a non-positive `max_supply` or `initial_block_subsidy`, a zero
  `epoch_length_blocks`, `target_block_time_seconds` or `max_claim_epochs_per_tx`, an
  unsupported enum value, any of the three feature flags set to `true`, a treasury share
  above 10000, or a positive treasury share with an empty or invalid `treasury_address`.

### The pause state is not a parameter

Reward accrual and release are stopped together by one canonical pause state, set by
[`rewards pause` / `rewards resume`](transactions.md#pause--resume) from the CoreSlot
**emergency authority** and effective at the start of the next block. Read it with
`rewards-query pause-state`, never from the deprecated flags in `params`.

## CoreSlot

`coreslot update-params` takes a full `Params` document, must be signed by the
**authority**, and takes effect immediately — later transactions in the same block
already see it; there is no queue.

| Field | Default | Rule |
|---|---|---|
| `authority` | genesis | Rejected if it differs: the role moves only by `nominate-authority primary` from the holder and `accept-authority` from the nominee. |
| `emergency_authority` | genesis | The same, with role `emergency`. |
| `slot_voting_power` | `1` | Positive; cannot change while any slot is ACTIVE; `max_active_slots × slot_voting_power` must fit in a signed 64-bit integer. |
| `min_active_slots` | `1` | At least 1 and at most `max_active_slots`. Inactivation, and suspension unless allowed below, refuse to take the active set below it. |
| `max_active_slots` | `100` | At least `min_active_slots`, at most 100 (an immutable ceiling). |
| `key_rotation_delay_blocks` | `1` | Blocks between a rotation request and the key switch. |
| `consensus_key_reuse_lockout` | `100000` | Blocks for which a retired consensus key cannot be reused. |
| `allow_emergency_below_min_active` | `false` | When `true`, a suspension by either authority may take the active set below `min_active_slots`, but never below one active slot. At the default minimum of 1 it changes nothing. |
| `selection_policy_update_cooldown_blocks` | `720` | At least 360 (an immutable minimum): the spacing between an operator's selection-policy updates. |
| `activation_delay_blocks`, `removal_delay_blocks`, `allow_self_registration` | `0`, `0`, `false` | Deprecated; validation requires exactly these values. |

Read the current record with `coreslot-query params`.

## Mining

Settlement parameters (the participant window, chunk and recipient limits, the minimum
payout) and the distribution mode are versioned histories with no transaction behind
them; see [Settlement → Parameters](settlement.md#parameters).
