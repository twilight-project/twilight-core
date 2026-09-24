package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ProtocolPayoutModulesForTest exposes the exemption list so the completeness
// test can compare it against the source-derived inventory. Exported only to
// the test build, because the list itself is an implementation detail.
func ProtocolPayoutModulesForTest() []string { return protocolPayoutModules }

// EmitTelemetryForTest runs the metrics export exactly as Commit does, so a test
// can trace its store operations in isolation from the commit that normally
// precedes it. Exported only to the test build.
func (a *App) EmitTelemetryForTest() { a.emitTelemetry() }

// TelemetryContextForTest returns the context the exporter reads through, so a
// test can write through it deliberately and prove the write is discarded.
// Exported only to the test build.
func (a *App) TelemetryContextForTest() sdk.Context { return a.telemetryContext() }
