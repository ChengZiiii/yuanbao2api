package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func resetLogger() {
	rl = &requestLogger{}
}

func TestLogRequestAndHandleLogs(t *testing.T) {
	resetLogger()

	// Log a single entry
	LogRequest("POST", "/v1/chat/completions", "deep_seek_v3", 200, 500*time.Millisecond, "")

	// Retrieve via HandleLogs
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/logs", nil)
	HandleLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Method != "POST" {
		t.Errorf("expected Method=POST, got %s", entries[0].Method)
	}
	if entries[0].Path != "/v1/chat/completions" {
		t.Errorf("expected Path=/v1/chat/completions, got %s", entries[0].Path)
	}
	if entries[0].Model != "deep_seek_v3" {
		t.Errorf("expected Model=deep_seek_v3, got %s", entries[0].Model)
	}
	if entries[0].Status != 200 {
		t.Errorf("expected Status=200, got %d", entries[0].Status)
	}
	if entries[0].Duration == "" {
		t.Errorf("expected non-empty Duration")
	}
}

func TestHandleLogsNewestFirst(t *testing.T) {
	resetLogger()

	// Log 3 entries in order
	LogRequest("GET", "/a", "model1", 200, 100*time.Millisecond, "")
	time.Sleep(2 * time.Millisecond)
	LogRequest("POST", "/b", "model2", 201, 200*time.Millisecond, "")
	time.Sleep(2 * time.Millisecond)
	LogRequest("DELETE", "/c", "model3", 204, 300*time.Millisecond, "")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/logs", nil)
	HandleLogs(c)

	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Newest first: DELETE, POST, GET
	if entries[0].Method != "DELETE" {
		t.Errorf("expected newest first Method=DELETE, got %s", entries[0].Method)
	}
	if entries[1].Method != "POST" {
		t.Errorf("expected second Method=POST, got %s", entries[1].Method)
	}
	if entries[2].Method != "GET" {
		t.Errorf("expected last Method=GET, got %s", entries[2].Method)
	}
}

func TestRingBufferWrap(t *testing.T) {
	resetLogger()

	// Write 250 entries (exceeds ring buffer size of 200)
	for i := 0; i < 250; i++ {
		LogRequest("GET", "/wrap", "m", 200, time.Millisecond, "")
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/logs", nil)
	HandleLogs(c)

	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should return at most 200 entries
	if len(entries) != 200 {
		t.Errorf("expected 200 entries (ring buffer size), got %d", len(entries))
	}
}

func TestConcurrentSafety(t *testing.T) {
	resetLogger()

	var wg sync.WaitGroup
	n := 100

	// Concurrent writes
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			LogRequest("GET", "/concurrent", "m", 200, time.Millisecond, "")
		}()
	}
	wg.Wait()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/logs", nil)
	HandleLogs(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should have some entries (may be less than 100 due to wrap, but at least 1)
	if len(entries) == 0 {
		t.Errorf("expected at least 1 entry after concurrent writes, got 0")
	}
}
