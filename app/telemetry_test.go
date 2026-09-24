package app_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/hashicorp/go-metrics"
	metricsprom "github.com/hashicorp/go-metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

// The telemetry contract.
//
// Metrics are the one thing this app does after a block that is not consensus.
// Three proofs keep them that way. A chain run twice, once with telemetry off
// and once on, with every app hash compared. A store-operation trace of the
// exporter across the states that change which reads it performs, requiring
// that no write or delete reaches the root multistore. And a gather of what is
// exported, name by name and value by value, against a chain whose state is
// known, so the operator documentation cannot drift from the binary.

// enableTelemetry turns the SDK telemetry flag on for the test and routes the
// global go-metrics sink to a Prometheus registry private to this test. The
// returned gather reads that registry.
//
// Deliberately not telemetry.New: it registers its sink with the process-global
// Prometheus registry, which can be done once per process, and every series any
// test ever exported would then be visible to every later test. A private
// registry per test makes "every exported series is documented" mean this
// test's series and nothing else. The SDK flag is process-global regardless,
// which is why the cleanup clears it.
func enableTelemetry(t *testing.T) func() map[string]float64 {
	t.Helper()
	registry := prometheus.NewRegistry()
	sink, err := metricsprom.NewPrometheusSinkFrom(metricsprom.PrometheusOpts{
		Registerer: registry,
		Expiration: time.Hour,
	})
	require.NoError(t, err)
	conf := metrics.DefaultConfig("")
	conf.EnableHostname = false
	conf.EnableRuntimeMetrics = false
	_, err = metrics.NewGlobal(conf, sink)
	require.NoError(t, err)
	telemetry.EnableTelemetry()
	t.Cleanup(disableTelemetry)
	return func() map[string]float64 { return gatherMetrics(t, registry) }
}

// disableTelemetry clears the SDK flag without touching any sink:
// telemetry.New with Enabled=false returns before it builds one.
func disableTelemetry() {
	_, _ = telemetry.New(telemetry.Config{Enabled: false})
}

// runChain boots the pinned-chain fixture and commits blocks through the given
// height, returning the app hash FinalizeBlock reported for each height.
func runChain(t *testing.T, through int64) (*pinnedChain, [][]byte) {
	t.Helper()
	chain := bootPinnedChain(t)
	hashes := make([][]byte, 0, through)
	for h := int64(1); h <= through; h++ {
		res, err := chain.app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: h})
		require.NoError(t, err)
		_, err = chain.app.Commit()
		require.NoError(t, err)
		require.Equal(t, res.AppHash, chain.app.LastCommitID().Hash, "height %d", h)
		hashes = append(hashes, res.AppHash)
	}
	chain.head = through
	return chain, hashes
}

func TestTelemetryDoesNotChangeTheAppHash(t *testing.T) {
	// Two full epochs and a few blocks of the third: two finalizations, two
	// settlement materializations, and the counters that run between them.
	const through = 2*epochLength + 5

	disableTelemetry()
	_, disabled := runChain(t, through)

	gather := enableTelemetry(t)
	require.True(t, telemetry.IsTelemetryEnabled())
	_, enabled := runChain(t, through)

	require.Len(t, enabled, int(through))
	require.Equal(t, disabled, enabled)

	// The enabled run must actually have exported, or the equality proves
	// nothing: the last commit of the third epoch is what the gauge shows.
	require.Equal(t, float64(3), gather()["twilight_rewards_current_epoch"])
}

// The exporter reads through a cache of the committed multistore. This traces
// every store operation at the root while it runs and requires that none is a
// write or a delete: the app-hash test above proves the states it happens to
// reach, this proves the mechanism, in the states that change which reads
// happen — a pending pause transition and an applied pause. Reads are required
// to be present so the trace is known to be watching the stores the exporter
// uses.
func TestTelemetryExporterWritesNothingToTheRootStore(t *testing.T) {
	enableTelemetry(t)
	chain := bootPinnedChain(t)
	chain.commitThrough(t, epochLength+2)

	ops := tracedExport(t, chain)
	require.Positive(t, ops["read"], "the trace saw no reads: it is not watching the exporter")
	require.Zero(t, ops["write"]+ops["delete"], "ops: %v", ops)

	require.NoError(t, chain.app.RewardsKeeper.SchedulePauseTransition(chain.headContext(), chain.head, true))
	ops = tracedExport(t, chain)
	require.Positive(t, ops["read"])
	require.Zero(t, ops["write"]+ops["delete"], "pending pause, ops: %v", ops)

	chain.commitThrough(t, chain.head+2)
	snap, err := chain.app.RewardsKeeper.TelemetrySnapshot(chain.headContext())
	require.NoError(t, err)
	require.True(t, snap.Paused, "the pause did not apply; the paused state was not exercised")
	ops = tracedExport(t, chain)
	require.Positive(t, ops["read"])
	require.Zero(t, ops["write"]+ops["delete"], "paused, ops: %v", ops)
}

