package api

import (
	"context"
	"errors"
	"sync"
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

// LimiterConfigGetter returns the resolved maxC / queueTimeout / cooldown
// values for the named provider. It is supplied to NewLimiterManager by the
// api package so the manager stays decoupled from the runtime config schema.
type LimiterConfigGetter func(name string) (maxC, queueTimeout, cooldown int)

// LimiterManager lazily constructs one RateLimiter per provider name.
// Unknown names get a pass-through limiter (effectively unlimited
// concurrency) so Registry.Route failure is the single source of truth
// for "I do not know this provider".
type LimiterManager struct {
	mu       sync.RWMutex
	limiters map[string]*RateLimiter
	getter   LimiterConfigGetter
	known    map[string]bool
}

// NewLimiterManager creates an empty manager. Pass the result to
// SetConfigGetter before calling For for the first time; if the getter
// is nil the manager falls back to the built-in defaults (1 / 120 / 0).
func NewLimiterManager() *LimiterManager {
	return &LimiterManager{
		limiters: map[string]*RateLimiter{},
		known:    map[string]bool{},
	}
}

// SetConfigGetter installs (or replaces) the function used to resolve a
// provider's rate-limit parameters at the time its limiter is first
// constructed. The manager caches the resulting limiter; runtime
// changes to the parameters require a restart (the buffered channel
// cannot be resized).
func (m *LimiterManager) SetConfigGetter(getter LimiterConfigGetter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getter = getter
}

// MarkKnown registers one or more provider names as "known" so For
// will construct a real limiter for them. Names not in this set fall
// through to a never-blocking pass-through limiter (used for typo'd
// or unconfigured names; the registry's Route should have rejected
// them already).
func (m *LimiterManager) MarkKnown(names ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range names {
		m.known[n] = true
	}
}

// For returns the limiter for the named provider, constructing it on
// first call. Unknown names return a pass-through limiter with
// maxConcurrency = 1<<30 so callers cannot accidentally deadlock while
// testing or in transitional states.
func (m *LimiterManager) For(name string) *RateLimiter {
	if name == "" {
		return m.passThrough()
	}
	m.mu.RLock()
	if !m.known[name] {
		m.mu.RUnlock()
		return m.passThrough()
	}
	rl, ok := m.limiters[name]
	m.mu.RUnlock()
	if ok {
		return rl
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if rl, ok = m.limiters[name]; ok {
		return rl
	}

	maxC, qTimeout, cooldown := 1, 120, 0
	if m.getter != nil {
		maxC, qTimeout, cooldown = m.getter(name)
	}
	if maxC < 1 {
		maxC = 1
	}
	if qTimeout < 1 {
		qTimeout = 120
	}
	if cooldown < 0 {
		cooldown = 0
	}

	rl = &RateLimiter{
		sem:            make(chan struct{}, maxC),
		maxConcurrency: maxC,
		queueTimeout:   time.Duration(qTimeout) * time.Second,
		cooldown:       time.Duration(cooldown) * time.Millisecond,
	}
	m.limiters[name] = rl
	return rl
}

// passThrough returns (and caches) a never-blocking limiter used for
// placeholder / unknown provider names.
func (m *LimiterManager) passThrough() *RateLimiter {
	m.mu.RLock()
	rl, ok := m.limiters[""]
	m.mu.RUnlock()
	if ok {
		return rl
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if rl, ok = m.limiters[""]; ok {
		return rl
	}
	rl = &RateLimiter{
		sem:            make(chan struct{}, 1<<30),
		maxConcurrency: 1 << 30,
		queueTimeout:   120 * time.Second,
		cooldown:       0,
	}
	m.limiters[""] = rl
	return rl
}

// Reset clears every cached limiter. Used by tests that swap the
// config getter between scenarios.
func (m *LimiterManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limiters = map[string]*RateLimiter{}
	m.known = map[string]bool{}
}

// globalManager is the process-wide LimiterManager used by every
// handler. main.go is expected to call InitLimiterManager once at
// startup; subsequent calls to For("...") lazily materialize per-
// provider limiters.
var globalManager *LimiterManager

// InitLimiterManager constructs the process-wide LimiterManager and
// installs the getter that resolves rate-limit parameters from the
// persisted RuntimeConfig, environment variables, and built-in
// defaults (in that order). It also materializes the yuanbao
// limiter up front (so /api/status has data immediately after
// startup) and mirrors the persisted Yuanbao cookie into the in-
// memory server config so EffectiveYuanbaoCookie keeps working
// across restarts.
//
// Returns the yuanbao limiter for backward compatibility — legacy
// tests and callers still call InitRateLimiter / GetRateLimiter, so
// those continue to function unchanged.
func InitLimiterManager() *LimiterManager {
	rc := LoadRuntimeConfig()

	maxC := getEnvInt("MAX_CONCURRENCY", 1)
	if maxC < 1 {
		maxC = 1
	}
	qTimeout := getEnvInt("QUEUE_TIMEOUT_SECONDS", 120)
	if qTimeout < 1 {
		qTimeout = 120
	}
	cooldown := getEnvInt("REQUEST_COOLDOWN_MS", 0)
	if cooldown < 0 {
		cooldown = 0
	}
	if rc.MaxConcurrencyField() != nil && *rc.MaxConcurrencyField() > 0 {
		maxC = *rc.MaxConcurrencyField()
	}
	if rc.QueueTimeoutSecondsField() != nil && *rc.QueueTimeoutSecondsField() > 0 {
		qTimeout = *rc.QueueTimeoutSecondsField()
	}
	if rc.RequestCooldownMsField() != nil && *rc.RequestCooldownMsField() >= 0 {
		cooldown = *rc.RequestCooldownMsField()
	}

	globalManager = NewLimiterManager()
	globalManager.SetConfigGetter(func(name string) (int, int, int) {
		return maxC, qTimeout, cooldown
	})
	// Mark every provider shipped with the binary as known so For(name)
	// returns a real limiter. Future providers added at runtime must be
	// MarkKnown'd too (or they fall through to pass-through, which is
	// the right behavior for typo'd names).
	globalManager.MarkKnown("yuanbao", "qwen", "kimi")

	// Materialize the yuanbao limiter so /api/status has live numbers
	// immediately after startup; other providers are constructed on
	// first request.
	_ = globalManager.For("yuanbao")

	// Surface the resolved values on the server config for visibility.
	// Persisted Yuanbao cookie is also restored to the in-memory slot
	// so it survives process restarts (mirrors SyncAgentID for
	// AGENT_ID). The env var remains a fallback for the empty /
	// absent on-disk case.
	serverConfigLock.Lock()
	serverConfig.MaxConcurrency = maxC
	serverConfig.QueueTimeoutSeconds = qTimeout
	serverConfig.RequestCooldownMs = cooldown
	if rc.YuanbaoCookieField() != nil {
		serverConfig.YuanbaoCookie = rc.YuanbaoCookieField()
	}
	serverConfigLock.Unlock()

	return globalManager
}

// InitRateLimiter keeps the legacy entry point. It builds (or reuses)
// the global manager and returns the yuanbao limiter so existing
// tests and callers continue to compile.
func InitRateLimiter() *RateLimiter {
	mgr := InitLimiterManager()
	return mgr.For("yuanbao")
}

// GetLimiterManager returns the process-wide LimiterManager (may be
// nil if not yet initialized).
func GetLimiterManager() *LimiterManager {
	return globalManager
}

// GetRateLimiter returns the yuanbao RateLimiter for callers that
// only need the default-provider limiter (backward-compat shim).
func GetRateLimiter() *RateLimiter {
	if globalManager == nil {
		return nil
	}
	return globalManager.For("yuanbao")
}

// ResetLimiterManager clears the global manager. Tests use it between
// scenarios to drop cached limiters.
func ResetLimiterManager() {
	if globalManager != nil {
		globalManager.Reset()
	}
}