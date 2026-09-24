---
title: Monitoring
---

# Monitoring

What to watch, and how to read it. The node exports the chain's economic and
validator-set state as Prometheus gauges through the Cosmos SDK telemetry facility;
the same signals remain available through queries and the node RPC for one-off
inspection.

## Enabling the exporter

Two settings, in two files. The SDK telemetry facility produces the series; CometBFT's
existing instrumentation endpoint serves them. **Validators need no API server.**

**Edit the existing keys in place — do not append these blocks.** Every node home
already has a `[telemetry]` table in `app.toml` and an `[instrumentation]` table in
`config.toml`, with all of the keys below present. Pasting a second `[telemetry]` or
`[instrumentation]` header makes every `twilightd` command, `start` included, fail with
`toml: table telemetry already exists`, and the node stays down until the file is
fixed. On a four-validator network, two nodes down at once halts the chain. Before
restarting, check the edited files parse with a harmless command such as
`twilightd comet show-node-id --home <home>`, which fails fast on a TOML error.

In `app.toml`, change these values in the existing `[telemetry]` table:

```toml
[telemetry]
enabled = true
# Seconds a series is retained without being refreshed. The Prometheus sink is only
# created when this is > 0. Every gauge below is refreshed once per commit, so keep
# this comfortably above the block interval.
prometheus-retention-time = 60
# Keep both false: a hostname prefix changes the NAME of every gauge below to
# <host>_twilight_..., which breaks every rule written against them. Use
# enable-hostname-label = true if you want the host as a label instead.
enable-hostname = false
enable-hostname-label = false
# Leave empty for the same reason: a service name is prepended to every metric name.
service-name = ""
# Attached to every series. The chain id is the one label a fleet dashboard needs.
# The generated file writes this as `global-labels = [` and `]` on two lines; replace
# both lines with this one.
global-labels = [["chain_id", "twilight-testnet-1"]]
```

In `config.toml`, change these values in the existing `[instrumentation]` table, on a
**private** interface:

```toml
[instrumentation]
prometheus = true
prometheus_listen_addr = "10.0.0.5:26660"
```

The `prometheus_listen_addr` that `twilightd init` writes is `":26660"`, which
binds **every** interface. Setting `prometheus = true` alone is therefore not enough:
you MUST also override the address to the private one, or the endpoint is public.

**A wrong address fails silently.** If the address is not assigned on the host, the
node keeps producing blocks and the only sign is one log line
(`Prometheus HTTP server ListenAndServe err="... can't assign requested address"`).
After every restart, `curl http://<addr>/metrics | grep twilightd_build_info` from the
monitoring host. On a host with no private interface, bind `127.0.0.1` and scrape
through a tunnel or local agent, or bind the public address behind a firewall rule
that admits only the monitoring host.

The SDK sink registers in the process-global Prometheus registry, and CometBFT's
`prometheus_listen_addr` endpoint serves that whole registry. So the one endpoint
carries CometBFT's consensus metrics (block height, rounds, peers, mempool) **and**
every `twilight_*` series below, with no extra listener:

```yaml
- job_name: twilightd
  static_configs:
    - targets: ["10.0.0.5:26660"]
```

**Never bind either endpoint to `0.0.0.0`.** `twilightd_build_info` carries the
version and commit, and the instrumentation endpoint also exposes peer and mempool
detail; both belong on the monitoring network only.

**Enabling telemetry also publishes `/metrics` on any API server the node runs** —
there is no separate switch (the SDK attaches telemetry to the API server whenever
`[telemetry] enabled = true`). A node whose API is reachable from the internet, such as
a public REST endpoint behind a reverse proxy, therefore starts serving
`twilightd_build_info`, every `twilight_*` series and Go runtime statistics to anyone
the moment telemetry is turned on. On such a node, **deny `/metrics` at the proxy
before enabling telemetry** (for nginx: `location = /metrics { return 404; }` in the
public server block), or leave telemetry off there and scrape another node.

