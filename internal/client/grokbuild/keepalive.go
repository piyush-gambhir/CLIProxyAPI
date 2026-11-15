package grokbuild

import (
	"bytes"
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

var keepaliveSSEComment = []byte(": keepalive\n\n")

func KeepaliveSSEComment() []byte {
	return bytes.Clone(keepaliveSSEComment)
}

func IsGrokClientUserAgent(userAgent string) bool {
	ua := strings.ToLower(userAgent)
	return strings.Contains(ua, "grok-pager") || strings.Contains(ua, "grok-shell")
}

func IsGrokClientHeaders(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for key, values := range headers {
		if strings.EqualFold(key, "User-Agent") {
			if slices.ContainsFunc(values, IsGrokClientUserAgent) {
				return true
			}
		}
	}
	return false
}

func IsGrokClientContext(ctx context.Context, headers http.Header) bool {
	if ctx != nil {
		if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
			if IsGrokClientHeaders(ginCtx.Request.Header) {
				return true
			}
		}
	}
	return IsGrokClientHeaders(headers)
}

func IsKeepalivePayload(payload []byte) bool {
	return ojson.GetBytesNoCopy(payload, "type").String() == "keepalive"
}

func IsKeepaliveSSELine(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if bytes.HasPrefix(trimmed, []byte("event:")) {
		eventName := bytes.TrimSpace(trimmed[6:])
		return bytes.Equal(eventName, []byte("keepalive"))
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) {
		data := bytes.TrimSpace(trimmed[5:])
		return IsKeepalivePayload(data)
	}
	return false
}

func TransformKeepaliveSSELine(line []byte, isGrokClient bool) ([]byte, bool) {
	if !isGrokClient {
		return line, false
	}
	if IsKeepaliveSSELine(line) {
		return bytes.Clone(keepaliveSSEComment), true
	}
	return line, false
}
