---
title: Events
---

# Events

Every event the three custom modules emit, with its attributes, taken from
`x/{coreslot,rewards,mining}/keeper/events.go`. They are untyped string events: every
attribute value is a string, and amounts are `utwlt` integer strings. No event and no
state record carries a transaction hash.

Only the custom names on this page exist. Names common on other Cosmos chains
(`reward_distributed`, `validator_jailed`, any staking, distribution or governance event)
are never emitted here, so a projection keyed on them never fires.

## Where to find them

An event lands in one of two places in CometBFT's `/block_results`, depending on what
emitted it:

| Emitted by | Appears in | Events |
|---|---|---|
| A transaction | `txs_results[].events` | every event whose trigger below is a transaction |
| The block itself (EndBlock) | `finalize_block_events`, each carrying `mode=EndBlock` | `epoch_finalized`, `params_activated`, `treasury_paid`, `coreslot_validator_update_emitted`, a scheduled `coreslot_key_rotated`, `coreslot_rotation_canceled` with reason `stale_rotation` |

An indexer that reads only transaction results misses epoch finalization and every
validator-set change. The genesis validator set produces no event at all: read it from
the genesis file or from `coreslot-query active`.

Value movement also produces the standard bank events (`coin_spent`, `coin_received`,
`transfer`, and `coinbase` for the epoch mint) next to these. Those are the SDK's own and
are not listed here.

## x/coreslot

**Slot lifecycle** (each is a transaction):

| Event | Attributes |
|---|---|
| `coreslot_registered` | `slot_id`, `operator_address`, `consensus_address`, `new_status` |
| `coreslot_activated` | `slot_id`, `operator_address`, `old_status`, `new_status`, `consensus_address`, `power` |
| `coreslot_inactivated` | `slot_id`, `operator_address`, `consensus_address`, `old_status`, `new_status`, `power` (always `0`), `reason` |
| `coreslot_suspended` | `slot_id`, `operator_address`, `consensus_address`, `old_status`, `new_status`, `power` (always `0`), `reason` |
| `coreslot_removed` | `slot_id`, `operator_address`, `old_status`, `new_status`, `consensus_address`, `reason` |

Status values are the enum names, for example `SLOT_STATUS_ACTIVE`. `reason` on these
three is the free text the signer supplied.

**Consensus keys:**

| Event | Trigger | Attributes |
|---|---|---|
| `coreslot_key_rotation_requested` | a rotation requested for an ACTIVE slot, which waits for `key_rotation_delay_blocks` | `slot_id`, `operator_address`, `old_consensus_address`, `new_consensus_address`, `effective_height` |
| `coreslot_key_rotated` | the rotation takes effect: at once for a slot that is not ACTIVE, in EndBlock at `effective_height` for one that is | `slot_id`, `operator_address`, `old_consensus_address`, `new_consensus_address`, `power`, `effective_height` |
| `coreslot_rotation_canceled` | a pending rotation is dropped | `slot_id`, `operator_address`, `old_consensus_address`, `new_consensus_address`, `reason`, `height` |

`reason` on a cancellation is `lifecycle_change` (the slot was inactivated, suspended or
removed while the rotation waited) or `stale_rotation` (dropped in EndBlock). The name is spelled `canceled`; an
earlier spelling, `coreslot_rotation_cancelled`, is never emitted, and the node has no
alias for it.

**Validator set:**

| Event | Attributes |
|---|---|
| `coreslot_validator_update_emitted` | `slot_id`, `operator_address`, `consensus_address`, `power`, `height` |

One per CometBFT validator update, emitted in EndBlock in consensus-address order. A
`power` of `0` removes the validator. This is the event that mirrors the validator set;
the lifecycle events above record the decisions that lead to it.

**Slot settings** (each is a transaction):

| Event | Attributes |
|---|---|
| `coreslot_payout_updated` | `slot_id`, `operator_address` |
| `coreslot_settlement_updated` | `slot_id`, `operator_address` |
| `coreslot_metadata_updated` | `slot_id`, `operator_address` |
| `coreslot_selection_policy_updated` | `slot_id`, `operator_address`, `policy_version`, `effective_height` |

The first three do not carry the new value; read the slot at the same height to get it.

**Authority and upgrades** (each is a transaction):

| Event | Attributes |
|---|---|
| `coreslot_params_updated` | `authority` |
| `coreslot_authority_nominated` | `authority_role`, `authority`, `nominee` |
| `coreslot_authority_accepted` | `authority_role`, `previous_authority`, `authority` (the new holder) |
| `coreslot_authority_nomination_canceled` | `authority_role`, `authority`, `nominee` |
| `coreslot_upgrade_scheduled` | `authority`, `upgrade_name`, `upgrade_height`, `upgrade_info` |
| `coreslot_upgrade_canceled` | `authority`, `upgrade_name` |

`authority_role` is `AUTHORITY_ROLE_PRIMARY` or `AUTHORITY_ROLE_EMERGENCY`.

Every `consensus_address` in these events is **lowercase hex** of the 20-byte address,
not bech32 `twilightvalcons…`. CometBFT's `/validators` prints the same bytes in
uppercase; `coreslot-query by-consensus` accepts either case.

## x/rewards

| Event | Trigger | Attributes |
|---|---|---|
| `epoch_finalized` | an epoch's final block, in EndBlock | `epoch`, `start_height`, `end_height`, `minted_emission`, `cumulative_emitted`, `reward_pool`, `allocated`, `carry_out`, `eligible_slots`, `distribution_method` |
| `treasury_paid` | the same block, when the treasury share is nonzero | `payout_address`, `amount` |
| `params_update_queued` | `rewards update-params` accepted | `authority` |
| `params_activated` | queued params take effect at an epoch boundary, in EndBlock | — |
| `rewards_paused` | `rewards pause` accepted | `authority` |
| `rewards_resumed` | `rewards resume` accepted | `authority` |

`epoch_finalized` is the signal that the epoch's aggregate and its per-slot entitlements
exist; read them with `rewards-query epoch-reward <epoch>` and
`rewards-query epoch-entitlements <epoch>`. A pause or resume takes effect at the start of
the next block, not in the block that carries the event.

There is no claim event. The claim path is retired and `reward_claimed` is never emitted;
value leaves escrow only through settlement.

## x/mining

| Event | Trigger | Attributes |
|---|---|---|
| `mining_settlement_chunk_submitted` | an accepted chunk | `slot_id`, `epoch`, `chunk_index`, `next_chunk_index`, `recipient_count`, `chunk_total` |
| `mining_settlement_finalized` | a finalized settlement | `slot_id`, `epoch`, `finalization_reason`, `released_remainder`, `finalized_height` |

Nothing is emitted when an epoch's settlement set is created at the epoch boundary, or
when a settlement's deadline passes; read those from state
(`mining-query open-settlements`, `mining-query settlement`).

Mining events are a convenience for a settlement worker, never a requirement. Every
attribute is also readable from state, so a worker that misses events rebuilds from the
queries in [Settlement](../rewards/settlement.md) without losing anything.
