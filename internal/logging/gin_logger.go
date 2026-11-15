


package logging

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)


var aiAPIPrefixes = []string{
	"/v1",
	"/v1beta",
	"/openai/v1",
	"/backend-api/codex",
}

const (
	skipGinLogKey  = "__gin_skip_request_logging__"
	creditsUsedKey = "__antigravity_credits_used__"
)










func GinLogrusLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := util.MaskSensitiveQuery(c.Request.URL.RawQuery)


		if strings.HasPrefix(path, "/v8/management/oauth/") {
			raw = ""
		}


		var requestID string
		if isAIAPIPath(path) {
			generatedID, errGenerate := GenerateRequestID()
			if errGenerate != nil {
				log.WithError(errGenerate).Error("failed to generate request ID")
			} else {
				requestID = generatedID
				SetGinRequestID(c, requestID)
				ctx := WithRequestID(c.Request.Context(), requestID)
				c.Request = c.Request.WithContext(ctx)
			}
		}

		c.Next()


		if path == "/healthz" && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) &&
			c.Writer.Status() >= http.StatusOK && c.Writer.Status() < http.StatusMultipleChoices {
			return
		}

		if shouldSkipGinRequestLogging(c) {
			return
		}

		if raw != "" {
			path = path + "?" + raw
		}

		latency := time.Since(start)
		if latency > time.Minute {
			latency = latency.Truncate(time.Second)
		} else {
			latency = latency.Truncate(time.Millisecond)
		}

		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()

		if requestID == "" {
			requestID = "--------"
		}
		logLine := fmt.Sprintf("%3d | %13v | %15s | %-7s %q", statusCode, latency, clientIP, method, path)
		if creditsUsed(c) {
			logLine += " [credits]"
		}
		if errorMessage != "" {
			logLine = logLine + " | " + errorMessage
		}

		entry := log.WithField("request_id", requestID)

		switch {
		case statusCode >= http.StatusInternalServerError:
			entry.Error(logLine)
		case statusCode >= http.StatusBadRequest:
			entry.Warn(logLine)
		default:
			entry.Info(logLine)
		}
	}
}


func isAIAPIPath(path string) bool {
	for _, prefix := range aiAPIPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}







func GinLogrusRecovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {

			panic(http.ErrAbortHandler)
		}

		log.WithFields(log.Fields{
			"panic": recovered,
			"stack": string(debug.Stack()),
			"path":  c.Request.URL.Path,
		}).Error("recovered from panic")

		c.AbortWithStatus(http.StatusInternalServerError)
	})
}



func SkipGinRequestLogging(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(skipGinLogKey, true)
}

func shouldSkipGinRequestLogging(c *gin.Context) bool {
	if c == nil {
		return false
	}
	val, exists := c.Get(skipGinLogKey)
	if !exists {
		return false
	}
	flag, ok := val.(bool)
	return ok && flag
}

func creditsUsed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	val, exists := c.Get(creditsUsedKey)
	if !exists {
		return false
	}
	flag, ok := val.(bool)
	return ok && flag
}
