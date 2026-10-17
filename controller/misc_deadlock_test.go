package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusTokenRankingBoolCompatibility(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})
	cases := []struct {
		name, raw    string
		exists, want bool
	}{
		{"missing", "", false, true}, {"empty", "", true, true},
		{"true", "true", true, true}, {"one", "1", true, true},
		{"lower t", "t", true, true}, {"upper T", "T", true, true},
		{"upper TRUE", "TRUE", true, true}, {"title True", "True", true, true},
		{"false", "false", true, false}, {"zero", "0", true, false},
		{"lower f", "f", true, false}, {"upper F", "F", true, false},
		{"upper FALSE", "FALSE", true, false}, {"title False", "False", true, false},
		{"invalid", "enabled", true, false}, {"spaces", " true ", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			if tc.exists {
				common.OptionMap["TokenRankingEnabled"] = tc.raw
			} else {
				delete(common.OptionMap, "TokenRankingEnabled")
			}
			common.OptionMapRWMutex.Unlock()
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
			GetStatus(c)
			require.Equal(t, http.StatusOK, response.Code)
			var payload struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			require.Equal(t, tc.want, payload.Data["token_ranking_enabled"])
		})
	}
}

// Run contention in a child: the pre-fix recursive RLock cannot be cancelled.
func TestGetStatusWithConcurrentOptionWriter(t *testing.T) {
	if os.Getenv("NEWAPI_STATUS_CONTENTION_CHILD") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestGetStatusWithConcurrentOptionWriter$", "-test.timeout=12s")
		cmd.Env = append(os.Environ(), "NEWAPI_STATUS_CONTENTION_CHILD=1", "GIN_MODE=release")
		output, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), "status handler deadlocked with an option writer: %s", output)
		require.NoError(t, err, "status contention child failed: %s", output)
		return
	}
	common.OptionMap = map[string]string{"TokenRankingEnabled": "true"}
	// Initialize lazy settings before exercising concurrent status reads.
	warm := httptest.NewRecorder()
	warmContext, _ := gin.CreateTestContext(warm)
	warmContext.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(warmContext)
	require.Equal(t, http.StatusOK, warm.Code)
	start := make(chan struct{})
	stop := make(chan struct{})
	var readers sync.WaitGroup
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		<-start
		for {
			select {
			case <-stop:
				return
			default:
			}
			common.OptionMapRWMutex.Lock()
			common.OptionMap["TokenRankingEnabled"] = "true"
			common.OptionMapRWMutex.Unlock()
			runtime.Gosched()
		}
	}()
	for i := 0; i < 24; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for j := 0; j < 50; j++ {
				response := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(response)
				c.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
				GetStatus(c)
				if response.Code != http.StatusOK {
					t.Errorf("status=%d", response.Code)
					return
				}
			}
		}()
	}
	close(start)
	readers.Wait()
	close(stop)
	writer.Wait()
}