Full nodes that already run the API server have a second option: with the same
`[telemetry]` block, the API server serves the series at
`/metrics?format=prometheus` on `[api] address`. That address defaults to
`localhost` — leave it there or on a private interface, and treat it like the RPC,
since it is also the REST surface. Without `format=prometheus` (the default, and
`format=text`) the endpoint returns the SDK's in-memory sink as JSON, which
Prometheus cannot scrape; with `prometheus-retention-time = 0` it returns 400.

### Where the gauges come from

Every `twilight_*` gauge is set **after `Commit`**, from committed state, by a path
that returns nothing into the block. It cannot emit an event or a validator update,
because the block that could carry them is over. It cannot write state because it
reads through a **cache of the committed multistore that is never written back**: a
write attempted through it stops in the cache and is discarded with it. Two tests
pin this: one commits a multi-epoch chain with telemetry disabled and enabled and
compares every app hash; one traces every store operation at the root multistore
while the exporter runs — in the normal, pause-pending and paused states — and
requires that none is a write or delete. A gauge that cannot be read is skipped for
that block and counted in `twilight_telemetry_read_failures_total`.

Values are carried as `float32` by the SDK telemetry API: exact below 2^24, rounded
to about seven significant digits above it. For amounts in `utwlt` that is fine for
trends, stalls, and thresholds, and not fine for equalities, which is why the one
equality that matters is exported as an exactly computed difference
(`twilight_rewards_escrow_solvency_delta_utwlt`). The same bound applies to heights
and the settlement clock, which pass 2^24 ≈ 16.8 million after roughly 2.7 years of
five-second blocks; from then on those gauges step in multiples of 2.

## Metrics reference

### Build identity

| Metric | Labels | Meaning |
|---|---|---|
| `twilightd_build_info` | `version`, `commit`, `go_version` | Always `1`. `version` and `commit` are the values stamped by the release script and `make build`; a binary built with a plain `go build` reports `unstamped`. Refreshed at API start and on every commit. |

Version skew across the validator set:

```promql
count(count by (version) (twilightd_build_info)) > 1
```

### `x/rewards`

| Metric | Unit | Meaning | Healthy |
|---|---|---|---|
| `twilight_rewards_current_epoch` | epoch | Open epoch number | advances by exactly 1 at each boundary |
| `twilight_rewards_current_epoch_start_height` | height | First block of the open epoch | |
| `twilight_rewards_current_epoch_end_height` | height | Canonical last block of the open epoch | `end = start + epoch_length − 1` |
| `twilight_rewards_epoch_blocks_remaining` | blocks | Blocks until the open epoch's last block, from the committed height; `0` on the closing block | decreases by 1 per block |
| `twilight_rewards_open_reward_enabled_blocks` | blocks | Reward-enabled blocks credited to the open epoch so far | increases by 1 per block while `paused = 0`; reads `1` on the first block of a new epoch (the counter resets as the epoch opens, then that block is credited) |
| `twilight_rewards_last_finalized_epoch` | epoch | Greatest finalized epoch; `0` before the first finalization | equals `current_epoch − 1` except on the closing block, where it equals `current_epoch` |
| `twilight_rewards_cumulative_emitted_utwlt` | utwlt | Total minted emission across all finalized epochs | monotonic non-decreasing; `≤ max_supply` |
| `twilight_rewards_max_supply_utwlt` | utwlt | The immutable supply cap | constant |
| `twilight_rewards_halving_tier` | tier | Supply-threshold halving tier reached | non-decreasing |
| `twilight_rewards_next_halving_threshold_utwlt` | utwlt | Cumulative emission at which the subsidy next halves; `0` once emission has reached the cap | |
| `twilight_rewards_escrow_balance_utwlt` | utwlt | Balance of the rewards module account | `= liability + carry` |
| `twilight_rewards_outstanding_entitlement_liability_utwlt` | utwlt | Unreleased entitlement value (the O(1) accumulator, not a scan) | rises at finalization, falls with settlement releases |
| `twilight_rewards_carry_forward_remainder_utwlt` | utwlt | Allocation remainder carried into the next epoch | small |
| `twilight_rewards_escrow_solvency_delta_utwlt` | utwlt | `escrow − (liability + carry)`, computed exactly | **exactly 0** |
| `twilight_rewards_paused` | 0/1 | Reward accrual and release are paused | 0 unless the emergency authority paused |
| `twilight_rewards_pause_transition_pending` | 0/1 | A pause or resume takes effect at the next block | transient |
| `twilight_rewards_release_enabled` | 0/1 | Monetary release is permitted (`1 − paused`) | 1 |

