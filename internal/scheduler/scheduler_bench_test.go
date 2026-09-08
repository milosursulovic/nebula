package scheduler

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/milosursulovic/nebula/internal/node"
)

// benchNodes builds n nodes with varied, randomized-but-deterministic
// capacity (spec section 47: "compare different scheduler algorithms") —
// large enough that each strategy has to actually scan/compare, not just
// return the first node it sees.
func benchNodes(n int) fakeNodeLister {
	rng := rand.New(rand.NewSource(1))
	nodes := make([]node.Node, n)
	for i := 0; i < n; i++ {
		totalCPU := 8 + rng.Intn(56)       // 8-64
		totalMem := 8192 + rng.Intn(57344) // 8-64 GB
		totalDisk := 100 + rng.Intn(900)   // 100-1000 GB
		availCPU := rng.Intn(totalCPU + 1)
		availMem := rng.Intn(totalMem + 1)
		availDisk := rng.Intn(totalDisk + 1)
		nodes[i] = onlineNode(
			fmt.Sprintf("node-%d", i),
			totalCPU, availCPU, totalMem, availMem, totalDisk, availDisk,
			rng.Float64()*8, rng.Intn(50),
		)
	}
	return fakeNodeLister{nodes: nodes}
}

var benchRequest = ResourceRequest{CPU: 2, MemoryMB: 4096, DiskGB: 50}

func benchmarkStrategy(b *testing.B, sched Scheduler) {
	b.Helper()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sched.Schedule(ctx, benchRequest); err != nil {
			b.Fatalf("Schedule: %v", err)
		}
	}
}

func BenchmarkFirstFit(b *testing.B) {
	benchmarkStrategy(b, NewFirstFit(benchNodes(1000)))
}

func BenchmarkBestFit(b *testing.B) {
	benchmarkStrategy(b, NewBestFit(benchNodes(1000)))
}

func BenchmarkLeastLoaded(b *testing.B) {
	benchmarkStrategy(b, NewLeastLoaded(benchNodes(1000)))
}

func BenchmarkWeighted(b *testing.B) {
	benchmarkStrategy(b, NewWeighted(benchNodes(1000)))
}
