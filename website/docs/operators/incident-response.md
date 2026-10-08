---
title: Incident Response
---

# Incident Response

What a symptom means, how serious it is, and what to do. The table points at the
playbook for each; node-level problems (a node that will not start or sync) are on the
[Node Operator Guide](node-operator-guide.md#troubleshooting), and the safety model is
on [Security & Failure Modes](../rewards/security-and-failure-modes.md).

## Symptom lookup

| Symptom | Likely cause | Check, or go to |
|---|---|---|
| Nodes disagree on the app hash | **critical:** a state fork | [App-hash divergence](#app-hash-divergence-across-nodes-critical) |
| Node stuck, repeated EndBlock error | a fail-closed finalization fault halted the block, by design | [Repeated EndBlock error](#repeated-endblock-error--chain-halted) |
| Epoch not finalizing | the boundary has not been reached | `rewards-query epoch-info`: the height against `current_epoch_end_height` |
| Mint is zero at the boundary | rewards were paused for the epoch, or the subsidy has floored to zero near the cap | `rewards-query pause-state`; `rewards-query next-halving` (`current_block_subsidy`) |
| `cumulative_emitted` not advancing | rewards are paused | [Rewards paused unexpectedly](#rewards-paused-unexpectedly) |
| Releases failing for everyone | rewards are paused | [Releases failing for everyone](#releases-failing-for-everyone) |
| One release rejected | the epoch is not finalized, there is no entitlement, or the amount exceeds what remains | `rewards-query epoch-reward <epoch>`; `rewards-query entitlement <slot-id> <epoch>`; [Settlement](../rewards/settlement.md) |
| Params update rejected | a field no transaction may change, an unsupported feature, or the wrong signer | [Parameters: what update-params rejects](../rewards/params.md#what-update-params-rejects) |
| Wrong params queued | — | [Wrong params queued](#wrong-params-queued) |
| `pause` or `resume` rejected | not signed by the emergency authority | sign with the CoreSlot emergency authority |
| Escrow below what it owes | an accounting defect | [Module-balance coverage failure](#module-balance-coverage-failure) |
| A key is lost or exposed | — | [Key compromise](#key-compromise) |
| Paging returns an empty `next_key` | the last page | normal; stop paging |

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
resume` via the emergency authority. A single rejected settlement transaction is not an
incident; see [Settlement](../rewards/settlement.md) for what each check refuses.

## Wrong params queued

The latest queued `update-params` wins before the boundary. Queue a corrected
update from the authority before the next epoch finalizes. Already-activated
params apply only to the next epoch onward and can be re-queued.

## Module-balance coverage failure

If `module-balances` shows `rewards_balance` below `outstanding_entitlement_liability`
plus `carry_forward_remainder`, stop and
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