Deliberately **not** exported: "epochs until the next halving". It is not derivable
in constant time — it depends on future participation, pauses, and the treasury
share — and a number that looks exact but is a projection would be worse than none.
Distance to the next halving in supply terms is
`next_halving_threshold_utwlt − cumulative_emitted_utwlt`.

### `x/mining`

| Metric | Unit | Meaning | Healthy |
|---|---|---|---|
| `twilight_mining_settlement_clock` | ticks | The monotonic settlement clock; ticks once per block whose beginning-of-block pause state permits release | increases by 1 per block while `release_enabled = 1` |
| `twilight_mining_last_processed_reward_epoch` | epoch | Materialization cursor: the greatest reward epoch whose settlement set exists | `= twilight_rewards_last_finalized_epoch` |

Deliberately **not** exported: a count of open settlements. The module keeps no
counter, and the only derivation walks the open-settlement index, whose size is
unbounded exactly when settlements go unsealed — the outage such a metric would be
watching for. Settlement progress is observed from the authorization servers that
seal settlements, and from `mining_settlement_finalized` events; a maintained on-chain
counter is a consensus-state change and is tracked separately.

### `x/coreslot`

| Metric | Labels | Meaning | Healthy |
|---|---|---|---|
| `twilight_coreslot_active_slots` | | Number of ACTIVE CoreSlots (the validator set size) | `min_active_slots ≤ active ≤ max_active_slots`; changes only on a lifecycle transition |
| `twilight_coreslot_min_active_slots` | | Parameter | constant between parameter updates |
| `twilight_coreslot_max_active_slots` | | Parameter | constant between parameter updates |
| `twilight_coreslot_pending_key_rotations` | | Consensus-key rotations queued and not yet effective | 0 except during a rotation |
| `twilight_coreslot_pending_authority_nomination` | `role` = `primary` \| `emergency` | An authority handover is nominated and not yet accepted or canceled | 0 except during a handover |