// The tracer test above cannot tell "the exporter did not write" from "the
// exporter wrote into a copy that was thrown away": under a context over the
// live root store it would pass for exactly as long as no snapshot happens to
// write. This pins the discard mechanism itself. A deliberate write through the
// exporter's own context — the pause flag flipped and stored — must leave the
// root multistore's working hash unchanged, reach the root trace as no write,
// and leave the root reading the original value.
func TestTelemetryAWriteThroughTheExporterContextNeverReachesTheRoot(t *testing.T) {
	chain := bootPinnedChain(t)
	chain.commitThrough(t, epochLength+2)
	cms := chain.app.CommitMultiStore()
	before := cms.WorkingHash()

	var trace bytes.Buffer
	cms.SetTracer(&trace)
	ctx := chain.app.TelemetryContextForTest()
	pause, err := chain.app.RewardsKeeper.GetPauseState(ctx)
	require.NoError(t, err)
	pause.CurrentPaused = !pause.CurrentPaused
	require.NoError(t, chain.app.RewardsKeeper.PauseState.Set(ctx, pause))
	// The write is visible inside the context it was made through, so the
	// context is proven to be one a write can be made through at all.
	inside, err := chain.app.RewardsKeeper.GetPauseState(ctx)
	require.NoError(t, err)
	require.Equal(t, pause.CurrentPaused, inside.CurrentPaused)
	cms.SetTracer(nil)

	require.NotContains(t, trace.String(), `"operation":"write"`)
	require.Equal(t, before, cms.WorkingHash(), "a write through the exporter context reached the root store")
	root, err := chain.app.RewardsKeeper.GetPauseState(chain.headContext())
	require.NoError(t, err)
	require.NotEqual(t, pause.CurrentPaused, root.CurrentPaused, "the root store took the write")
}

