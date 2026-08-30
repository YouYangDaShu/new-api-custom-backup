package logger

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// TestLogHelperConcurrentUse guards the shared rotation counters against the
// data race previously reported by `go test -race` (unsynchronized logCount++
// and setupLogWorking writes in logHelper). It fails under the race detector
// if those counters regress to plain variables.
func TestLogHelperConcurrentUse(t *testing.T) {
	common.LogWriterMu.Lock()
	oldWriter, oldErrWriter := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	common.LogWriterMu.Unlock()
	defer func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = oldWriter, oldErrWriter
		common.LogWriterMu.Unlock()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				logHelper(context.Background(), loggerINFO, "race regression check")
				logHelper(context.Background(), loggerError, "race regression check")
			}
		}()
	}
	wg.Wait()
}
