package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestResolveTokenRoutingGroupUsesManualTokenGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "@manual:ST,gemini")

	assert.Equal(t, "@manual:ST,gemini", resolveTokenRoutingGroup(ctx, "vip"))
}

func TestResolveTokenRoutingGroupKeepsSingleUsingGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyTokenGroup, "ST")

	assert.Equal(t, "ST", resolveTokenRoutingGroup(ctx, "ST"))
}
