---
title: Monitoring
---

# Monitoring

What to watch, and how to read it. The node exports the chain's economic and
validator-set state as Prometheus gauges through the Cosmos SDK telemetry facility;
the same signals remain available through queries and the node RPC for one-off
inspection.

## Metrics endpoint

Metrics are served at `/metrics` on the **API server**, in Prometheus exposition
format when requested with `?format=prometheus`. Enable both in `app.toml`:

```toml
[api]
enable = true
address = "tcp://0.0.0.0:1317"

[telemetry]
enabled = true
# Seconds a series is retained without being refreshed. Must be > 0 for the
# Prometheus endpoint to exist. Every gauge below is refreshed once per commit,
# so keep this comfortably above the block interval.
prometheus-retention-time = 60
# Keep both false: a hostname prefix changes every metric NAME below to
# <host>_twilight_..., which breaks every rule written against them. Use
# enable-hostname-label = true if you want the host as a label instead.
enable-hostname = false
enable-hostname-label = false
# Leave empty for the same reason: a service name is prepended to every metric name.
service-name = ""
# Attached to every series. The chain id is the one label a fleet dashboard needs.
global-labels = [["chain_id", "twilight-testnet-1"]]
```

A Prometheus scrape job then looks like:

```yaml
- job_name: twilightd
  metrics_path: /metrics
  params:
    format: [prometheus]
  static_configs:
    - targets: ["node-1:1317", "node-2:1317"]
```

The API server also serves the REST endpoints; if it is exposed beyond the
monitoring network, put it behind the same access controls as the RPC. CometBFT's
own consensus metrics (block height, rounds, peers, mempool) are a separate endpoint
configured in `config.toml` under `[instrumentation]` and are not repeated here.

### Where the gauges come from

Every `twilight_*` gauge is set **after `Commit`**, from committed state, by a
read-only path that returns nothing into the block. It cannot write state, emit an
event, or produce a validator update, and a node with telemetry disabled runs
byte-for-byte the same state machine — a test commits a multi-epoch chain both ways
and compares every app hash. A gauge that cannot be read is skipped for that block
and counted in `twilight_telemetry_read_failures_total`.

Values are carried as `float32` by the SDK telemetry API. Amounts in `utwlt` are exact
below 2^24 and rounded to about seven significant digits above it. That is fine for
trends, stalls, and thresholds; it is not fine for equalities, which is why the one
equality that matters is exported as an exactly computed difference
(`twilight_rewards_escrow_solvency_delta_utwlt`).

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
| `twilight_rewards_open_reward_enabled_blocks` | blocks | Reward-enabled blocks credited to the open epoch so far | increases by 1 per block while `paused = 0`; resets to 0 when an epoch opens |
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

### Exporter health

| Metric | Labels | Meaning |
|---|---|---|
| `twilight_telemetry_read_failures_total` | `module` | Counter of commits at which the module's snapshot could not be read and its gauges were skipped. Any increase is a node-local fault worth a look; the chain itself halts on the same corruption one block later. |

## Alerts worth having

These are the conditions the metrics above exist for. Thresholds assume the
testnet's 360-block epochs; scale to your epoch length.

| Condition | Expression | Why |
|---|---|---|
| Accrual stalled | `increase(twilight_rewards_open_reward_enabled_blocks[10m]) == 0 and twilight_rewards_paused == 0 and increase(twilight_rewards_current_epoch[10m]) == 0` | Blocks are committing but no reward-enabled block is being credited |
| Epoch not finalizing | `time() - timestamp(changes(twilight_rewards_last_finalized_epoch[1h]) > 0)` exceeds one epoch's wall-clock length | The finalization boundary passed without a finalized epoch |
| Materialization behind | `twilight_rewards_last_finalized_epoch - twilight_mining_last_processed_reward_epoch > 0` for more than one block | A finalized epoch has no settlement set |
| Escrow imbalance | `twilight_rewards_escrow_solvency_delta_utwlt != 0` | Money in escrow no longer matches what is owed |
| Unexpected pause | `twilight_rewards_paused == 1` | Correlate with operator intent |
| Validator set changed | `changes(twilight_coreslot_active_slots[1h]) > 0` | Every change should map to a known admission or removal |
| Version skew | `count(count by (version) (twilightd_build_info)) > 1` | A rollout is incomplete, or a node was not upgraded |
| Exporter fault | `increase(twilight_telemetry_read_failures_total[1h]) > 0` | A snapshot read failed on that node |

`absent(twilightd_build_info)` on a node whose API server is up means it has not
committed within the retention window, which is itself a liveness signal.

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
