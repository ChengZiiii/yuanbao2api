package api

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// exitFn is the function HandleRestart uses to terminate the process. It is a
// package-level variable so tests can intercept it without killing the test
// runner. Defaults to os.Exit.
var exitFn = os.Exit

// HandleRestart responds with 200, then asynchronously terminates the process
// after a small delay so the response can flush. The process is expected to
// be wrapped by restart.bat (Windows) or a process manager, which will
// relaunch it. New runtime values take effect on the next start.
func HandleRestart(c *gin.Context) {
	log.Println("收到重启请求，服务即将退出...")
	c.JSON(http.StatusOK, gin.H{"status": "restarting"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		exitFn(0)
	}()
}
