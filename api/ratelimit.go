package api

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ErrQueueTimeout is returned by Acquire when a request waits longer than the
// configured queue timeout without obtaining a concurrency slot.
var ErrQueueTimeout = errors.New("queue timeout: server is busy, request exceeded max wait time")

// RateLimiter enforces a strict concurrency limit on upstream Yuanbao access.
// It uses a buffered channel as a semaphore: at most MaxConcurrency requests
// may be "in flight" (actively talking to upstream) at once. Additional
// requests block (FIFO, channel ordering) until a slot frees up or the queue
// timeout elapses, at which point they receive ErrQueueTimeout.
type RateLimiter struct {
	sem            chan struct{}
	maxConcurrency int
	queueTimeout   time.Duration
	cooldown       time.Duration

	// inflight counts requests currently holding a slot (upstream in progress).
	inflight int64
	// waiting counts requests currently blocked inside Acquire.
	waiting int64
}

// globalRateLimiter is the process-wide gate shared by both the OpenAI and
// Anthropic handlers. It is initialized once at startup via InitRateLimiter.
var globalRateLimiter *RateLimiter

// InitRateLimiter builds the global rate limiter from persisted runtime
// overrides, environment variables, and built-in defaults (in that order). It
// also records the resolved values on the server config for /api/config.
// Safe to call once at startup.
func InitRateLimiter() *RateLimiter {
	maxC := getEnvInt("MAX_CONCURRENCY", 1)
	if maxC < 1 {
		maxC = 1
	}
	qTimeout := time.Duration(getEnvInt("QUEUE_TIMEOUT_SECONDS", 120)) * time.Second
	if qTimeout < 0 {
		qTimeout = 120 * time.Second
	}
	cooldown := time.Duration(getEnvInt("REQUEST_COOLDOWN_MS", 0)) * time.Millisecond
	if cooldown < 0 {
		cooldown = 0
	}

	// Persisted values override env defaults (runtime_config.json > env > built-in).
	rc := LoadRuntimeConfig()
	if rc.MaxConcurrency != nil && *rc.MaxConcurrency > 0 {
		maxC = *rc.MaxConcurrency
	}
	if rc.QueueTimeoutSeconds != nil && *rc.QueueTimeoutSeconds > 0 {
		qTimeout = time.Duration(*rc.QueueTimeoutSeconds) * time.Second
	}
	if rc.RequestCooldownMs != nil && *rc.RequestCooldownMs >= 0 {
		cooldown = time.Duration(*rc.RequestCooldownMs) * time.Millisecond
	}

	globalRateLimiter = &RateLimiter{
		sem:            make(chan struct{}, maxC),
		maxConcurrency: maxC,
		queueTimeout:   qTimeout,
		cooldown:       cooldown,
	}

	// Surface the resolved values on the server config for visibility.
	// Persisted Yuanbao cookie is also restored to the in-memory slot so it
	// survives process restarts (mirrors SyncAgentID for AGENT_ID). The env
	// var remains a fallback for the empty / absent on-disk case.
	serverConfigLock.Lock()
	serverConfig.MaxConcurrency = maxC
	serverConfig.QueueTimeoutSeconds = int(qTimeout.Seconds())
	serverConfig.RequestCooldownMs = int(cooldown.Milliseconds())
	if rc.YuanbaoCookie != nil {
		serverConfig.YuanbaoCookie = rc.YuanbaoCookie
	}
	serverConfigLock.Unlock()

	return globalRateLimiter
}

// GetRateLimiter returns the global rate limiter (may be nil if not yet init).
func GetRateLimiter() *RateLimiter {
	return globalRateLimiter
}

// Acquire blocks until a concurrency slot is available or the queue timeout (or
// the request context) elapses. On success the caller must call Release exactly
// once when done (typically via defer).
func (rl *RateLimiter) Acquire(ctx context.Context) error {
	atomic.AddInt64(&rl.waiting, 1)
	defer atomic.AddInt64(&rl.waiting, -1)

	// Fast path: take an immediately available slot without allocating a timer.
	select {
	case rl.sem <- struct{}{}:
		atomic.AddInt64(&rl.inflight, 1)
		return nil
	default:
	}

	// Slow path: queue (FIFO) until a slot frees or the timeout fires.
	tctx, cancel := context.WithTimeout(ctx, rl.queueTimeout)
	defer cancel()

	select {
	case rl.sem <- struct{}{}:
		atomic.AddInt64(&rl.inflight, 1)
		return nil
	case <-tctx.Done():
		if ctx.Err() != nil {
			// Client disconnected / request canceled while queued.
			return ctx.Err()
		}
		return ErrQueueTimeout
	}
}

// Release frees the slot. It first applies an optional cooldown so the upstream
// is not immediately hammered by the next queued request, which lowers the
// chance of risk-control triggers. Must be called exactly once per Acquire.
func (rl *RateLimiter) Release() {
	if rl.cooldown > 0 {
		time.Sleep(rl.cooldown)
	}
	<-rl.sem
	atomic.AddInt64(&rl.inflight, -1)
}

// Inflight returns the number of requests currently holding a slot.
func (rl *RateLimiter) Inflight() int64 { return atomic.LoadInt64(&rl.inflight) }

// Waiting returns the number of requests currently blocked in the queue.
func (rl *RateLimiter) Waiting() int64 { return atomic.LoadInt64(&rl.waiting) }

// MaxConcurrency returns the configured concurrency limit.
func (rl *RateLimiter) MaxConcurrency() int { return rl.maxConcurrency }

// QueueTimeout returns the configured queue wait limit.
func (rl *RateLimiter) QueueTimeout() time.Duration { return rl.queueTimeout }

// Cooldown returns the configured release cooldown.
func (rl *RateLimiter) Cooldown() time.Duration { return rl.cooldown }
