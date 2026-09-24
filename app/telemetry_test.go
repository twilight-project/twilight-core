package app_test

import (
	"bufio"
	"bytes"
	"math/big"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

// The telemetry contract.
//
// Metrics are the one thing this app does after a block that is not consensus,
// and the proof that they stay that way is not an argument about where the code
// sits but a chain run twice: once with telemetry off, once with it on, with
// every app hash compared. The second proof is that what is exported is what the
// operator documentation says, name by name, against a chain whose state is
// known.

// The Prometheus sink registers with the process-global registry and can be
// created only once per process, so it is created lazily and shared. Enabling
// and disabling afterwards touches only the SDK's telemetry flag, which is what
// gates every emission: telemetry.New with Enabled=false returns before it
// builds a sink, and EnableTelemetry sets the flag alone.
var (
	prometheusOnce    sync.Once
	prometheusMetrics *telemetry.Metrics
	prometheusErr     error
)

func enablePrometheusTelemetry(t *testing.T) *telemetry.Metrics {
	t.Helper()
	prometheusOnce.Do(func() {
		prometheusMetrics, prometheusErr = telemetry.New(telemetry.Config{
			Enabled:                 true,
			EnableHostname:          false,
			EnableHostnameLabel:     false,
			PrometheusRetentionTime: 3600,
		})
	})
	require.NoError(t, prometheusErr)
	require.NotNil(t, prometheusMetrics)
	telemetry.EnableTelemetry()
	t.Cleanup(disableTelemetry)
	return prometheusMetrics
}

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

	metrics := enablePrometheusTelemetry(t)
	require.True(t, telemetry.IsTelemetryEnabled())
	_, enabled := runChain(t, through)

	require.Len(t, enabled, int(through))
	require.Equal(t, disabled, enabled)

	// The enabled run must actually have exported, or the equality proves
	// nothing: the last commit of the third epoch is what the gauge shows.
	values := gatherMetrics(t, metrics)
	require.Equal(t, float64(3), values["twilight_rewards_current_epoch"])
}

func TestTelemetryExportsEveryDocumentedGauge(t *testing.T) {
	metrics := enablePrometheusTelemetry(t)
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

	values := gatherMetrics(t, metrics)
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
	// Every exported twilight_ series is a documented one: an undocumented gauge
	// is a name an operator cannot look up.
	for name := range values {
		if strings.HasPrefix(name, "twilight") {
			_, documented := expected[name]
			require.True(t, documented, "metric %s is exported but not documented", name)
		}
	}
	_, failures := values[`twilight_telemetry_read_failures_total{module="rewards"}`]
	require.False(t, failures, "a snapshot read failed")
}

// gatherMetrics renders the Prometheus exposition and indexes every sample by
// its full series name, labels included, exactly as a scrape would see it.
func gatherMetrics(t *testing.T, metrics *telemetry.Metrics) map[string]float64 {
	t.Helper()
	res, err := metrics.Gather(telemetry.FormatPrometheus)
	require.NoError(t, err)
	values := map[string]float64{}
	scanner := bufio.NewScanner(bytes.NewReader(res.Metrics))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		split := strings.LastIndex(line, " ")
		require.Positive(t, split, line)
		value, err := strconv.ParseFloat(line[split+1:], 64)
		require.NoError(t, err, line)
		values[line[:split]] = value
	}
	require.NoError(t, scanner.Err())
	return values
}

// gaugeOf applies the exact conversion the exporter applies: to float32, as the
// SDK telemetry API takes, then widened by the Prometheus sink.
func gaugeOf(amount sdkmath.Int) float64 {
	value, _ := new(big.Float).SetInt(amount.BigInt()).Float32()
	return float64(value)
}
