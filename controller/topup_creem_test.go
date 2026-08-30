package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

// The Creem webhook issues quota, so verification must fail closed whenever the
// webhook secret is empty — including in test mode.
func TestCreemWebhookVerificationFailsClosedWithoutSecret(t *testing.T) {
	originalTestMode := setting.CreemTestMode
	t.Cleanup(func() { setting.CreemTestMode = originalTestMode })

	payload := `{"eventType":"checkout.completed"}`
	for _, testMode := range []bool{false, true} {
		setting.CreemTestMode = testMode
		require.False(t, verifyCreemSignature(payload, "", ""))
		require.False(t, verifyCreemSignature(payload, generateCreemSignature(payload, "guessed-secret"), ""))
	}
}

func TestCreemWebhookVerificationRequiresMatchingSignature(t *testing.T) {
	const secret = "creem-test-secret"
	payload := `{"eventType":"checkout.completed"}`
	require.True(t, verifyCreemSignature(payload, generateCreemSignature(payload, secret), secret))
	require.False(t, verifyCreemSignature(payload, generateCreemSignature(payload, "other-secret"), secret))
	require.False(t, verifyCreemSignature(payload, "", secret))
}
