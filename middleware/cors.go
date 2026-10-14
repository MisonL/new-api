package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	// The fetch spec forbids pairing a wildcard origin with
	// Access-Control-Allow-Credentials: true, and gin-contrib/cors hardcodes
	// the wildcard whenever AllowAllOrigins is set — emitting both produced a
	// preflight response browsers reject. The gateway's API is consumed with
	// bearer keys, not cookies, so wildcard without credentials is what
	// clients need.
	config.AllowCredentials = false
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func PoweredBy() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
