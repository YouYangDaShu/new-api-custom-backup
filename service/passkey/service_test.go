package passkey

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When neither Passkey Origins nor ServerAddress are configured, the request
// Host must only be trusted for loopback (local development); any other Host
// is client-controlled and must yield a configuration error instead of
// becoming the WebAuthn RP origin.
func TestResolveOriginsDoesNotTrustArbitraryRequestHost(t *testing.T) {
	oldServerAddress := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = oldServerAddress })
	system_setting.ServerAddress = ""
	settings := &system_setting.PasskeySettings{AllowInsecureOrigin: true}

	// Loopback development hosts keep working.
	for _, host := range []string{"localhost:3000", "127.0.0.1:3000", "localhost"} {
		req := httptest.NewRequest("GET", "http://"+host+"/api/passkey", nil)
		req.Host = host
		origins, err := resolveOrigins(req, settings)
		require.NoError(t, err, host)
		require.Equal(t, []string{"http://" + host}, origins)
	}

	// An attacker-supplied Host header is rejected when nothing is configured.
	req := httptest.NewRequest("GET", "https://attacker.example/api/passkey", nil)
	req.Host = "attacker.example"
	_, err := resolveOrigins(req, settings)
	assert.Error(t, err)

	// With ServerAddress configured, the configured origin wins over the
	// request Host.
	system_setting.ServerAddress = "https://api.example.com"
	origins, err := resolveOrigins(req, settings)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://api.example.com"}, origins)

	rpID, err := resolveRPID(req, settings, origins)
	require.NoError(t, err)
	assert.Equal(t, "api.example.com", rpID)
}
