package middleware

import (
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS never combines wildcard origins with credentials: reflecting every
// Origin while allowing credentials would let any website ride a visitor's
// cookies/headers into the API. Deployments that need credentialed
// cross-origin dashboard access must list their origins explicitly in
// CORS_ALLOW_ORIGINS (comma-separated); the default stays maximally
// compatible for API clients (any origin, no credentials — same-origin
// requests are unaffected because CORS does not apply to them).
func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	if allowed := strings.TrimSpace(os.Getenv("CORS_ALLOW_ORIGINS")); allowed != "" {
		origins := make([]string, 0)
		for _, origin := range strings.Split(allowed, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				origins = append(origins, origin)
			}
		}
		config.AllowOrigins = origins
		config.AllowCredentials = true
	} else {
		config.AllowAllOrigins = true
		config.AllowCredentials = false
	}
	return cors.New(config)
}

func Version() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