// tracedExport runs the exporter with the SDK store tracer attached to the root
// multistore and returns a count of every operation the trace recorded. A cache
// created while the tracer is attached wraps each root store in the tracer, so
// reads that fall through the cache are recorded and a write that reached the
// root would be too.
func tracedExport(t *testing.T, chain *pinnedChain) map[string]int {
	t.Helper()
	var trace bytes.Buffer
	cms := chain.app.CommitMultiStore()
	cms.SetTracer(&trace)
	chain.app.EmitTelemetryForTest()
	cms.SetTracer(nil)

	ops := map[string]int{}
	scanner := bufio.NewScanner(&trace)
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		var op struct {
			Operation string `json:"operation"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &op))
		ops[op.Operation]++
	}
	require.NoError(t, scanner.Err())
	return ops
}

func TestTelemetryExportsEveryDocumentedGauge(t *testing.T) {
	gather := enableTelemetry(t)
	chain := bootPinnedChain(t)
	// Two blocks into epoch 2: epoch 1 has finalized and materialized, the open
	// counter has credited exactly two blocks, and nothing is paused.
	chain.commitThrough(t, epochLength+2)

	ctx := chain.headContext()
	rewards, err := chain.app.RewardsKeeper.TelemetrySnapshot(ctx)
	require.NoError(t, err)
	csParams, err := chain.app.CoreSlotKeeper.Params.Get(ctx)
	require.NoError(t, err)
	rParams, err := chain.app.RewardsKeeper.GetParams(ctx)
	require.NoError(t, err)
	maxSupply, ok := sdkmath.NewIntFromString(rParams.MaxSupply)
	require.True(t, ok)

	// The amounts being cross-checked must be real money, or a gauge that
	// exported zero for everything would pass.
	require.True(t, rewards.CumulativeEmitted.IsPositive(), "epoch 1 minted nothing")
	require.True(t, rewards.OutstandingLiability.IsPositive(), "epoch 1 created no entitlement")
	require.True(t, rewards.EscrowBalance.IsPositive())

	values := gather()
	expected := map[string]float64{
		"twilight_rewards_current_epoch":                                   2,
		"twilight_rewards_current_epoch_start_height":                      float64(epochLength + 1),
		"twilight_rewards_current_epoch_end_height":                        float64(2 * epochLength),
		"twilight_rewards_epoch_blocks_remaining":                          float64(epochLength - 2),
		"twilight_rewards_open_reward_enabled_blocks":                      2,
		"twilight_rewards_last_finalized_epoch":                            1,
		"twilight_rewards_cumulative_emitted_utwlt":                        gaugeOf(rewards.CumulativeEmitted),
		"twilight_rewards_max_supply_utwlt":                                gaugeOf(maxSupply),
		"twilight_rewards_halving_tier":                                    0,
		"twilight_rewards_next_halving_threshold_utwlt":                    gaugeOf(maxSupply.QuoRaw(2)),
		"twilight_rewards_escrow_balance_utwlt":                            gaugeOf(rewards.EscrowBalance),
		"twilight_rewards_outstanding_entitlement_liability_utwlt":         gaugeOf(rewards.OutstandingLiability),
		"twilight_rewards_carry_forward_remainder_utwlt":                   gaugeOf(rewards.CarryForwardRemainder),
		"twilight_rewards_escrow_solvency_delta_utwlt":                     0,
		"twilight_rewards_paused":                                          0,
		"twilight_rewards_pause_transition_pending":                        0,
		"twilight_rewards_release_enabled":                                 1,
		"twilight_mining_settlement_clock":                                 float64(epochLength + 2),
		"twilight_mining_last_processed_reward_epoch":                      1,
		"twilight_coreslot_active_slots":                                   1,
		"twilight_coreslot_min_active_slots":                               float64(csParams.MinActiveSlots),
		"twilight_coreslot_max_active_slots":                               float64(csParams.MaxActiveSlots),
		"twilight_coreslot_pending_key_rotations":                          0,
		`twilight_coreslot_pending_authority_nomination{role="emergency"}`: 0,
		`twilight_coreslot_pending_authority_nomination{role="primary"}`:   0,
		`twilightd_build_info{commit="unstamped",go_version="` + goruntime.Version() + `",version="unstamped"}`: 1,
	}
	for name, want := range expected {
		got, found := values[name]
		require.True(t, found, "metric %s is not exported", name)
		require.Equal(t, want, got, "metric %s", name)
	}
	// Every series this app exports is a documented one: an undocumented gauge
	// is a name an operator cannot look up, and a read-failure counter here
	// means a snapshot failed on a healthy chain. The registry is this test's
	// own, so nothing another test exported can appear here; what can appear
	// is the SDK's own module-manager timing series, which share the sink and
	// are not this app's to document.
	for name := range values {
		if !strings.HasPrefix(name, "twilight") {
			continue
		}
		_, documented := expected[name]
		require.True(t, documented, "metric %s is exported but not documented", name)
	}
}

// gatherMetrics indexes every sample in the registry by its full series name,
// labels included, exactly as a scrape would render it.
func gatherMetrics(t *testing.T, registry *prometheus.Registry) map[string]float64 {
	t.Helper()
	families, err := registry.Gather()
	require.NoError(t, err)
	values := map[string]float64{}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			labels := make([]string, 0, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				labels = append(labels, fmt.Sprintf("%s=%q", label.GetName(), label.GetValue()))
			}
			name := family.GetName()
			if len(labels) > 0 {
				name += "{" + strings.Join(labels, ",") + "}"
			}
			// Summaries are the SDK's own begin/end-blocker timings, which the
			// module manager emits into the same global sink once telemetry is
			// on; this app exports only gauges and counters.
			switch {
			case metric.GetGauge() != nil:
				values[name] = metric.GetGauge().GetValue()
			case metric.GetCounter() != nil:
				values[name] = metric.GetCounter().GetValue()
			}
		}
	}
	return values
}

// gaugeOf applies the exact conversion the exporter applies: to float32, as the
// SDK telemetry API takes, then widened by the Prometheus sink.
func gaugeOf(amount sdkmath.Int) float64 {
	value, _ := new(big.Float).SetInt(amount.BigInt()).Float32()
	return float64(value)
}
