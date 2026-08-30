package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stored verification code allows a bounded number of wrong guesses, stays
// verifiable (without being consumed) while valid, and is gone after DeleteKey.
func TestVerifyCodeWithKeyAttemptLimitAndConsumption(t *testing.T) {
	const key = "user@example.com"
	RegisterVerificationCodeWithKey(key, "123456", EmailVerificationPurpose)

	// A correct guess succeeds repeatedly (verify does not consume: the reset
	// flow legitimately verifies twice; consumption is the caller's job).
	require.True(t, VerifyCodeWithKey(key, "123456", EmailVerificationPurpose))
	require.True(t, VerifyCodeWithKey(key, "123456", EmailVerificationPurpose))

	// Explicit consumption removes it.
	DeleteKey(key, EmailVerificationPurpose)
	assert.False(t, VerifyCodeWithKey(key, "123456", EmailVerificationPurpose))

	// Wrong guesses are bounded: after the limit the code is invalidated even
	// for the correct value.
	RegisterVerificationCodeWithKey(key, "654321", EmailVerificationPurpose)
	for i := 0; i < verificationMaxFailures; i++ {
		assert.False(t, VerifyCodeWithKey(key, "000000", EmailVerificationPurpose))
	}
	assert.False(t, VerifyCodeWithKey(key, "654321", EmailVerificationPurpose),
		"code must be invalidated after too many wrong guesses")

	// Failures below the limit do not lock out the legitimate holder.
	RegisterVerificationCodeWithKey(key, "112233", EmailVerificationPurpose)
	for i := 0; i < verificationMaxFailures-1; i++ {
		assert.False(t, VerifyCodeWithKey(key, "999999", EmailVerificationPurpose))
	}
	assert.True(t, VerifyCodeWithKey(key, "112233", EmailVerificationPurpose))
	DeleteKey(key, EmailVerificationPurpose)
}

func TestVerifyCodeWithKeyExpiry(t *testing.T) {
	const key = "expiry@example.com"
	RegisterVerificationCodeWithKey(key, "424242", EmailVerificationPurpose)
	verificationMutex.Lock()
	entry := verificationMap[EmailVerificationPurpose+key]
	entry.time = time.Now().Add(-time.Duration(VerificationValidMinutes+1) * time.Minute)
	verificationMap[EmailVerificationPurpose+key] = entry
	verificationMutex.Unlock()
	assert.False(t, VerifyCodeWithKey(key, "424242", EmailVerificationPurpose))
}
