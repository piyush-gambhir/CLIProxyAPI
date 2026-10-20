package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// The optional server-side key is used only by same-origin browser requests on
// loopback. Never return this credential in an asset, response, or browser store.
func (s *Server) consoleEnvironmentAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.console == nil || s.console.managementKey == "" || c.GetHeader("X-Cliproxy-Console") != "1" || c.GetHeader("Authorization") != "" || c.GetHeader("X-Management-Key") != "" {
			c.Next()
			return
		}
		scheme := "http"
		// The protocol multiplexer exposes an empty TLS state for plain HTTP.
		if c.Request.TLS != nil && c.Request.TLS.HandshakeComplete {
			scheme = "https"
		}
		origin := c.GetHeader("Origin")
		reason := ""
		if !consoleLocal(c) {
			reason = "local console requires loopback"
		} else if c.GetHeader("Sec-Fetch-Site") != "same-origin" {
			reason = "local console requires same-origin fetch metadata"
		} else if origin != "" && origin != scheme+"://"+c.Request.Host {
			reason = "local console origin does not match host"
		}
		if reason != "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": reason})
			return
		}
		c.Request.Header.Set("Authorization", "Bearer "+s.console.managementKey)
		c.Next()
	}
}
