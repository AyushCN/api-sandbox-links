package api

import (
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

func sampleAt(t time.Time) orchestrator.ResourceSample {
	return orchestrator.ResourceSample{RecordedAt: t, CPUPercent: 1}
}

// The guard exists because a stats handler has two sources — the collector
// channel and a ticker that replays the newest sample — which can be ready in
// the same select iteration, with Go choosing pseudo-randomly. Unguarded, the
// browser gets the same sample twice about half the time.
func TestStatsReplayGuardDropsTheSameSampleTwice(t *testing.T) {
	var g statsReplayGuard
	at := time.Now().UTC()

	// The channel delivers it, then the ticker replays the very same sample.
	if !g.accept(sampleAt(at)) {
		t.Fatal("the first sample was rejected")
	}
	if g.accept(sampleAt(at)) {
		t.Error("the same sample was accepted twice")
	}
	// And again, for good measure.
	if g.accept(sampleAt(at)) {
		t.Error("a third delivery of the same sample was accepted")
	}
}

func TestStatsReplayGuardAcceptsOnlyStrictlyNewerSamples(t *testing.T) {
	var g statsReplayGuard
	base := time.Now().UTC()

	if !g.accept(sampleAt(base)) {
		t.Fatal("the first sample was rejected")
	}
	if !g.accept(sampleAt(base.Add(time.Second))) {
		t.Error("a newer sample was rejected")
	}
	// An out-of-order arrival must not rewind the guard, or a late frame would
	// be sent and the window would go backwards.
	if g.accept(sampleAt(base)) {
		t.Error("a sample older than the last sent one was accepted")
	}
	if !g.accept(sampleAt(base.Add(2 * time.Second))) {
		t.Error("the newest sample was rejected after an out-of-order one")
	}
}

func TestStatsReplayGuardAcceptsTheFirstSampleRegardlessOfStamp(t *testing.T) {
	// A collector that has been running for a while reports a timestamp well in
	// the past, and the zero value of the guard must not suppress it.
	var g statsReplayGuard
	if !g.accept(sampleAt(time.Now().UTC().Add(-6 * time.Hour))) {
		t.Error("a first sample with an old timestamp was rejected")
	}
}

func TestStatsReplayGuardHandlesAClockThatGoesBackwards(t *testing.T) {
	// Sample timestamps come from the collector's clock. A backwards step (NTP
	// correction, a container on a skewed host) must not produce a frame that is
	// older than the newest already sent, which is what `After` gives us.
	var g statsReplayGuard
	base := time.Now().UTC()
	if !g.accept(sampleAt(base)) {
		t.Fatal("the first sample was rejected")
	}
	if g.accept(sampleAt(base.Add(-time.Minute))) {
		t.Error("a sample from a backwards clock was accepted")
	}
}
