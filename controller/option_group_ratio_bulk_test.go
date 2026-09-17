package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpdateGroupRatioOptionsBulkRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/group-ratios",
		strings.NewReader(`{"options":[{"key":"GroupRatio","value":{"default":1}}],"unexpected":true}`),
	)

	UpdateGroupRatioOptionsBulk(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestUpdateGroupRatioOptionsBulkRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/group-ratios",
		strings.NewReader(`{"options":[]}`+strings.Repeat(" ", int(maxGroupRatioOptionsBodyBytes))),
	)

	UpdateGroupRatioOptionsBulk(context)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}
