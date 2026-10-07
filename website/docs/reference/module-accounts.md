---
title: Module Accounts
---

# Module Accounts

The chain has **five** module accounts, declared once in `app/config.go` and created by
`auth` at genesis. The same declaration feeds the economic-address rule, so a module
account is always **refused as a payee**: a participant payout, a settlement address or
a treasury address naming one is rejected (`mining-query validate-economic-address`
reports why).

| Account | Permission | Holds | Used by |
|---|---|---|---|
| `rewards` | `Minter` | minted epoch emission; every unreleased entitlement and the carry-forward remainder | the only minter: mints at finalization; sends on settlement release and on the treasury share |
| `rewards_fee_pool` | _(none)_ | nothing while fees are disabled | reserved for future fee plumbing; dormant |
| `fee_collector` | _(none)_ | nothing: the chain charges no fees | the SDK's standard fee destination, present because `auth` requires it |
| `coreslot-authority` | _(none)_ | nothing; no key exists for it | the authority the SDK modules (`auth`, `bank`, `consensus`, `upgrade`) are bound to, so none of their own authority messages can be signed; CoreSlot's `ScheduleUpgrade`/`CancelUpgrade` is the only path to `x/upgrade`. Also what `twilightd init` writes as both CoreSlot authorities until a launch sets real ones |
| `coreslot-emergency` | _(none)_ | nothing; no key exists for it | the placeholder `twilightd init` writes as the emergency authority |

No account has `Burner` or `Staking` permissions, and no staking, distribution, slashing
or governance module accounts exist.

## Addresses

A module account's address is derived from its name, so it is the same on every network
that uses the `twilight` bech32 prefix:

| Account | Address |
|---|---|
| `rewards` | `twilight1245yut9zht8q4hz39sd0lzqtzkuw5us5pd3c3u` |
| `rewards_fee_pool` | `twilight15vlqpeqkdq9a5zk8x0dhnc3duddqdpz8p738kh` |
| `fee_collector` | `twilight17xpfvakm2amg962yls6f84z3kell8c5ltxtf5t` |
| `coreslot-authority` | `twilight17te68tpa0etfn4cmlqryw06uqh5qc2tp2fracm` |
| `coreslot-emergency` | `twilight1z56ffxw0fdrzvd0n57vxeukm5alazthcyuezug` |

Read them from a node rather than trusting a table:

```bash
twilightd query auth module-accounts --node <rpc> --output json
twilightd rewards-query module-balances --node <rpc>
# { "denom": "utwlt", "rewards_balance": "...", "fee_pool_balance": "0",
#   "outstanding_entitlement_liability": "...", "carry_forward_remainder": "..." }
```

## The `rewards` account is the escrow

At finalization the epoch emission is minted into `rewards`; the configured treasury
share, if any, is sent out of it in the same step; the rest stays as entitlements until
[settlement](../rewards/settlement.md) releases it to participants and to each slot's
snapshotted payout address. The module-balance-coverage invariant requires its balance
to cover the outstanding entitlement liability plus the carry-forward remainder at all
times; `rewards-query module-balances` reports all three.
