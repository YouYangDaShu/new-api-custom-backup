package logger

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestChannelTestLoggerSuppressesMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(common.ChannelTestSafeLogKey, true)
	LogError(ctx, "Authorization: Bearer test-sensitive-value; api_key=test-sensitive-value")

	assert.Contains(t, output.String(), "channel test diagnostic suppressed")
	assert.NotContains(t, output.String(), "test-sensitive-value")
	assert.NotContains(t, output.String(), "Authorization")
	assert.NotContains(t, output.String(), "api_key")
}

func TestRegularLoggerKeepsMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	LogError(ctx, "ordinary diagnostic")

	assert.Contains(t, output.String(), "ordinary diagnostic")
}
