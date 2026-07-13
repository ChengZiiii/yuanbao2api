package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const maxLogEntries = 200

// LogEntry represents a single request record.
type LogEntry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Model    string `json:"model"`
	Status   int    `json:"status"`
	Duration string `json:"duration"`
	Note     string `json:"note"`
}

// requestLogger holds a ring buffer of recent request logs.
type requestLogger struct {
	mu    sync.Mutex
	ring  [maxLogEntries]LogEntry
	index int // next write position
	count int // total entries written (for knowing when ring is full)
}

var rl = &requestLogger{}

// LogRequest appends an entry to the ring buffer (thread-safe).
func LogRequest(method, path, model string, status int, duration time.Duration, note string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry := LogEntry{
		Time:     time.Now().Format("15:04:05"),
		Method:   method,
		Path:     path,
		Model:    model,
		Status:   status,
		Duration: fmt.Sprintf("%.1fs", duration.Seconds()),
		Note:     note,
	}
	rl.ring[rl.index] = entry
	rl.index = (rl.index + 1) % maxLogEntries
	rl.count++
}

// HandleLogs returns recent request logs (newest first).
func HandleLogs(c *gin.Context) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	total := rl.count
	if total > maxLogEntries {
		total = maxLogEntries
	}

	result := make([]LogEntry, 0, total)
	// Walk backwards from (index - 1) mod maxLogEntries
	pos := (rl.index - 1 + maxLogEntries) % maxLogEntries
	for i := 0; i < total; i++ {
		result = append(result, rl.ring[pos])
		pos = (pos - 1 + maxLogEntries) % maxLogEntries
	}

	c.JSON(http.StatusOK, result)
}
