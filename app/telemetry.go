package app

import (
	"math/big"
	goruntime "runtime"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/hashicorp/go-metrics"

	sdkmath "cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/version"

	coreslotkeeper "github.com/twilight-project/twilight-core/x/coreslot/keeper"
	coreslottypes "github.com/twilight-project/twilight-core/x/coreslot/types"
	miningkeeper "github.com/twilight-project/twilight-core/x/mining/keeper"
	miningtypes "github.com/twilight-project/twilight-core/x/mining/types"
	rewardskeeper "github.com/twilight-project/twilight-core/x/rewards/keeper"
	rewardstypes "github.com/twilight-project/twilight-core/x/rewards/types"
)

// Node-local metrics for the chain's economic and validator-set state.
//
// # Where this runs, and why there
//
// Every gauge below is set from Commit, after BaseApp.Commit has returned. At
// that point the block's state is persisted, its app hash was already produced by
// FinalizeBlock, and no cache context of any module is open. Nothing that runs
// here can reach an event or a ValidatorUpdate, because the block that could
// carry them is over.
//
// The store is a different matter, and "after Commit" is not what protects it.
// The root multistore is live: a write made through a context over it after
// Commit(H) lands in FinalizeBlock(H+1)'s app hash, on the nodes that have
// telemetry enabled and on no others. So the snapshots are read through a CACHE
// of the committed multistore that is never written back. A write attempted
// through it — by a future edit to a snapshot, or by an accessor that grows a
// side effect — is buffered in the cache and discarded with it. The tracer test
// in telemetry_test.go pins that no write or delete reaches the root store while
// the exporter runs, across the pause states that change which reads happen.
//
// The values are read through each module's own TelemetrySnapshot, which is a
// pure read. The keepers set no gauges and never call this; the dependency runs
// one way, from the app to the modules.
//
// # What this can never do
//
// It returns nothing. A read that fails skips that module's gauges and counts the
// failure; a panic is recovered and logged, and the counter the recovery bumps is
// itself guarded, so a sink that panics cannot escape through its own failure
// report. When telemetry is disabled in app.toml the function returns before the
// first read, so the disabled path is exactly the pre-telemetry app. The
// disabled-versus-enabled app-hash equality test pins that.
//
// # Precision
//
// The SDK telemetry API carries float32. Amounts in utwlt are exact below 2^24
// and rounded to about seven significant digits above it, which is fine for
// trend and stall alerts and useless for equalities — so the one equality that
// matters, escrow == liability + carry, is exported as an exactly computed
// difference rather than as three terms to subtract. The same bound applies to
// heights and the settlement clock, which reach 2^24 after about 2.7 years of
// five-second blocks.

const (
	// telemetryNamespace prefixes every module gauge: twilight_<module>_<name>.
	telemetryNamespace = "twilight"
	// buildInfoMetric is the binary identity gauge, named after the binary
	// rather than the chain so it reads like the build_info convention
	// (prometheus_build_info, go_build_info) operators already alert on.
	buildInfoMetric = "twilightd"
	// unstampedBuild is the label value for a binary built without the release
	// ldflags. It is a word rather than an empty string because Prometheus
	// treats an empty label value as an absent label, and a node whose version
	// label is missing is indistinguishable from one whose build_info is absent.
	unstampedBuild = "unstamped"
)

// Commit persists the block through BaseApp and then, with the store committed,
// exports the module gauges. The export runs after the commit response is
// complete and cannot alter it: an error from BaseApp.Commit is returned before
// any metric is touched, and the export itself has no error to return.
func (a *App) Commit() (*abci.ResponseCommit, error) {
	res, err := a.App.Commit()
	if err != nil {
		return res, err
	}
	a.emitTelemetry()
	return res, nil
}

// emitTelemetry exports the build identity and every module snapshot for the
// height the store is committed at. It never returns anything.
func (a *App) emitTelemetry() {
	if !telemetry.IsTelemetryEnabled() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			a.Logger().Error("telemetry export panicked; the gauges for this height were skipped", "panic", r)
			a.countTelemetryReadFailure("app")
		}
	}()

	emitBuildInfo()

	ctx := a.telemetryContext()

	if snap, err := a.CoreSlotKeeper.TelemetrySnapshot(ctx); err != nil {
		a.reportTelemetryReadFailure(coreslottypes.ModuleName, err)
	} else {
		emitCoreSlotTelemetry(snap)
	}
	if snap, err := a.RewardsKeeper.TelemetrySnapshot(ctx); err != nil {
		a.reportTelemetryReadFailure(rewardstypes.ModuleName, err)
	} else {
		emitRewardsTelemetry(snap)
	}
	if snap, err := a.MiningKeeper.TelemetrySnapshot(ctx); err != nil {
		a.reportTelemetryReadFailure(miningtypes.ModuleName, err)
	} else {
		emitMiningTelemetry(snap)
	}
}

// telemetryContext is the context every snapshot is read through: a cache of the
// committed multistore, at the committed height. Reads fall through to the
// committed stores; a write stops in the cache, which nothing ever writes back.
// It is not a block context: no module cache, no event manager anything will
// read.
//
// This is a seam on purpose. The tracer test proves the exporter issues no
// write, but on its own it cannot tell "did not write" from "wrote into a copy
// that was thrown away" — so a second test makes a deliberate write through
// this context and requires the root store to be unchanged. Building the
// context anywhere else would put it out of that test's reach.
func (a *App) telemetryContext() sdk.Context {
	return sdk.NewContext(
		a.CommitMultiStore().CacheMultiStore(),
		cmtproto.Header{Height: a.LastBlockHeight()},
		false,
		a.Logger(),
	)
}