**The nomination gauge only sees a nomination that waits.** It catches the honest
two-step handover, a nomination carried in genesis, and an attacker who nominates and
then pauses. It does **not** catch someone holding the authority key who nominates and
accepts in the same block: both transactions are admitted before either executes, the
nomination exists only inside that block, and the gauge — like the
`pending-authority-transfers` query — reads 0 before and after. Treat the gauge as an
early warning, never as the control. The control is an alert on **the authority
addresses themselves differing from the recorded, known-good values** (see "Authority
changed" below).

### Exporter health

| Metric | Labels | Meaning |
|---|---|---|
| `twilight_telemetry_read_failures_total` | `module` | Counter of commits at which the module's snapshot could not be read and its gauges were skipped. Any value is a node-local fault worth a look; the chain itself halts on the same corruption one block later. **The sink expires this counter** like every other series: one that is not incremented within `prometheus-retention-time` disappears and restarts at `1` on the next failure, so `increase()`/`rate()` miss one-off failures — alert on `max_over_time`, and note every increment is also logged at error level. |

## Alerts worth having

These are the conditions the metrics above exist for. Thresholds assume the
testnet's 360-block epochs; scale to your epoch length.

| Condition | Expression | Why |
|---|---|---|
| Accrual stalled | `increase(twilight_rewards_open_reward_enabled_blocks[10m]) == 0 and twilight_rewards_paused == 0 and increase(twilight_rewards_current_epoch[10m]) == 0` | Blocks are committing but no reward-enabled block is being credited |
| Epoch not finalizing | `changes(twilight_rewards_last_finalized_epoch[1h]) == 0` with `for: 1h` (window ≈ 2× the epoch's wall-clock length: 360 blocks × 5 s = 30 min; the `for` keeps a freshly appeared series, which has no changes yet, from firing) | A finalization boundary passed without a finalized epoch. **Pair with the liveness alert below:** during a full halt the gauge expires and this expression returns nothing |
| Materialization behind | `twilight_rewards_last_finalized_epoch - twilight_mining_last_processed_reward_epoch > 0` for more than one block | A finalized epoch has no settlement set |
| Escrow imbalance | `twilight_rewards_escrow_solvency_delta_utwlt != 0` | Money in escrow no longer matches what is owed |
| Unexpected pause | `twilight_rewards_paused == 1` | Correlate with operator intent |
| Validator set changed | `changes(twilight_coreslot_active_slots[1h]) > 0` | Every change should map to a known admission or removal |
| Authority nomination pending | `max by (role) (twilight_coreslot_pending_authority_nomination) == 1` | A handover is waiting to be accepted. Expected only during a planned rotation; otherwise an incident. Blind to a same-block nominate + accept (see above) |
| Version skew | `count(count by (version) (twilightd_build_info)) > 1` | A rollout is incomplete, or a node was not upgraded |
| Exporter fault | `max_over_time(twilight_telemetry_read_failures_total[1h]) > 0` | A snapshot read failed on that node (not `increase()`: the sink expires and restarts the counter, see above) |

**Authority changed:** no gauge carries the authority addresses yet, so this check runs
outside Prometheus: on a schedule, compare `twilightd coreslot-query params`
(`authority`, `emergency_authority`) against the addresses recorded when the network
launched or last rotated, and page on any difference. This is the check that catches a
stolen authority key, whatever order its holder uses. An exported
`authority_info` series that Prometheus can alert on is tracked as a follow-up.

**Liveness:** `absent(twilightd_build_info)` on a node whose metrics endpoint is up
means it has not committed within the retention window. Every `twilight_*` gauge
expires with it, so each stall alert above returns *no data* — not a firing
condition — during a full halt; this alert is what fires then, and it is not
optional alongside them.

## Signals via queries

The same state is available through the CLI for one-off inspection, and for values
the exporter deliberately leaves out.

### Epoch / emission

| Signal | Source | Healthy |
|---|---|---|
| `current_epoch` | `epoch-info` | advances by 1 each boundary |
| `current_epoch_start_height` / `current_epoch_end_height` | `epoch-info` | end = start + epoch_length − 1 |
| `cumulative_emitted` | `cumulative-emitted` | monotonic non-decreasing; ≤ `max_supply` |
| `minted_emission` (per epoch) | `epoch-reward <e>` | matches expected bounded emission for that epoch, including halving and cap behavior |
| current subsidy / tier | `next-halving` | subsidy halves at each threshold; may reach 0 near cap |

### Balance / coverage

| Signal | Source | Healthy |
|---|---|---|
| `rewards_balance` | `module-balances` | ≥ outstanding entitlements + carry |
| `fee_pool_balance` | `module-balances` | `0` (fees dormant) |
| `carry_out` (per epoch) | `epoch-reward <e>` | `reward_pool − allocated_amount`, ≥ 0 |

### Pause state

```bash
twilightd rewards-query pause-state --node <rpc> --output json
```

Rewards has one canonical pause state, not per-area flags. It reports
`pause_state` (including any pending transition) and `release_enabled`. If
paused, correlate with operator intent.

## Consensus / determinism signals (multi-node)

| Signal | Source | Healthy |
|---|---|---|
| app hash | block header / `agree.sh` | identical across all nodes at the same height |
| validators hash / next-validators hash | block header / `agree.sh` | identical across nodes |
| `num_val_updates` | `agree.sh` output | only CoreSlot ever produces these |

> **Operator check:** app-hash divergence across nodes is the single most
> important alarm — it indicates a state fork. Page on it immediately and see
> [Incident Response](incident-response.md).

## Logs

Watch node logs for repeated EndBlock errors (a fail-closed finalization fault
halts the block), for `epoch_finalized` events (see
[Events](../rewards/events.md)), and for `telemetry snapshot could not be read`,
which accompanies every increment of `twilight_telemetry_read_failures_total`.
