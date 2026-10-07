---
title: Incident Response
---

# Incident Response

Playbooks for rewards-related incidents. For symptom lookup see
[Troubleshooting](../rewards/troubleshooting.md); for the safety model see
[Security & Failure Modes](../rewards/security-and-failure-modes.md).

## App-hash divergence across nodes (critical)

A state fork. Highest severity.

1. **Stop** affected nodes; do not let them continue producing.
2. Identify the divergence height (`agree.sh` / block headers).
3. Compare `app_state.rewards` and `app_state.coreslot` across nodes at that
   height (e.g. via `twilightd export`).
4. Do not resume until the cause is understood. The rewards path is designed and tested for deterministic execution, so
   divergence suggests a real defect or a non-identical binary/genesis across nodes
   — verify both are identical first.

## Repeated EndBlock error / chain halted

Rewards is fail-closed: a finalization fault halts the block rather than
half-committing.

1. Read node logs for the error returned from `FinalizeBlock`.
2. Confirm the committed height did not advance and no partial epoch was written
   (`epoch-info`, `epoch-reward`).
3. Resolve the underlying fault (e.g. a CoreSlot/rewards state inconsistency).
   A pause is not a tool for this. No transaction can be included while the
   chain is halted, and a pause would not stop finalization anyway: the boundary
   is unconditional (see
   [Security & Failure Modes](../rewards/security-and-failure-modes.md)).

## Rewards paused unexpectedly

Read the canonical pause state, not `params`:

```bash
twilightd rewards-query pause-state --node <rpc> --output json
```

```json
{
  "pause_state": {
    "current_paused": true,
    "has_pending": false,
    "pending_value": false,
    "pending_effective_height": "0"
  },
  "release_enabled": false
}
```

`current_paused` is the state in force; `has_pending`, `pending_value` and
`pending_effective_height` describe a pause or resume that takes effect at the
next block; `release_enabled` is whether settlement releases are accepted now.

:::warning Do not read `params` for this
`emissions_enabled`, `epoch_settlement_enabled` and `claims_enabled` in
`rewards-query params` are retired fields that carry no authority. They read
`true` on a paused chain.
:::

If paused and unintended, the emergency authority resumes:

```bash
twilightd rewards resume --from <emergency-authority-key> --chain-id <chain-id> --node <rpc>
```

A pause is global, so there are no flags to match, and `rewards pause` and
`rewards resume` take none. Both take effect at the next block. Blocks produced
while paused earned nothing and are not repaid; the settlement clock was frozen
for the same blocks, so no settlement window was consumed. Epochs that closed while
paused finalized at their boundary as usual, with only the blocks before the pause
counted.

## Releases failing for everyone

Check the canonical pause state (`rewards-query pause-state`, above). If paused
intentionally (incident containment), communicate the window; if not, `rewards
resume` via the emergency authority. Individual settlement rejections are not
incidents — see [Troubleshooting](../rewards/troubleshooting.md).

## Wrong params queued

The latest queued `update-params` wins before the boundary. Queue a corrected
update from the authority before the next epoch finalizes. Already-activated
params apply only to the next epoch onward and can be re-queued.

## Module-balance coverage failure

If `module-balances.rewards_balance` < outstanding entitlements + carry, stop and
investigate — this should not occur under the rewards accounting invariants (the
coverage invariant holds after every finalize and every release). Treat as a
critical accounting defect.

## Key compromise

- **Emergency key:** can pause (a denial of service) and suspend slots. Rotate the
  role (nominate a fresh key and accept, ideally in one block); resume any unwanted
  pause; reactivate any slot it suspended.
- **Authority key:** can change the validator set — and with it who earns future
  emission — change CoreSlot params, and schedule an upgrade at a height; it cannot
  move value already escrowed, and a rewards params update changes only informational
  fields. Rotate the role the same way; cancel any plan you did not schedule
  (`coreslot cancel-upgrade`); review the slot set and reverse admissions you did not
  make. See
  [what each key controls](../rewards/security-and-failure-modes.md#what-each-key-controls-and-what-to-do-if-it-is-lost).
