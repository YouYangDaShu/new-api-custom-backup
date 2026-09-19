package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLogScopeRejectsModelTestForCommonStatEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log/stat?scope=model_test", nil)
	c.Set("role", common.RoleAdminUser)

	_, ok := parseLogScope(c, false)

	require.False(t, ok)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestParseLogScopeRejectsModelTestForCommonUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log/self?scope=model_test", nil)
	c.Set("role", common.RoleCommonUser)

	_, ok := parseLogScope(c, false)

	require.False(t, ok)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestParseLogScopeRejectsModelTestOnSelfEndpointEvenForAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log/self?scope=model_test", nil)
	c.Set("role", common.RoleAdminUser)

	_, ok := parseLogScope(c, false)

	require.False(t, ok)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestParseLogScopeAllowsModelTestForAdminEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log?scope=model_test", nil)
	c.Set("role", common.RoleAdminUser)

	scope, ok := parseLogScope(c, true)

	require.True(t, ok)
	assert.Equal(t, model.LogScopeModelTest, scope)
}

func TestParseLogScopeRejectsUnknownScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/log?scope=unexpected", nil)
	c.Set("role", common.RoleRootUser)

	_, ok := parseLogScope(c, true)

	require.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
