package app

// ProtocolPayoutModulesForTest exposes the exemption list so the completeness
// test can compare it against the source-derived inventory. Exported only to
// the test build, because the list itself is an implementation detail.
func ProtocolPayoutModulesForTest() []string { return protocolPayoutModules }

// EmitTelemetryForTest runs the metrics export exactly as Commit does, so a test
// can trace its store operations in isolation from the commit that normally
// precedes it. Exported only to the test build.
func (a *App) EmitTelemetryForTest() { a.emitTelemetry() }
