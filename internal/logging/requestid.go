package logging

import (
	"context"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)


type requestIDKey struct{}


const ginRequestIDKey = "__request_id__"



func GenerateRequestID() (string, error) {
	return GenerateRequestIDFromReader(nil)
}



func GenerateRequestIDFromReader(r io.Reader) (string, error) {
	var id uuid.UUID
	var errNewV7 error
	if r == nil {
		id, errNewV7 = uuid.NewV7()
	} else {
		id, errNewV7 = uuid.NewV7FromReader(r)
	}
	if errNewV7 != nil {
		return "", errNewV7
	}
	return id.String(), nil
}



func ShortRequestID(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if len(requestID) > 8 {
		return requestID[len(requestID)-8:]
	}
	return requestID
}


func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}



func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}


func SetGinRequestID(c *gin.Context, requestID string) {
	if c != nil {
		c.Set(ginRequestIDKey, requestID)
	}
}


func GetGinRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if id, exists := c.Get(ginRequestIDKey); exists {
		if s, ok := id.(string); ok {
			return s
		}
	}
	return ""
}
