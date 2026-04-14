//go:build darwin

package macos

import (
	"context"
	"runtime"
	"testing"

	"github.com/gravitrone/providence-core/internal/config"
)

// bridgeInstalled returns true if the Swift bridge binary is findable.
func bridgeInstalled() bool {
	return lookupSwiftBinary("") != ""
}

// BenchmarkScreenshot benchmarks a full screenshot round-trip via the bridge.
func BenchmarkScreenshot(b *testing.B) {
	if runtime.GOOS != "darwin" {
		b.Skip("macOS only")
	}
	if !bridgeInstalled() {
		b.Skip("native bridge not installed")
	}

	bridge := New(WithConfig(config.BridgeConfig{Mode: "auto"}))
	defer bridge.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bridge.Screenshot(ctx)
	}
}

// BenchmarkAXTree benchmarks the AX tree walk for Finder (if running).
func BenchmarkAXTree(b *testing.B) {
	if runtime.GOOS != "darwin" {
		b.Skip("macOS only")
	}
	if !bridgeInstalled() {
		b.Skip("native bridge not installed")
	}

	bridge := New(WithConfig(config.BridgeConfig{Mode: "auto"}))
	defer bridge.Close()

	ctx := context.Background()
	p := AXTreeParams{App: "Finder", MaxDepth: 4, MaxNodes: 200}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bridge.AXTree(ctx, p)
	}
}

// BenchmarkScreenDiff benchmarks a screen diff operation.
func BenchmarkScreenDiff(b *testing.B) {
	if runtime.GOOS != "darwin" {
		b.Skip("macOS only")
	}
	if !bridgeInstalled() {
		b.Skip("native bridge not installed")
	}

	bridge := New(WithConfig(config.BridgeConfig{Mode: "auto"}))
	defer bridge.Close()

	ctx := context.Background()
	p := ScreenDiffParams{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bridge.ScreenDiff(ctx, p)
	}
}

// BenchmarkMetricsRecord benchmarks the in-memory metrics ring buffer.
func BenchmarkMetricsRecord(b *testing.B) {
	m := NewMetrics()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Record("screenshot", 10_000_000, true) // 10ms in nanoseconds
	}
}

// BenchmarkMetricsSnapshot benchmarks snapshotting all ops.
func BenchmarkMetricsSnapshot(b *testing.B) {
	m := NewMetrics()
	ops := []string{"screenshot", "click", "ax_tree", "screen_diff", "action_batch"}
	for _, op := range ops {
		for j := 0; j < 100; j++ {
			m.Record(op, 5_000_000, j%10 != 0)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Snapshot()
	}
}
