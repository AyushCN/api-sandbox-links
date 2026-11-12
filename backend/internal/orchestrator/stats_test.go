package orchestrator

import (
	"sync"
	"testing"

	"github.com/docker/docker/api/types/container"
)

// A WebSocket handler unsubscribes when it returns, and a collector whose Docker
// stream ended closes every subscriber from its own defer. Both paths close the
// same channels, so the subscriber bookkeeping has to tolerate:
//
//   - a broadcast that is in flight while Unsubscribe closes the channel
//     (send on closed channel — a panic that takes the process down)
//   - an Unsubscribe that arrives *after* close already closed that channel
//     (close of closed channel — also a panic)
//
// Both were live before this was tracked; the fix is the write lock in
// broadcast plus the `closed` flag in close/Unsubscribe.
func TestBroadcastRacesWithUnsubscribe(t *testing.T) {
	for i := 0; i < 500; i++ {
		c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
		ch := c.Subscribe()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); c.broadcast(ResourceSample{SandboxID: "sb"}) }()
		go func() { defer wg.Done(); c.Unsubscribe(ch) }()
		wg.Wait()
	}
}

func TestCloseRacesWithUnsubscribe(t *testing.T) {
	for i := 0; i < 500; i++ {
		c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
		ch := c.Subscribe()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); c.close() }()
		go func() { defer wg.Done(); c.Unsubscribe(ch) }()
		wg.Wait()
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
	ch := c.Subscribe()
	c.close()
	c.close() // must not panic
	c.Unsubscribe(ch)
}

func TestBroadcastAfterCloseIsSafe(t *testing.T) {
	c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
	c.Subscribe()
	c.close()
	c.broadcast(ResourceSample{SandboxID: "sb"})
}

// A slow subscriber must be dropped rather than back-pressuring the Docker
// stats stream, which is shared by every other panel watching the same sandbox.
func TestBroadcastDropsForSlowSubscriber(t *testing.T) {
	c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
	ch := c.Subscribe() // buffer 16, never drained

	for i := 0; i < 100; i++ {
		c.broadcast(ResourceSample{SandboxID: "sb"})
	}
	if got := len(ch); got != 16 {
		t.Errorf("buffered %d samples, want it capped at 16", got)
	}
}

// Last must hand back a copy: the collector keeps replacing the sample, and a
// caller that held the shared pointer could observe a torn read.
func TestLastReturnsACopy(t *testing.T) {
	c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
	first := &ResourceSample{SandboxID: "sb", CPUPercent: 10}
	c.setLast(first)

	got := c.Last()
	if got == first {
		t.Error("Last() returned the collector's own pointer, not a copy")
	}
	*first = ResourceSample{SandboxID: "sb", CPUPercent: 99}
	if got.CPUPercent != 10 {
		t.Errorf("caller's copy changed to %v after the collector's value was reused", got.CPUPercent)
	}
	if c.Last() == nil {
		t.Error("Last() = nil after setLast")
	}
}

func TestLastIsNilBeforeFirstSample(t *testing.T) {
	c := &StatsCollector{subscribers: map[chan ResourceSample]struct{}{}}
	if got := c.Last(); got != nil {
		t.Errorf("Last() = %+v on a fresh collector, want nil", got)
	}
}

