// Package cache wraps Redis, the platform's ephemeral/hot-state store.
//
// Redis deliberately holds only two kinds of data (see BUILD_PROMPT section 2):
//  1. last-request markers used by the idle-timeout reaper,
//  2. the latest resource sample per sandbox, flushed to Postgres on a timer.
//
// Everything durable lives in Postgres; nothing in Redis is an authoritative
// record of anything.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is the typed wrapper around a go-redis client.
type Redis struct {
	client *redis.Client
}

// New connects to Redis and verifies connectivity with a ping.
func New(ctx context.Context, url string) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parsing redis url: %w", err)
	}
	client := redis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: unable to reach redis: %w", err)
	}
	return &Redis{client: client}, nil
}

// Ping verifies connectivity to Redis.
func (r *Redis) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("cache: redis client not initialized")
	}
	return r.client.Ping(ctx).Err()
}

// ---- last-request-at markers (idle timeout) -----------------------------
//
// Each request through the sandbox gateway calls Touch on the sandbox's key
// with a TTL equal to its idle timeout. The idle reaper hibernates a running
// sandbox when its key is absent — i.e. no request has bumped it within the
// timeout window. This TTL-as-timer trick means the reaper needs no stored
// timestamps and no clock skew issues.

func lastRequestKey(sandboxID string) string {
	return "lr:" + sandboxID
}

// Touch marks sandboxID as active for the next ttlSeconds. Called on every
// proxied request (hot path; keep cheap).
func (r *Redis) Touch(ctx context.Context, sandboxID string, ttlSeconds int) error {
	if err := r.client.Set(ctx, lastRequestKey(sandboxID), time.Now().Unix(), time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("cache: touching last-request marker: %w", err)
	}
	return nil
}

// IsIdle reports whether no request has touched the sandbox's marker, meaning
// it has likely been idle at least as long as its configured timeout.
func (r *Redis) IsIdle(ctx context.Context, sandboxID string) bool {
	n, err := r.client.Exists(ctx, lastRequestKey(sandboxID)).Result()
	if err != nil {
		return false
	}
	return n == 0
}

// Clear removes a sandbox's marker (used when hibernating so a fresh wake is
// counted as brand-new activity).
func (r *Redis) Clear(ctx context.Context, sandboxID string) error {
	return r.client.Del(ctx, lastRequestKey(sandboxID)).Err()
}

// ---- latest live resource sample (resource dashboard) -------------------
//
// The stats collector overwrites one key per sandbox on every sample, and the
// flush worker reads it on its own slower cadence to build the history chart.
// A single key rather than a growing list, because the 1s feed exists to drive
// a live readout and only its newest value is ever worth persisting.

func latestStatKey(sandboxID string) string {
	return "stats:latest:" + sandboxID
}

// latestStatTTL is deliberately short. If a container's stats stream dies, the
// key must expire rather than serve a frozen reading as though it were live.
const latestStatTTL = 10 * time.Second

// SetLatestStat overwrites the sandbox's most recent sample. The TTL is
// refreshed on every write, so the key's lifetime tracks the collector's.
func (r *Redis) SetLatestStat(ctx context.Context, sandboxID string, sample []byte) error {
	if err := r.client.Set(ctx, latestStatKey(sandboxID), sample, latestStatTTL).Err(); err != nil {
		return fmt.Errorf("cache: writing latest stat: %w", err)
	}
	return nil
}

// LatestStat returns the sandbox's most recent sample, or nil when the key is
// absent or has expired.
func (r *Redis) LatestStat(ctx context.Context, sandboxID string) ([]byte, error) {
	raw, err := r.client.Get(ctx, latestStatKey(sandboxID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache: reading latest stat: %w", err)
	}
	return raw, nil
}

// StatFrame is the JSON shape published in Redis between the stats collector
// and the Postgres flush worker.
type StatFrame struct {
	SandboxID        string    `json:"sandbox_id"`
	CPUPercent       float32   `json:"cpu_percent"`
	MemoryUsedBytes  int64     `json:"memory_used_bytes"`
	MemoryLimitBytes int64     `json:"memory_limit_bytes"`
	NetworkRxBytes   int64     `json:"network_rx_bytes"`
	NetworkTxBytes   int64     `json:"network_tx_bytes"`
	RecordedAt       time.Time `json:"recorded_at"`
}

// EncodeStatFrame renders a sample for publishing.
func EncodeStatFrame(f StatFrame) ([]byte, error) {
	return json.Marshal(f)
}

// DecodeStatFrame parses a published sample.
func DecodeStatFrame(raw []byte) (*StatFrame, error) {
	var f StatFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("cache: decoding published stat sample: %w", err)
	}
	return &f, nil
}
