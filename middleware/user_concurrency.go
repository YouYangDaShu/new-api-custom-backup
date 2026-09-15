package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const userConcurrencyRequestKey = "user_concurrency_request_id"

// UserConcurrency gates synchronous relay requests after authentication and
// before channel distribution. Realtime and asynchronous task routes opt out.
func UserConcurrency() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") || strings.HasPrefix(c.Request.URL.Path, "/v1/tasks") || strings.Contains(c.Request.URL.Path, "/videos/") {
			c.Next()
			return
		}
		userID := c.GetInt("id")
		role := c.GetInt("role")
		if userID <= 0 || role >= common.RoleAdminUser {
			c.Next()
			return
		}
		limit := c.GetInt("user_concurrency")
		if !c.GetBool("user_concurrency_override") {
			limit = common.GetUserConcurrencyDefault()
		}
		requestID := c.GetString(common.RequestIdKey)
		if requestID == "" {
			requestID = common.NewRequestId()
			c.Set(common.RequestIdKey, requestID)
		}
		acquired, err := service.AcquireUserConcurrency(c.Request.Context(), userID, limit, requestID)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrUserConcurrencyLimit):
				c.Header("Retry-After", "1")
				c.Header("X-User-Concurrency-Limit", stringInt(limit))
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, "user concurrency limit exceeded", types.ErrorCode("user_concurrency_limit"))
			default:
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "user concurrency storage unavailable", types.ErrorCode("user_concurrency_unavailable"))
			}
			return
		}
		if !acquired {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, "user concurrency limit exceeded", types.ErrorCode("user_concurrency_limit"))
			return
		}
		c.Set(userConcurrencyRequestKey, requestID)
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if releaseErr := service.ReleaseUserConcurrency(releaseCtx, userID, requestID); releaseErr != nil {
				common.SysLog("failed to release user concurrency slot: " + releaseErr.Error())
			}
		}()
		c.Next()
	}
}

func stringInt(value int) string {
	if value == 0 {
		return "0"
	}
	return strconv.Itoa(value)
}
