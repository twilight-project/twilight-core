---
title: Queries
---

# Rewards Queries

All rewards queries are **read-only** and live under `twilightd rewards-query <command>`
(the generated `query rewards` tree reaches the same handlers under RPC-style names).
Add `--node <rpc>` to target a node and `--output json` for machine output. The
complete table, with arguments, response fields and REST routes for every command, is on
the [CLI Reference](../reference/cli.md#rewards-query).

## What each command answers

| Command | Args | Question it answers |
|---|---|---|
| `params` | — | What are the rewards parameters? |
| `epoch-info` | — | Which epoch is open, when did it start, when does it end, how many reward-enabled blocks has it counted, is a params update queued? |
| `epoch-boundaries` | `[epoch]` | What heights did epoch N span, and how long was it? |
| `epoch-reward` | `[epoch]` | What did epoch N mint, pool, allocate and carry? (`NotFound` until it is finalized) |
| `next-halving` | — | Which tier, which subsidy, how far to the next halving? |
| `cumulative-emitted` | — | How much has ever been minted, against what cap? |
| `supply-schedule` | — | Params and the halving view together |
| `current-active-blocks` | — (paginated) | Which slots have been credited how many blocks in the open epoch? |
| `module-balances` | — | What does the `rewards` escrow hold, what does it owe (`outstanding_entitlement_liability`), and what is carried forward? |
| `entitlement` | `[slot-id] [epoch]` | What did slot S earn in epoch N, how much of it is released, and to which snapshotted payout address? |
| `epoch-entitlements` | `[epoch]` (paginated) | All entitlements of epoch N |
| `reward-config-versions`, `reward-config-version` | —; `[version]` or `epoch:N` | The reward-configuration history (subsidy, treasury), or the version binding an epoch |
| `epoch-config-versions` | — (paginated) | The epoch-configuration history (epoch length) |
| `pause-state` | — | Is accrual and release paused, is a transition pending, and may settlement release right now? |

## Examples

```bash
twilightd rewards-query epoch-info --node <rpc>
twilightd rewards-query epoch-reward 1 --node <rpc>
twilightd rewards-query entitlement 1 1 --node <rpc>
twilightd rewards-query epoch-entitlements 1 --limit 10 --node <rpc>
twilightd rewards-query pause-state --node <rpc>
twilightd rewards-query module-balances --node <rpc>
twilightd rewards-query reward-config-version epoch:1 --node <rpc>
```

## Pagination

`current-active-blocks`, `epoch-entitlements`, `reward-config-versions` and
`epoch-config-versions` are paginated (their collections grow over time) and accept the
standard flags:

| Flag | Meaning |
|---|---|
| `--limit` | Max rows per page (default 100) |
| `--offset` | Numeric offset |
| `--page` | Page number (offset = page × limit) |
| `--page-key` | Continuation key from a previous response's `next_key` |
| `--count-total` | Include total count |
| `--reverse` | Descending order |

The other commands take no pagination flags. Ordering is deterministic:
`epoch-entitlements` returns ascending slot id within the requested epoch;
`current-active-blocks` returns ascending slot id; the version histories ascend by
version. An offset and a page key supplied together are refused as `InvalidArgument`.

## Reading the answers

- **`epoch-info`** → `state.current_epoch`, `current_epoch_start_height`,
  `current_epoch_end_height`, `current_epoch_length_blocks`, `open_reward_enabled_blocks`,
  `has_pending_params`. At an epoch's closing height `current_epoch` is still that epoch:
  the next one opens at the following BeginBlock.
- **`epoch-reward`** → `epoch_reward.minted_emission`, `reward_pool`, `allocated_amount`,
  `carry_out`, `cumulative_emitted_after_epoch`. The embedded `rewards[]` is always
  empty; the obligation an epoch creates is a slot entitlement.
- **`entitlement`** → `entitlement_amount`, `released_amount`, `payout_address`; what is
  still owed is the difference, and [settlement](settlement.md) is what releases it.
- **`pause-state`** → `pause_state.current_paused`, `has_pending`, `pending_value`,
  `pending_effective_height`, and `release_enabled`, which is what settlement checks.
  Never read the deprecated enable flags in `params` for this.
- **`module-balances`** → `rewards_balance` must cover
  `outstanding_entitlement_liability + carry_forward_remainder`; the invariant that
  enforces it is on [Invariants](invariants.md).

Errors follow one classification on every query: a caller's mistake is
`InvalidArgument`, an absent record is `NotFound` (an empty list is a success), and state
that exists but cannot be read is `Internal` — see the
[CLI Reference](../reference/cli.md#errors).
