package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Sessions carry role/status for up to 30 days; authHelper must revalidate
// them so a ban or demotion takes effect on existing sessions immediately.
func TestAuthHelperRevalidatesSessionRoleAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	require.NoError(t, db.AutoMigrate(&model.User{}))

	accessToken := "auth-reval-access-token-0001"
	user := &model.User{
		Id:       1,
		Username: "reval_admin",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	user.SetAccessToken(accessToken)
	require.NoError(t, db.Create(user).Error)

	adminReached := false
	userReached := false
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("auth-reval-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", user.Username)
		session.Set("role", common.RoleAdminUser) // snapshot taken at login time
		session.Set("id", user.Id)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/admin", AdminAuth(), func(c *gin.Context) {
		adminReached = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	router.GET("/user", UserAuth(), func(c *gin.Context) {
		userReached = true
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// Log in once and reuse the session cookie for every later request.
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, httptest.NewRequest(http.MethodGet, "/login", nil))
	cookies := loginRec.Result().Cookies()
	require.NotEmpty(t, cookies)

	request := func(path string, withToken bool) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("New-Api-User", "1")
		if withToken {
			req.Header.Set("Authorization", accessToken)
		} else {
			for _, ck := range cookies {
				req.AddCookie(ck)
			}
		}
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	// Fresh admin session works.
	adminReached = false
	request("/admin", false)
	assert.True(t, adminReached, "enabled admin session must pass AdminAuth")

	// Demotion takes effect on the existing session immediately.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("role", common.RoleCommonUser).Error)
	adminReached = false
	request("/admin", false)
	assert.False(t, adminReached, "demoted user's old session must lose admin access")
	userReached = false
	request("/user", false)
	assert.True(t, userReached, "demoted user keeps common-user access")

	// Disabling the user kills the existing session entirely.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusDisabled).Error)
	userReached = false
	request("/user", false)
	assert.False(t, userReached, "disabled user's old session must stop working")

	// Access-token behavior is preserved: fresh DB read on every request, so
	// the disabled user is rejected there too...
	userReached = false
	request("/user", true)
	assert.False(t, userReached, "disabled user's access token must be rejected")

	// ...and re-enabling restores access for both paths without a new login.
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusEnabled).Error)
	userReached = false
	request("/user", true)
	assert.True(t, userReached, "re-enabled user's access token works")
	userReached = false
	request("/user", false)
	assert.True(t, userReached, "re-enabled user's session works")
}

func TestAuthHelperRejectsMalformedSessionFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	common.RedisEnabled = false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "session_user", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default",
	}).Error)

	cases := []struct {
		name  string
		field string
		value any
		omit  bool
	}{
		{name: "missing username", field: "username", omit: true},
		{name: "wrong username type", field: "username", value: 1},
		{name: "missing role", field: "role", omit: true},
		{name: "wrong role type", field: "role", value: "admin"},
		{name: "missing status", field: "status", omit: true},
		{name: "wrong status type", field: "status", value: "enabled"},
		{name: "missing id", field: "id", omit: true},
		{name: "wrong id type", field: "id", value: "1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("malformed-session-test"))))
			router.GET("/login", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("username", "session_user")
				session.Set("role", common.RoleCommonUser)
				session.Set("id", 1)
				session.Set("status", common.UserStatusEnabled)
				session.Set("group", "default")
				if tc.omit {
					session.Delete(tc.field)
				} else {
					session.Set(tc.field, tc.value)
				}
				require.NoError(t, session.Save())
				c.Status(http.StatusNoContent)
			})
			router.GET("/protected", UserAuth(), func(c *gin.Context) { reached = true })

			login := httptest.NewRecorder()
			router.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login", nil))
			require.Equal(t, http.StatusNoContent, login.Code)
			require.NotEmpty(t, login.Result().Cookies())
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("New-Api-User", "1")
			for _, sessionCookie := range login.Result().Cookies() {
				req.AddCookie(sessionCookie)
			}
			resp := httptest.NewRecorder()
			assert.NotPanics(t, func() { router.ServeHTTP(resp, req) })
			assert.False(t, reached)
			assert.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	}
}
