---
title: Genesis
---

# Genesis

The localnet fixtures described on this page fund accounts and are **not** a
production genesis. Production monetary genesis is expected to be zero-premine —
total supply rising only through epoch emission. See
[Status & Validation](status-and-validation.md) for maturity status.

## Module init order

`InitGenesis` runs in this order (set in `app/config.go`):

```text
upgrade → auth → bank → consensus → coreslot → rewards → mining
```

`upgrade` goes first; it carries no state in the genesis file and only records each
module's consensus version, which a later upgrade handler migrates from. `auth` and
`bank` initialize next so module accounts exist before rewards genesis runs. CoreSlot precedes rewards, and mining comes last: it is the consumer of both
custom modules. Neither rewards nor mining `InitGenesis` mints or sends — they only
write state; mining also rebuilds its derived indexes (the open-settlement index and
the version indexes) from the rows it imported rather than importing them.

## Module accounts created at genesis

| Account | Permission |
|---|---|
| `rewards` | `Minter` |
| `rewards_fee_pool` | _(none)_ |

These are created by the `auth` module from `ModuleAccountPermissions` in
`app/config.go`, not lazily. See [Module Accounts](../reference/module-accounts.md).

## Rewards default genesis

Fresh default rewards genesis (from `x/rewards/types/defaults.go`):

- `params` — production defaults (see [Parameters](../rewards/params.md)).
- `state` — `current_epoch = 1`, `current_epoch_start_height = 1`,
  `cumulative_emitted = "0"`, `carry_forward_remainder = "0"`.
- `current_epoch_config` — the epoch snapshot built from `params`.
- no pending params, no finalized epochs, no slot entitlements.

No premine: default genesis sets `cumulative_emitted = 0` and adds no rewards
balances. Total supply begins at zero and rises only through emission.

The full schema and a sample JSON are in
[Genesis Reference](../reference/genesis-reference.md).

## Mining default genesis

Fresh default mining genesis (from `x/mining/types/genesis.go`) describes a
trusted-distribution chain whose first epoch is 1:

- one version in each history, effective from epoch 1: the distribution mode
  (`TRUSTED_AS_DISTRIBUTION`), the settlement parameters (window 2 epochs, 32
  recipients per chunk, 4 chunks per settlement, minimum payout `10000utwlt`), and
  the selection parameters;
- no scheduled changes;
- `settlement_clock = 0` and `last_processed_reward_epoch = 0` — both are required
  to be zero on a fresh genesis;
- no settlement epoch anchors and no settlements.

See [Settlement](../rewards/settlement.md) for what each of these governs.

## Inspecting genesis

```bash
twilightd init <moniker> --chain-id <chain-id>
# the generated genesis includes the default genesis of every wired module
jq '.app_state.coreslot' ~/.twilightd/config/genesis.json
jq '.app_state.rewards' ~/.twilightd/config/genesis.json
jq '.app_state.mining' ~/.twilightd/config/genesis.json
```

## Localnet fixtures (not production)

The localnet `init.sh` funds the authority and emergency accounts with
`1,000,000,000,000utwlt` each and registers four active CoreSlots. This is a
development fixture: it is funded (premined) and is **not** the production
zero-premine genesis. See [Localnet](localnet.md).
