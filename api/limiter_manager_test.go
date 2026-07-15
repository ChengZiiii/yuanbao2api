package api

import (
	"sync"
	"testing"
)

func TestLimiterManager_IndependentPerProvider(t *testing.T) {
	mgr := NewLimiterManager()
	mgr.MarkKnown("yuanbao", "qwen")
	mgr.SetConfigGetter(func(name string) (int, int, int) {
		switch name {
		case "yuanbao":
			return 1, 120, 0
		case "qwen":
			return 3, 120, 0
		default:
			return 1, 120, 0
		}
	})

	yuanbao := mgr.For("yuanbao")
	qwen := mgr.For("qwen")
	if yuanbao == qwen {
		t.Fatal("yuanbao and qwen should be distinct limiter instances")
	}
	if yuanbao.MaxConcurrency() != 1 {
		t.Errorf("yuanbao maxC: got %d want 1", yuanbao.MaxConcurrency())
	}
	if qwen.MaxConcurrency() != 3 {
		t.Errorf("qwen maxC: got %d want 3", qwen.MaxConcurrency())
	}
}

func TestLimiterManager_UnknownReturnsPassThrough(t *testing.T) {
	mgr := NewLimiterManager()
	rl := mgr.For("nonexistent")
	if rl.MaxConcurrency() < 1000 {
		t.Errorf("unknown name: maxC %d should be large (pass-through)", rl.MaxConcurrency())
	}
	// Empty name also passes through.
	rl2 := mgr.For("")
	if rl2.MaxConcurrency() < 1000 {
		t.Errorf("empty name: maxC %d should be large", rl2.MaxConcurrency())
	}
}

func TestLimiterManager_CachesPerName(t *testing.T) {
	mgr := NewLimiterManager()
	mgr.MarkKnown("yuanbao")
	calls := 0
	mgr.SetConfigGetter(func(name string) (int, int, int) {
		calls++
		return 2, 120, 0
	})
	first := mgr.For("yuanbao")
	second := mgr.For("yuanbao")
	if first != second {
		t.Fatal("For should return the same instance on repeated calls")
	}
	if calls != 1 {
		t.Errorf("getter calls: got %d want 1 (constructor should run once)", calls)
	}
}

func TestLimiterManager_ConcurrentFirstFor(t *testing.T) {
	mgr := NewLimiterManager()
	mgr.MarkKnown("yuanbao")
	calls := 0
	var mu sync.Mutex
	mgr.SetConfigGetter(func(name string) (int, int, int) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return 1, 120, 0
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.For("yuanbao")
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Errorf("concurrent first For should call getter exactly once, got %d", calls)
	}
}