package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// notCalledSentinel marks exitFn as not yet invoked. We need this because
// the real exit code is 0, which is indistinguishable from the zero value
// of int32.
const notCalledSentinel = int32(-1)

func TestHandleRestart_RespondsAndCallsExit(t *testing.T) {
	// 替换 exitFn，避免真退出
	var called int32 = notCalledSentinel
	origExit := exitFn
	exitFn = func(code int) {
		atomic.StoreInt32(&called, int32(code))
	}
	defer func() { exitFn = origExit }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/restart", nil)
	HandleRestart(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// 等异步退出（HandleRestart 启动 goroutine，500ms 后调用 exitFn）
	deadline := time.After(2 * time.Second)
	for {
		if atomic.LoadInt32(&called) != notCalledSentinel {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("exitFn was not called within 2s")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("expected exit code 0, got %d", got)
	}
}
