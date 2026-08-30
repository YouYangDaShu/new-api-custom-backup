package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

// The violation-fee marker must be detected when it appears only in the
// OpenAI-converted message while the outer error string differs (the second
// branch previously re-tested the outer error string instead of the message,
// under-applying the fee in that shape).
func TestHasCSAMViolationMarker_DetectsMessageOnlyMarkers(t *testing.T) {
	assert.False(t, HasCSAMViolationMarker(nil))

	messageOnly := func(message string) *types.NewAPIError {
		apiErr := types.WithOpenAIError(types.OpenAIError{Message: message, Type: "invalid_request_error"}, 400)
		// The converted message keeps the marker; the outer error does not.
		apiErr.Err = errors.New("upstream request failed")
		return apiErr
	}

	assert.True(t, HasCSAMViolationMarker(messageOnly("upstream: "+ContentViolatesUsageMarker)))
	assert.True(t, HasCSAMViolationMarker(messageOnly("upstream: "+CSAMViolationMarker)))
	assert.False(t, HasCSAMViolationMarker(messageOnly("ordinary upstream failure")))
}