// CPU% is a rate, so the first sample of a stream — which has no predecessor —
// must report 0 rather than a lifetime average. The sample data below models a
// 2-core host: system_cpu_usage is host-wide, so a fully busy core contributes
// half of the system delta.
func TestComputeCPUPercent(t *testing.T) {
	busyCore := container.CPUUsage{TotalUsage: 2e9, PercpuUsage: []uint64{2e9, 1e9}}
	busyBoth := container.CPUUsage{TotalUsage: 3e9, PercpuUsage: []uint64{2e9, 2e9}}
	prev := container.CPUUsage{TotalUsage: 1e9, PercpuUsage: []uint64{1e9, 1e9}}

	cases := []struct {
		name      string
		prev, cur container.CPUStats
		want      float64
	}{
		{
			name: "zero predecessor is not a rate",
			// What Docker sends on the first frame of a fresh stream.
			prev: container.CPUStats{},
			cur: container.CPUStats{
				CPUUsage: busyCore, SystemUsage: 1.2e10, OnlineCPUs: 2,
			},
			want: 0,
		},
		{
			name: "one of two cores busy reads 100%",
			prev: container.CPUStats{
				CPUUsage: prev, SystemUsage: 1e10, OnlineCPUs: 2,
			},
			cur: container.CPUStats{
				CPUUsage: busyCore, SystemUsage: 1.2e10, OnlineCPUs: 2,
			},
			want: 100,
		},
		{
			name: "both cores busy exceeds 100%",
			prev: container.CPUStats{
				CPUUsage: prev, SystemUsage: 1e10, OnlineCPUs: 2,
			},
			cur: container.CPUStats{
				CPUUsage: busyBoth, SystemUsage: 1.2e10, OnlineCPUs: 2,
			},
			want: 200,
		},
		{
			name: "zero system delta is not a division",
			prev: container.CPUStats{
				CPUUsage: prev, SystemUsage: 1e10, OnlineCPUs: 2,
			},
			cur: container.CPUStats{
				CPUUsage: busyCore, SystemUsage: 1e10, OnlineCPUs: 2,
			},
			want: 0,
		},
		{
			name: "counter reset is not a rate",
			prev: container.CPUStats{
				CPUUsage: busyBoth, SystemUsage: 1.2e10, OnlineCPUs: 2,
			},
			cur: container.CPUStats{
				CPUUsage:    container.CPUUsage{TotalUsage: 1, PercpuUsage: []uint64{1, 0}},
				SystemUsage: 1.3e10, OnlineCPUs: 2,
			},
			want: 0,
		},
		{
			// A cpuset-limited container reports fewer online CPUs than
			// percpu_usage entries. The multiplier is what decides the answer
			// here: online_cpus=1 yields 50, while the percpu length of 8
			// would yield 400. system_cpu_usage is host-wide, so on a 2-core
			// host one busy core of two is 50% by this formula.
			name: "online_cpus wins over percpu length",
			prev: container.CPUStats{
				CPUUsage:    container.CPUUsage{TotalUsage: 1e9, PercpuUsage: make([]uint64, 8)},
				SystemUsage: 1e10, OnlineCPUs: 1,
			},
			cur: container.CPUStats{
				CPUUsage:    container.CPUUsage{TotalUsage: 2e9, PercpuUsage: make([]uint64, 8)},
				SystemUsage: 1.2e10, OnlineCPUs: 1,
			},
			want: 50,
		},
		{
			name: "percpu length is the fallback when online_cpus is absent",
			prev: container.CPUStats{
				CPUUsage: prev, SystemUsage: 1e10,
			},
			cur: container.CPUStats{
				CPUUsage: busyCore, SystemUsage: 1.2e10,
			},
			want: 100,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeCPUPercent(&tc.prev, &tc.cur)
			if diff := got - tc.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("computeCPUPercent() = %v, want %v", got, tc.want)
			}
		})
	}
	if got := computeCPUPercent(nil, nil); got != 0 {
		t.Errorf("computeCPUPercent(nil, nil) = %v, want 0", got)
	}
}

// Raw cgroup usage includes page cache, so a container that merely touched a lot
// of files looks like it is about to hit its limit.
func TestMemoryUsedBytesSubtractsReclaimableCache(t *testing.T) {
	cases := []struct {
		name string
		mem  container.MemoryStats
		want int64
	}{
		{
			name: "cgroup v2 inactive_file",
			mem:  container.MemoryStats{Usage: 1000, Stats: map[string]uint64{"inactive_file": 400}},
			want: 600,
		},
		{
			name: "cgroup v1 cache",
			mem:  container.MemoryStats{Usage: 1000, Stats: map[string]uint64{"cache": 250}},
			want: 750,
		},
		{
			name: "no cache stat reported",
			mem:  container.MemoryStats{Usage: 1000, Stats: map[string]uint64{}},
			want: 1000,
		},
		{
			name: "total_inactive_file must not be used (includes active_file)",
			mem:  container.MemoryStats{Usage: 1000, Stats: map[string]uint64{"total_inactive_file": 900, "active_file": 500}},
			want: 1000,
		},
		{
			name: "cache larger than usage falls back to raw",
			mem:  container.MemoryStats{Usage: 1000, Stats: map[string]uint64{"inactive_file": 5000}},
			want: 1000,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memoryUsedBytes(tc.mem); got != tc.want {
				t.Errorf("memoryUsedBytes() = %d, want %d", got, tc.want)
			}
		})
	}
}

// A container has several interfaces and Docker does not promise which one
// carries traffic, so both directions are summed.
func TestNetworkBytesSumsEveryInterface(t *testing.T) {
	rx, tx := networkBytes(map[string]container.NetworkStats{
		"eth0": {RxBytes: 100, TxBytes: 10},
		"eth1": {RxBytes: 20, TxBytes: 2},
	})
	if rx != 120 || tx != 12 {
		t.Errorf("networkBytes() = (%d, %d), want (120, 12)", rx, tx)
	}
}