func (a *App) reportTelemetryReadFailure(module string, err error) {
	a.Logger().Error("telemetry snapshot could not be read; the module's gauges were skipped",
		"module", module, "err", err)
	a.countTelemetryReadFailure(module)
}

// countTelemetryReadFailure increments twilight_telemetry_read_failures_total
// for the module.
//
// It is the failure report of the panic recovery above, so it must not be able
// to panic out of it: if the sink itself is what failed, the second panic would
// escape emitTelemetry through the recovery's own counter call. It is guarded
// separately for that reason.
//
// The Prometheus sink expires a counter that is not incremented within the
// retention window and recreates it at 1 on the next failure, so a rate over it
// misses one-off failures. The documented alert is max_over_time, not increase;
// every increment is also logged at error level, which does not expire.
func (a *App) countTelemetryReadFailure(module string) {
	defer func() {
		if r := recover(); r != nil {
			a.Logger().Error("telemetry read-failure counter panicked", "module", module, "panic", r)
		}
	}()
	telemetry.IncrCounterWithLabels(
		[]string{telemetryNamespace, "telemetry", "read_failures_total"},
		1,
		[]metrics.Label{telemetry.NewLabel("module", module)},
	)
}

// emitBuildInfo sets twilightd_build_info{version,commit,go_version} = 1.
//
// version.Version and version.Commit are stamped by the release script and by
// `make build` through ldflags; a plain `go build` leaves them empty and the
// gauge says so. It is a gauge rather than a one-time registration because the
// Prometheus sink expires any series that is not refreshed within its retention
// window, so it is refreshed on every commit.
func emitBuildInfo() {
	telemetry.SetGaugeWithLabels(
		[]string{buildInfoMetric, "build_info"},
		1,
		[]metrics.Label{
			telemetry.NewLabel("version", stampedOr(version.Version)),
			telemetry.NewLabel("commit", stampedOr(version.Commit)),
			telemetry.NewLabel("go_version", goruntime.Version()),
		},
	)
}

func stampedOr(value string) string {
	if value == "" {
		return unstampedBuild
	}
	return value
}

func emitRewardsTelemetry(snap rewardskeeper.TelemetrySnapshot) {
	const module = rewardstypes.ModuleName
	setGauge(module, "current_epoch", float32(snap.CurrentEpoch))
	setGauge(module, "current_epoch_start_height", float32(snap.CurrentEpochStartHeight))
	setGauge(module, "current_epoch_end_height", float32(snap.CurrentEpochEndHeight))
	setGauge(module, "epoch_blocks_remaining", float32(snap.BlocksRemainingInEpoch))
	setGauge(module, "open_reward_enabled_blocks", float32(snap.OpenRewardEnabledBlocks))
	setGauge(module, "last_finalized_epoch", float32(snap.LastFinalizedEpoch))
	setGauge(module, "cumulative_emitted_utwlt", amountGauge(snap.CumulativeEmitted))
	setGauge(module, "max_supply_utwlt", amountGauge(snap.MaxSupply))
	setGauge(module, "halving_tier", float32(snap.HalvingTier))
	setGauge(module, "next_halving_threshold_utwlt", amountGauge(snap.NextHalvingThreshold))
	setGauge(module, "escrow_balance_utwlt", amountGauge(snap.EscrowBalance))
	setGauge(module, "outstanding_entitlement_liability_utwlt", amountGauge(snap.OutstandingLiability))
	setGauge(module, "carry_forward_remainder_utwlt", amountGauge(snap.CarryForwardRemainder))
	setGauge(module, "escrow_solvency_delta_utwlt", amountGauge(snap.EscrowSolvencyDelta))
	setGauge(module, "paused", boolGauge(snap.Paused))
	setGauge(module, "pause_transition_pending", boolGauge(snap.PauseTransitionPending))
	setGauge(module, "release_enabled", boolGauge(snap.ReleaseEnabled))
}

func emitMiningTelemetry(snap miningkeeper.TelemetrySnapshot) {
	const module = miningtypes.ModuleName
	setGauge(module, "settlement_clock", float32(snap.SettlementClock))
	setGauge(module, "last_processed_reward_epoch", float32(snap.LastProcessedRewardEpoch))
}

func emitCoreSlotTelemetry(snap coreslotkeeper.TelemetrySnapshot) {
	const module = coreslottypes.ModuleName
	setGauge(module, "active_slots", float32(snap.ActiveSlots))
	setGauge(module, "min_active_slots", float32(snap.MinActiveSlots))
	setGauge(module, "max_active_slots", float32(snap.MaxActiveSlots))
	setGauge(module, "pending_key_rotations", float32(snap.PendingKeyRotations))
	setRoleGauge(module, "pending_authority_nomination", "primary", snap.PrimaryNominationPending)
	setRoleGauge(module, "pending_authority_nomination", "emergency", snap.EmergencyNominationPending)
}

func setGauge(module, name string, value float32) {
	telemetry.SetGauge(value, telemetryNamespace, module, name)
}

func setRoleGauge(module, name, role string, pending bool) {
	telemetry.SetGaugeWithLabels(
		[]string{telemetryNamespace, module, name},
		boolGauge(pending),
		[]metrics.Label{telemetry.NewLabel("role", role)},
	)
}

func boolGauge(value bool) float32 {
	if value {
		return 1
	}
	return 0
}

// amountGauge converts a utwlt amount to the float32 the telemetry API takes.
// Rounding is to nearest, which is monotonic, so a non-decreasing amount stays
// non-decreasing in the gauge.
func amountGauge(amount sdkmath.Int) float32 {
	value, _ := new(big.Float).SetInt(amount.BigInt()).Float32()
	return value
}
