---
title: Rewards Operator Guide
---

# Rewards Operator Guide

How to watch the rewards module day to day, and the commands to do it. Metrics and
alerts are on [Monitoring](monitoring.md); faults are on
[Incident Response](incident-response.md).

The examples read the node from a variable, so they paste as written:

```bash
export NODE=tcp://127.0.0.1:26657
```

## What you are watching

Each epoch, the chain mints a `utwlt` pool and allocates it to the active slots by the
blocks each was active for. Each slot's share is held in escrow as a per-slot
entitlement; there is no claim transaction. Settlement in `x/mining` releases it,
paying participants and returning the remainder to the payout address snapshotted
when the epoch closed (see
[Settlement](../rewards/settlement.md)). Your job is to confirm each epoch finalizes
correctly, that settlement keeps up, and to respond to pauses and faults.

## After each epoch boundary

1. **Epoch advanced:** `epoch-info` shows `state.current_epoch` incremented.
2. **Emission correct:** `epoch-reward <closed-epoch>` shows a `minted_emission` equal
   to the block subsidy times its `reward_enabled_blocks` (except in an epoch that
   crosses a halving threshold or reaches the cap, where the subsidy changes
   mid-epoch), and `cumulative-emitted` advanced by that amount.
3. **Allocation reconciles:** `allocated_amount` (the sum of the epoch's entitlements)
   plus `carry_out` equals the epoch's `reward_pool`.
4. **Escrow covers the liability:** right after a finalization, `module-balances`
   shows `rewards_balance` equal to `outstanding_entitlement_liability` plus
   `carry_forward_remainder`.
5. **Nodes agree** (on a multi-node network): every node reports the same app hash at
   the same height; see
   [Check status and agreement](node-operator-guide.md#check-status-and-agreement).

```bash
twilightd rewards-query epoch-info --node "$NODE"
twilightd rewards-query epoch-reward <epoch> --node "$NODE"         # rewards[] is always empty
twilightd rewards-query epoch-entitlements <epoch> --node "$NODE"
twilightd rewards-query cumulative-emitted --node "$NODE"
twilightd rewards-query module-balances --node "$NODE"
twilightd rewards-query current-active-blocks --limit 100 --node "$NODE"   # the open epoch so far
```

Add `--output json` for machine-readable output.

## Settlement keeping up

These show a slot's open settlements and, for each, its `deadline_clock`,
`current_settlement_clock` and `permissionless_finalization_now`;
[Settlement](../rewards/settlement.md) explains the deadline:

```bash
twilightd mining-query open-settlements <slot-id> --node "$NODE"
twilightd mining-query settlement <slot-id> <epoch> --node "$NODE"
```

Once the settlement clock reaches a settlement's `deadline_clock`
(`permissionless_finalization_now: true`), any account may submit
`finalize-settlement`. Nothing finalizes a settlement automatically, and nothing
finalizes while rewards are paused. The remainder goes to the payout address
snapshotted when the epoch closed.

## Halving

The per-block subsidy halves as cumulative emission crosses supply thresholds:

```bash
twilightd rewards-query next-halving --node "$NODE"
twilightd rewards-query supply-schedule --node "$NODE"
```

Near the cap the subsidy can floor to zero: emission legitimately stops while the
cumulative total stays below the cap (see
[Economics](../rewards/economics.mdx#halving-and-the-supply-cap)).

## Authority actions

| Action | Command | Signed by |
|---|---|---|
| Queue a params update | `twilightd rewards update-params ./params.json --from <authority>` | CoreSlot authority |
| Pause | `twilightd rewards pause --from <emergency-authority>` | CoreSlot emergency authority |
| Resume | `twilightd rewards resume --from <emergency-authority>` | CoreSlot emergency authority |

A pause stops reward accrual and release together, from the next block. Epochs still
advance and finalize; a fully paused epoch emits nothing. See
[Transactions](../rewards/transactions.md), the full table of pause effects on
[Epoch Lifecycle](../rewards/epoch-lifecycle.md#pause-interactions), and the
[Authority & Emergency Guide](authority-and-emergency-guide.md).

## When something looks wrong

[Incident Response](incident-response.md) maps each symptom to its severity and the
first thing to do.
