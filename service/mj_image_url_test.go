package service

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MJ image proxy signature is the only access control on an otherwise
// anonymous route: it must accept exactly the issued (id, expiry, sig) triple
// and reject everything else, including expired and cross-task reuse.
func TestVerifyMjImageSignature(t *testing.T) {
	const mjId = "1118138338562560102"
	future := time.Now().Add(10 * time.Minute).Unix()
	futureStr := strconv.FormatInt(future, 10)
	sig := MjImageSignature(mjId, future)

	require.NoError(t, VerifyMjImageSignature(mjId, futureStr, sig))

	// Anonymous / missing parameters.
	assert.Error(t, VerifyMjImageSignature(mjId, "", ""))
	assert.Error(t, VerifyMjImageSignature(mjId, futureStr, ""))
	assert.Error(t, VerifyMjImageSignature(mjId, "", sig))

	// Expired signature (signed correctly for a past expiry).
	past := time.Now().Add(-time.Minute).Unix()
	assert.Error(t, VerifyMjImageSignature(mjId, strconv.FormatInt(past, 10), MjImageSignature(mjId, past)))

	// Tampering: another user's task id, shifted expiry, or a forged sig.
	assert.Error(t, VerifyMjImageSignature("other-task", futureStr, sig))
	assert.Error(t, VerifyMjImageSignature(mjId, strconv.FormatInt(future+1, 10), sig))
	assert.Error(t, VerifyMjImageSignature(mjId, futureStr, "deadbeef"))
	assert.Error(t, VerifyMjImageSignature(mjId, "not-a-number", sig))
}

func TestSignedMjImagePathRoundTrip(t *testing.T) {
	const mjId = "task-123"
	path := SignedMjImagePath(mjId)
	var ex int64
	var sig string
	_, err := fmt.Sscanf(path, "/mj/image/"+mjId+"?ex=%d&sig=%s", &ex, &sig)
	require.NoError(t, err)
	require.NoError(t, VerifyMjImageSignature(mjId, strconv.FormatInt(ex, 10), sig))
}
