package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestModelTestLogStatRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/log/model_test/stat" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestModelTestLogStatRouteRequiresAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/log/model_test/stat", nil)
	engine.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
