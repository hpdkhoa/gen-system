// generator/internal/engine/logic_stats.go, v26.24 (WP1): per-run counters for
// what happened to model-proposed operations, so the benchmark can report a
// quality axis that is able to move. compile_pass alone cannot: the stub path
// guarantees the service builds, so "did it build" is 100% by construction.
// What varies with model quality is how many operations survived the compile
// gate as real logic versus degrading to a `not implemented` stub, and how many
// repair round-trips that took. These counters record exactly that, from the
// same code path production uses. Nothing is instrumented differently for the
// bench.
//
// Package-level (mutex-guarded) rather than threaded through call signatures,
// matching the existing run-state seam (SetLogicRun). genbench resets before a
// run and snapshots after.
package engine

import "sync"

// LogicStats counts operation outcomes for the current run.
type LogicStats struct {
	OpsTotal        int // operations that entered the compile gate and were emitted
	OpsStubbed      int // of those, how many degraded to a `not implemented` stub
	RepairAttempts  int // op-level model repair round-trips (maxRepairAttempts cap)
	RepairSuccesses int // repair attempts whose result then compiled

	// v26.33: the style gate, separate from the compile gate. An operation
	// that compiles but breaks a team rule is a violation; a style repair
	// round-trip is counted like a compile repair; a success is a repaired
	// body that both compiles and is clean. Style never stubs.
	StyleViolations     int
	StyleRepairAttempts int
	StyleRepairSuccess  int
}

var (
	logicStatsMu sync.Mutex
	logicStats   LogicStats
)

// ResetLogicStats zeroes the counters. Call before a measured run.
func ResetLogicStats() {
	logicStatsMu.Lock()
	logicStats = LogicStats{}
	logicStatsMu.Unlock()
}

// LogicStatsSnapshot returns a copy of the counters.
func LogicStatsSnapshot() LogicStats {
	logicStatsMu.Lock()
	defer logicStatsMu.Unlock()
	return logicStats
}

func recordOpOutcome(stubbed bool) {
	logicStatsMu.Lock()
	logicStats.OpsTotal++
	if stubbed {
		logicStats.OpsStubbed++
	}
	logicStatsMu.Unlock()
}

func recordRepairAttempt(success bool) {
	logicStatsMu.Lock()
	logicStats.RepairAttempts++
	if success {
		logicStats.RepairSuccesses++
	}
	logicStatsMu.Unlock()
}

func recordStyleViolation() {
	logicStatsMu.Lock()
	logicStats.StyleViolations++
	logicStatsMu.Unlock()
}

func recordStyleRepair(success bool) {
	logicStatsMu.Lock()
	logicStats.StyleRepairAttempts++
	if success {
		logicStats.StyleRepairSuccess++
	}
	logicStatsMu.Unlock()
}
