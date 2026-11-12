package api

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/api-sandbox-links/backend/internal/db"
	"github.com/api-sandbox-links/backend/internal/orchestrator"
)

func newTestReader(r io.Reader) *bufio.Reader { return bufio.NewReaderSize(r, 64) }

// The browser reads these fields as required numbers and calls .toFixed() on
// them, so a stats frame that omits one crashes the metrics panel. The values
// that get omitted by a naive `omitempty` are the common ones: an idle
// container is at 0% CPU, and the first sample of any stream carries no network
// delta.
func TestStatsFrameAlwaysCarriesEveryMeasurement(t *testing.T) {
	at := time.Date(2026, 9, 26, 10, 15, 32, 0, time.UTC)
	raw, err := json.Marshal(statsEvent(orchestrator.ResourceSample{
		SandboxID: "sb-1",
		// Every measurement zero: the shape an idle sandbox reports.
		CPUPercent:       0,
		MemoryUsedBytes:  0,
		MemoryLimitBytes: 0,
		NetworkRxBytes:   0,
		NetworkTxBytes:   0,
		RecordedAt:       at,
	}))
	if err != nil {
		t.Fatalf("marshalling stats event: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshalling stats event: %v", err)
	}
	for _, field := range []string{
		"cpu_percent", "memory_used_bytes", "memory_limit_bytes",
		"network_rx_bytes", "network_tx_bytes", "recorded_at",
	} {
		v, ok := got[field]
		if !ok {
			t.Errorf("stats frame is missing %q; body was %s", field, raw)
			continue
		}
		if f, isNum := v.(float64); isNum && f != 0 {
			t.Errorf("%q = %v, want 0", field, v)
		}
	}
	if got["type"] != "stats" {
		t.Errorf("type = %v, want \"stats\"", got["type"])
	}
}

// A log frame carries none of the numeric fields, and must not invent zeros for
// them: the browser uses the discriminator to decide what a message is.
func TestLogFrameOmitsMeasurements(t *testing.T) {
	at := time.Date(2026, 9, 26, 10, 15, 30, 0, time.UTC)
	raw, err := json.Marshal(wsEvent{
		Type:      "log",
		Stream:    "stderr",
		Timestamp: &at,
		Line:      "boom",
	})
	if err != nil {
		t.Fatalf("marshalling log event: %v", err)
	}
	for _, field := range []string{"cpu_percent", "memory_used_bytes", "network_rx_bytes"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("log frame should not carry %q; body was %s", field, raw)
		}
	}
}

// A line longer than the cap must be truncated and the pump must keep going.
// Before this was a bufio.Scanner, one oversized line silently ended the whole
// stream, so a single base64 blob in a container's output killed the panel.
func TestReadLogLineTruncatesAndContinues(t *testing.T) {
	const max = 64
	// One reader for the whole test: bufio reads ahead, so wrapping the source
	// again per call would discard whatever the first call buffered.
	br := newTestReader(strings.NewReader(strings.Repeat("a", 500) + "\nshort\n"))

	line, truncated, err := readLogLine(br, max)
	if err != nil {
		t.Fatalf("first readLogLine: %v", err)
	}
	if !truncated {
		t.Error("expected the 500-byte line to be reported as truncated")
	}
	if len(line) != max {
		t.Errorf("truncated line length = %d, want %d", len(line), max)
	}

	// The point of the fix: the next line still arrives.
	line, truncated, err = readLogLine(br, max)
	if err != nil {
		t.Fatalf("second readLogLine: %v", err)
	}
	if truncated {
		t.Error("short line should not be marked truncated")
	}
	if string(line) != "short" {
		t.Errorf("second line = %q, want %q", line, "short")
	}
}

func TestReadLogLineHandlesFinalLineWithoutNewline(t *testing.T) {
	line, _, err := readLogLine(newTestReader(strings.NewReader("no trailing newline")), 64)
	if err != nil {
		t.Fatalf("readLogLine: %v", err)
	}
	if string(line) != "no trailing newline" {
		t.Errorf("line = %q", line)
	}
}

// Docker's stamp is peeled off so the Tail backfill is dated honestly, and an
// unparseable prefix is kept rather than dropped.
func TestSplitLogTimestamp(t *testing.T) {
	at, rest := splitLogTimestamp("2026-09-26T10:15:30.123456789Z listening on :3000")
	if rest != "listening on :3000" {
		t.Errorf("rest = %q", rest)
	}
	if at.Year() != 2026 || at.Minute() != 15 {
		t.Errorf("parsed time = %v", at)
	}

	at, rest = splitLogTimestamp("not-a-timestamp hello")
	if rest != "not-a-timestamp hello" {
		t.Errorf("unparseable prefix should be kept, got %q", rest)
	}
	if at.IsZero() {
		t.Error("unparseable prefix should fall back to a real time")
	}
}

// A hibernated sandbox must be reported as unavailable rather than as a
// stream that silently produces nothing.
func TestUnavailableReason(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		sb   *db.Sandbox
		want bool // expect a reason
	}{
		{"destroyed", &db.Sandbox{Status: db.StatusDeleted, DestroyedAt: &now}, true},
		{"hibernated", &db.Sandbox{Status: db.StatusHibernated}, true},
		{"building", &db.Sandbox{Status: db.StatusBuilding}, true},
		{"running without container", &db.Sandbox{Status: db.StatusRunning}, true},
		{"running", &db.Sandbox{Status: db.StatusRunning, ContainerID: "abc"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unavailableReason(tc.sb) != ""
			if got != tc.want {
				t.Errorf("unavailableReason() reported = %v, want %v", got, tc.want)
			}
		})
	}
}
