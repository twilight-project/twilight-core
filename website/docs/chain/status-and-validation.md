---
title: Status & Validation
---

# Status & Validation

## Status

Twilight Core is under active development. It runs today as **Twilight Testnet**,
a public testnet whose tokens have no value. The current implementation provides the
**CoreSlot** Proof-of-Authority validator lifecycle and a bounded **Rewards**
emission and settlement system denominated in `utwlt`. It is **not yet mainnet-ready**
and has **not undergone an external security audit**.

This page is a plain-language summary of what has been validated, and how — and,
just as importantly, what has not. It is engineering validation evidence, not an
audit or a mainnet-readiness certification.

## Validated behavior

Each behavior below is exercised by the evidence type named next to it.

| Behavior | How it is checked |
|---|---|
| CoreSlot is the single source of validator-set updates | deterministic keeper tests and randomized state-machine simulation |
| CoreSlot lifecycle transitions enforce authorization, active-set floor/cap rules, and validator metadata retention | deterministic keeper tests and randomized state-machine simulation |
| Bounded emission — supply-threshold halving, max-supply clipping, and terminal behavior | deterministic keeper tests with exact numeric vectors |
| Epoch finalization and active-block reward allocation (`amount = pool × blocks_active / Σ blocks_active`) | keeper tests and integration drills |
| Carry-forward remainder accounting | keeper tests and branch-coverage drills |
| Treasury split, when enabled | keeper tests and branch-coverage drills |
| Entitlement release — over-release rejection, release against a missing entitlement, and payment to the snapshotted payout address | keeper tests and integration drills |
| Settlement end to end — an epoch's entitlement released to participants by chunk, then finalized with the remainder returned to the operator | application-level end-to-end test with exact economics |
| Rewards accounting identity — treasury, released rewards, unreleased entitlements, and carry-forward reconcile to cumulative emitted supply | invariant tests checked against real module, treasury, and payout balances |
| Genesis export / import round trip: a populated genesis (non-default state, a queued params update; no closed epoch) exported through the app's module manager byte-for-byte and re-imported with its state preserved | application-level round-trip test |
| An export of a running chain is complete (archive, entitlements, liability, supply, escrow) and is refused by the fresh-genesis importer, naming the closed-epoch state | application-level test |
| Local multi-node epoch finalization, with cross-node app-hash agreement | four-node localnet validation |
| Long-run determinism and exact accounting over many epochs on a zero-premine chain | endurance soak testing |
| Cross-host fault tolerance — peer loss, network partition, and quorum-loss safe-halt, each with recovery | fault-tolerance drills on a live multi-host network |
| Off-happy-path economic branches (halving crossing, non-zero carry, non-uniform participation, treasury, active-set churn) | branch-coverage drills |
| Module invariants across long, random operation sequences | randomized state-machine simulations |

## Known limitations

- **Not externally audited.** No independent security review has been performed.
- **Not mainnet-ready.** This is a public testnet; any network carrying value
  requires an external audit and further deployment validation beyond what is
  listed above.
- **An export of a running chain cannot be re-imported.** `twilightd export` is
  complete, but every Twilight module's importer accepts only a fresh genesis; a continuation
  importer is deferred. Recovery is from a node's own data and backups, never from an
  export. See [Upgrade & Export/Import](../operators/upgrade-and-export-import.md#an-export-cannot-be-re-imported-by-this-binary).
- **Multi-day cross-host endurance is not yet done.** Cross-host coverage to date
  is the fault-tolerance drills above plus single-host endurance soak; a sustained
  multi-day run across hosts is still pending.
- **On-chain upgrades are not yet exercised on a public network.** `x/upgrade` is
  wired and scheduled only by the CoreSlot authority; the mechanism is covered by
  application tests and a four-validator localnet drill with two separately built
  binaries. Not yet exercised: store-layout changes (adding, renaming or deleting a
  store) and Cosmovisor itself, which the drill swaps by hand. See
  [Upgrade & Export/Import](../operators/upgrade-and-export-import.md).
- **Weighted rewards and fee-funded rewards are not active.** They are code-gated
  and rejected until implemented. A slot's reward weight is CoreSlot metadata
  only, is not recorded on an entitlement, and has no payout effect — payout is by
  [active-block participation](../rewards/economics.mdx).
- **Code-gated behavior is not user-facing functionality.** Anything not enabled
  in the current implementation is simply not available.

## Where the evidence lives

Detailed evidence is maintained in the repository's test suites and curated
validation reports. The main locations are `x/coreslot/`, `x/rewards/`, `app/`,
`scripts/localnet/`, and `docs/testing/validation-summary.md`.
