---
title: Localnet & Drills
---

# Localnet & Drills

Every end-to-end target in the `Makefile`. Each localnet target starts its own
throwaway network (four nodes unless noted), exercises it, and stops it. Most drive it
with real signed transactions and check cross-node app-hash agreement after the
transition under test; the rows say which do not. None of them runs in CI; the first six
[chain-free checks](#chain-free-checks) at the end are the part that does.

Homes can be isolated and the port band can be moved (see [Ports](#ports)), but several
drills stop every `twilightd start` process on the machine when they exit: run one localnet
at a time, and never beside a node you care about.

Before a change to a consensus or economic path, run the four the repository asks for:

```bash
make localnet-smoke                 # 4-node startup and agreement
make localnet-rewards-epoch-smoke   # epoch finalization and entitlements
make localnet-settlement-smoke      # real settlement payouts
make drills                         # lifecycle, restart-rotation and quorum drills
```

## Smoke

| Target | Covers |
|---|---|
| `make localnet-smoke` | startup on the default profile and app/validators/next-validators hash agreement; never closes an epoch and submits no transaction |
| `make localnet-rewards-epoch-smoke` | one 360-block epoch: exact minting, allocation, the entitlements it creates, agreement before and after; submits no transaction, so releases no value. `localnet-rewards-smoke` is the same target under its old name |
| `make localnet-settlement-smoke` | the money-movement proof: real `submit-settlement-chunk` and `finalize-settlement` transactions paying participants, across more than one epoch |
| `make api-smoke` | the REST and Swagger routes, against a throwaway network with REST enabled and a seeded consensus-address reservation |
| `make localnet-init`, `make localnet-agree` | build a network without starting it; check agreement on one that is already running |

### What the epoch smoke produces

`scripts/localnet/rewards-smoke.sh` edits only its own isolated genesis; production
defaults are untouched. The epoch length is bounded to [360, 720] blocks and cannot be
shortened, so the smoke closes a full 360-block epoch at a fast block time instead.

| Stage | Result |
|---|---|
| Pre-finalization | epoch 1 open; 4 active-block rows; module balance 0; 4-node hash agreement |
| After finalization | epoch advances to 2; minted `149,828,400utwlt` (= 360 × 416,190); 4 entitlements × `37,457,100utwlt`; escrow holds the full emission; carry 0; 4-node hash agreement |

The minted emission is **per block over the epoch** (`360 × 416,190`), not per
slot — distribution then splits the minted pool across the 4 active slots. See
[Rewards economics](../rewards/economics.mdx).

:::warning Funded development fixture
The rewards smoke runs on a **funded** development fixture (the localnet funds two
accounts with `1,000,000,000,000utwlt` each, so total supply after finalization is
`2,000,149,828,400utwlt`). It exercises deterministic rewards behavior and exact
supply accounting **under that fixture**. A production **zero-premine**
monetary-genesis run is a separate case — see
[Status & Validation](../chain/status-and-validation.md).
:::

### The agreement check

`scripts/localnet/agree.sh` queries every node and verifies they agree on the
**app hash**, **validators hash**, and **next-validators hash** at a common
height. App-hash divergence is the catastrophic failure it guards against — a
silent state fork. The epoch smoke runs it before and after finalization; `localnet-smoke` and the
settlement smoke run it once, at the end; most drills run it after the transition they
test.

## Validator set

| Target | Covers |
|---|---|
| `make drills` | the three drills below, in sequence, stopping at the first that fails |
| `make drill-lifecycle` | register, activate, inactivate, suspend, remove and rotate through the CLI, checking after every action that its block carries exactly the expected number of validator updates |
| `make drill-restart-rotation` | rotate an active validator's key, restart the node on the new key, and prove it rejoins at full power while the old key holds none |
| `make drill-quorum` | by stopping nodes rather than by transactions: lose one of four validators and continue; lose two and halt without forking; restart and resume. An offline validator keeps its slot and its power |
| `make localnet-validator-growth` | a chain that grows from one validator to five, each node syncing before it is admitted |
| `make localnet-validator-departures` | the four ways a validator leaves (offline, inactivated, suspended, removed), the guards on the way down, and a key rotation with no quorum margin; no cross-node hash check |
| `make localnet-quorum-table` | builds sets of each size and degrades them by stopping nodes; writes `docs/testing/quorum-threshold-table.md` |
| `make validator-set-study` | the last three together |

## Settlement

| Target | Covers |
|---|---|
| `make localnet-join-and-settle` | two nodes: one slot, then a second joins; both earn, and each operator settles its own entitlement from its own node; no cross-node hash check |
| `make localnet-settlement-matrix` | three nodes and three slots over three epochs: membership changes with settlements outstanding, every settlement bound, and both finalization arms including the deadline. Long: three epoch boundaries plus a 720-block window; no cross-node hash check |

## Authority and upgrades

| Target | Covers |
|---|---|
| `make localnet-authority-rotation-drill` | two-step rotation of both authority roles: nominate, prove the incumbent still acts and the nominee does not, accept, prove the roles swapped, and prove a params update cannot rotate either; no cross-node hash check |
| `make localnet-upgrade-drill` | a coordinated `x/upgrade` across four validators and two binaries: every validator halts at the same height, upgraded nodes agree across the boundary, a node left on the old binary halts, and downtime does not consume the settlement clock |
| `make release-upgrade-rehearsal PROFILE=…` | qualifies a published release for upgrade to the candidate through the production handler, with a partial rollout and a straggler. Each profile in `scripts/localnet/lib/rehearsal-profiles.sh` pins the release (tag and commit), the upgrade name and what the upgrade must deliver; today `v0.1.0-to-v0.2.0`. Slow; needs `gh` and free ports |

## State, genesis and load

| Target | Covers |
|---|---|
| `make localnet-export-restore-drill` | after two epochs and a settlement: an export taken mid-epoch, a restore attempt classified as refused-as-designed, supported or defect, and a fresh node joining |
| `make check-genesis GENESIS=…` | verifies a genesis file before it is distributed, against launch decisions passed as `GC_*` variables (it refuses to infer them); see [Genesis](../reference/genesis-reference.md) |
| `make localnet-block-gas-drill` | a network with a finite `block.max_gas` under flood: the ceiling holds, the excess is deferred rather than dropped, and blocks keep coming. The ceiling is a drill constant, not a production value |
| `make localnet-load-calibration` | the measurement a `block.max_gas` value would be chosen from; targets an existing network and writes machine-readable results; when the measurement qualifies it reports a candidate value for human review. It ratifies nothing |
| `make localnet-rewards-soak` | runs until `SOAK_EPOCHS` epochs (default 3) of 360 blocks have closed, asserting determinism and accounting at the boundaries. Along the way it does an emergency pause and resume, an authority params update (`SOAK_EPOCHS` of at least 5 to see it activate) and a node restart (at least 5). `PREMINE=off` empties the genesis balances, so supply rises only from emission. No settlement |

## Chain-free checks

Fast, and no network is started. The first six run in CI's *build & test* job.

| Target | Proves |
|---|---|
| `make check-cli-surface` | the CoreSlot transaction commands against a real binary, including that retired names fail loudly |
| `make check-vulncheck-pin` | the vulnerability scan runs under the Go version `go.mod` declares |
| `make release-upgrade-faults` | the release rehearsal's readers fail on bad input instead of reporting success |
| `make block-gas-faults` | the same for the block-gas tooling |
| `make check-genesis-faults` | every genesis check fires on its own fault |
| `make upgrade-drill-faults` | every upgrade-drill predicate that can decide a pass fails on bad input |
| `make localnet-export-restore-faults` | the export-restore drill's outcome classifiers |
| `make check-release-stamping` | a release binary's version stamp, including the dirty-tree marker; needs a clean tree |

## Ports

Node *i* of a localnet listens at `26657 + i×100` (CometBFT RPC, on loopback),
`26656 + i×100` (P2P, on every interface — CometBFT's default) and `9090 + i×100` (gRPC,
on localhost). The two targets that enable the REST API, `api-smoke` and the rewards
fixture, do so on node 0 at `1317`. `scripts/localnet/lib/ports.sh` is the one definition
of that scheme, and every script derives its endpoints from it. CometBFT's pprof listener
(`localhost:6060` in every generated `config.toml`) is outside it.

`LOCALNET_PORT_BASE` adds a constant to every family, so a host that already carries a
node on the default ports can run its drills on an unused band instead:

```bash
LOCALNET_PORT_BASE=3000 make localnet-smoke   # RPC 29657…, P2P 29656…, gRPC 12090… (REST, where enabled, 4317)
```

Set it for every command that touches the same localnet (`localnet-init`,
`localnet-agree`, the fixture, …); nothing records the base in the home. The default,
`0`, is the scheme these scripts have always used. The largest accepted value is `37578`,
past which the highest port in use would exceed 65535; a value with a leading zero is
refused rather than read as octal; and a base of `4810` or less keeps every port this
tooling binds, the export-restore band included, below the Linux ephemeral range (32768
and up), where an outbound connection can briefly hold a port. The second chain the
export-restore drill starts sits a fixed distance above the ordinary band and moves with
it.

Five drills refuse to start while *any* `twilightd` process exists, whatever the band,
because their exit trap stops every `twilightd` on the machine: `localnet-quorum-table`,
`localnet-validator-growth`, `localnet-validator-departures`, `localnet-join-and-settle`
and `localnet-settlement-matrix`. `localnet-settlement-smoke` refuses too, for isolation
(it asserts on the exact number of node processes), though it stops only its own nodes.
Beside a node you care about, those six belong in a network namespace or a container, not
on another band.

**A localnet refuses to start on a port that is already in use.** Before the binary is
built or the localnet home is touched, every script that starts nodes checks the ports it
will bind: `init.sh` for the nodes it creates, and the drills that add nodes later, start
a second chain, resume without `init.sh` or enable REST for those ports as well. A port
counts as busy when `lsof` shows a listener on it *or* a TCP connect to it on loopback
succeeds — the second check sees a loopback listener whoever owns it, which `lsof` as an
ordinary user cannot. A busy port is re-checked for about `LOCALNET_PORT_GRACE_SECONDS`
(default 5) so that a node still exiting is not mistaken for a live one; one that stays
busy stops the run with the busy ports named. No `lsof` to check with is refused as well,
with its own message. Nothing else arbitrates these ports, so without the check a drill
could bind over a live node, or a live node could come up on a drill's port.
`LOCALNET_SKIP_PORT_CHECK=1` turns the check off (the port-range check still runs); it is
for a network namespace or container where nothing else can be on the ports, and for
nowhere else.

## Adding a drill

- Reuse the harness: `scripts/localnet/init.sh`, `start.sh`, `stop.sh`, `agree.sh` (each
  takes `TWILIGHT_LOCALNET_HOME` for its home and `LOCALNET_PORT_BASE` for its ports —
  derive endpoints from `scripts/localnet/lib/ports.sh`, never from `26657 + i*100`) and
  `scripts/localnet/lib/`.
- Drive the chain with real signed transactions, not in-app hooks or hand-edited state.
- A signer's account must exist on chain: give it a genesis account, even with a zero
  balance. The chain needs no fee, so a zero balance is enough to sign.
- Assert agreement **after** the transition under test, not only before it.
- Add a `Makefile` target, and list it here.
