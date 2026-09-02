package controller

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestContextLengthErrorIsNotRetried(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "context length exceeded",
		Type:    "context_length_exceeded",
		Code:    "context_length_exceeded",
	}, 400, types.ErrOptionWithSkipRetry())
	if shouldRetry(c, err, 3) {
		t.Fatal("context length rejection must terminate without retry or channel switch")
	}
}

func TestShouldRetryDecisionOrder(t *testing.T) {
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}
	channelErr := types.NewError(fmt.Errorf("upstream"), types.ErrorCode("channel:timeout"), types.ErrOptionWithStatusCode(500))
	for _, tc := range []struct {
		name  string
		setup func(*gin.Context)
		err   *types.NewAPIError
		times int
		want  bool
	}{
		{"affinity before channel", func(c *gin.Context) { c.Set("channel_affinity_skip_retry_on_failure", true) }, channelErr, 3, false},
		{"skip error before channel", nil, types.NewError(fmt.Errorf("bad"), types.ErrorCodeBadResponseBody, types.ErrOptionWithStatusCode(500)), 3, false},
		{"always skip 524 before channel", nil, types.NewError(fmt.Errorf("timeout"), types.ErrorCode("channel:timeout"), types.ErrOptionWithStatusCode(524)), 3, false},
		{"specific channel before budget", func(c *gin.Context) { c.Set("specific_channel_id", 7) }, channelErr, 3, false},
		{"budget before channel", nil, channelErr, 0, false},
		{"channel error after budget", nil, channelErr, 3, true},
		{"status retry after channel", nil, types.NewError(fmt.Errorf("server"), types.ErrorCode("other"), types.ErrOptionWithStatusCode(500)), 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCtx()
			if tc.setup != nil {
				tc.setup(c)
			}
			if got := shouldRetry(c, tc.err, tc.times); got != tc.want {
				t.Fatalf("shouldRetry=%v want %v", got, tc.want)
			}
		})
	}
}

func TestShouldRetryChannelError524IsAlwaysSkipped(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := types.NewError(fmt.Errorf("upstream timeout"), types.ErrorCode("channel:timeout"), types.ErrOptionWithStatusCode(524))
	if !types.IsChannelError(err) {
		t.Fatal("fixture must be a channel error")
	}
	if shouldRetry(c, err, 3) {
		t.Fatal("channel error with HTTP 524 must be non-retryable")
	}
}
