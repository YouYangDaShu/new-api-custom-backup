package aws

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFormatRequestSuppressesChannelTestPayloadInDebugLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer

	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	previousDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() {
		common.DebugEnabled = previousDebug
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set(common.ChannelTestSafeLogKey, true)
	_, err := formatRequest(ctx, strings.NewReader(`{"messages":[{"role":"user","content":"sensitive-message"}],"system":"sensitive-system","tools":[{"name":"tool-secret"}]} `), http.Header{})
	require.NoError(t, err)

	require.Contains(t, output.String(), "channel test diagnostic suppressed")
	require.NotContains(t, output.String(), "sensitive-message")
	require.NotContains(t, output.String(), "sensitive-system")
	require.NotContains(t, output.String(), "tool-secret")
}
