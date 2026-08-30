package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ACCESS_TOKEN_MAX_AGE_DAYS bounds access-token lifetime when enabled; the
// default (0) must keep legacy no-expiry behavior, and legacy tokens without a
// creation stamp must be rejected once a lifetime is enforced (their age is
// unknown and unbounded).
func TestValidateAccessTokenMaxAge(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&User{}))
	oldMaxAge := common.AccessTokenMaxAgeDays
	t.Cleanup(func() { common.AccessTokenMaxAgeDays = oldMaxAge })

	const freshToken = "test-access-token-fresh-000000"
	const staleToken = "test-access-token-stale-000000"
	const legacyToken = "test-access-token-legacy-00000"
	DB.Where("id IN ?", []int{990021, 990022, 990023}).Delete(&User{})
	mkUser := func(id int, token string, createdAt int64) {
		u := &User{Id: id, Username: "at_user_" + token[18:24], Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "aff_" + token[18:24]}
		u.SetAccessToken(token)
		u.AccessTokenCreatedAt = createdAt
		require.NoError(t, DB.Create(u).Error)
	}
	now := time.Now().Unix()
	mkUser(990021, freshToken, now)
	mkUser(990022, staleToken, now-40*24*60*60)
	mkUser(990023, legacyToken, 0)

	// Default: no expiry, everything (including legacy) keeps working.
	common.AccessTokenMaxAgeDays = 0
	for _, token := range []string{freshToken, staleToken, legacyToken} {
		user, err := ValidateAccessToken(token)
		require.NoError(t, err)
		assert.NotNil(t, user, token)
	}

	// With a 30-day lifetime: fresh passes, stale and legacy are rejected.
	common.AccessTokenMaxAgeDays = 30
	user, err := ValidateAccessToken(freshToken)
	require.NoError(t, err)
	assert.NotNil(t, user)
	for _, token := range []string{staleToken, legacyToken} {
		user, err := ValidateAccessToken(token)
		require.NoError(t, err)
		assert.Nil(t, user, token)
	}
}
