---
title: Transactions
---

# Rewards Transactions

Rewards transactions live under `twilightd rewards <command>` (the generated
`tx rewards` tree offers the same three as `update-rewards-params`, `pause-rewards`
and `resume-rewards`). They wrap the messages `MsgUpdateRewardsParams`,
`MsgPauseRewards` and `MsgResumeRewards` and nothing else; the CLI never infers or
substitutes a signer — the message server enforces the role. The full command tables,
including CoreSlot and settlement transactions, are on the
[CLI Reference](../reference/cli.md#transactions).

## Commands

| Command | Args | Authority required |
|---|---|---|
| `update-params` | `[params-json-file]` | CoreSlot **authority** |
| `pause` | _(none)_ | CoreSlot **emergency authority** |
| `resume` | _(none)_ | CoreSlot **emergency authority** |

The `--from`, `--chain-id`, `--node`, `--gas`, `--fees`, `--yes` flags are the
standard Cosmos tx flags.

## `update-params`

Queues a full `Params` update from a JSON file. The update is **queued** in
`PendingParams` and activates at the **next epoch boundary**; the current epoch
finalizes under its existing configuration.

```bash
twilightd rewards update-params ./params.json --from <authority> \
  --chain-id <chain-id> --node <rpc> --yes
```

The simplest way to produce a valid `params.json` is to query the current params
and edit it:

```bash
# the query wraps the record in {"params": …}; update-params takes the bare record
twilightd rewards-query params --node <rpc> --output json | jq .params > params.json
# only target_block_time_seconds and max_claim_epochs_per_tx may differ, then submit
```

:::warning Almost every field must keep its value
Only `target_block_time_seconds` (informational) and the deprecated
`max_claim_epochs_per_tx` may change. The denom and cap are immutable; the subsidy,
treasury share, treasury address and epoch length are governed by versioned histories
that no transaction writes; the halving mode, distribution method, remainder policy
and fee treasury share are genesis-fixed; the three deprecated enable flags carry no
authority; and enabling fees or weighted rewards is rejected by validation. The full
table is on [Parameters](params.md#rewards).
:::

See [Parameters](params.md) for the full field list and mutability.

## `pause` / `resume`

Toggle the single canonical pause state. There are no per-area selectors: pausing
stops reward accrual and release together.

```bash
twilightd rewards pause  --from <emergency-authority> ...
twilightd rewards resume --from <emergency-authority> ...
```

A transition accepted in block H takes effect at the beginning of H+1, before any
reward sampling, so a block's reward treatment is a property of the block rather
than of the order its transactions happened to execute in.

Pausing does not stop epoch time: epoch numbering advances and epochs still
finalize. A fully paused epoch counts zero reward-enabled blocks and emits
nothing. These commands do not touch pending params or closed epochs.

## Failure cases

| Command | Failure |
|---|---|
| `update-params` | wrong authority; immutable field change; unsupported feature; invalid JSON |
| `pause` / `resume` | wrong emergency authority |
