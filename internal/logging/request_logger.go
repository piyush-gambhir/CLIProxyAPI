


package logging

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
)

const (
	WebsocketTimelineSourceContextKey    = "WEBSOCKET_TIMELINE_SOURCE"
	APIRequestSourceContextKey           = "API_REQUEST_SOURCE"
	DeferredAPIRequestContextKey         = "DEFERRED_API_REQUEST"
	DeferredAPIResponseContextKey        = "DEFERRED_API_RESPONSE"
	APIResponseSourceContextKey          = "API_RESPONSE_SOURCE"
	APIResponseCapturedContextKey        = "API_RESPONSE_CAPTURED"
	APIWebsocketTimelineSourceContextKey = "API_WEBSOCKET_TIMELINE_SOURCE"
)


type DeferredAPIRequest func() []byte



type RequestLogger interface {




















	LogRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline, apiRequest, apiResponse, apiWebsocketTimeline []byte, apiResponseErrors []*interfaces.ErrorMessage, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error













	LogStreamingRequest(url, method string, headers map[string][]string, body []byte, requestID string) (StreamingLogWriter, error)





	IsEnabled() bool
}



type StreamingLogWriter interface {




	WriteChunkAsync(chunk []byte)









	WriteStatus(status int, headers map[string][]string) error









	WriteAPIRequest(apiRequest []byte) error









	WriteAPIResponse(apiResponse []byte) error









	WriteAPIWebsocketTimeline(apiWebsocketTimeline []byte) error


	WriteAPIResponseErrors(errors []*interfaces.ErrorMessage) error





	SetFirstChunkTimestamp(timestamp time.Time)





	Close() error
}



type FileRequestLogger struct {

	enabled bool


	logsDir string


	errorLogsMaxFiles int

	homeEnabled bool
}












func NewFileRequestLogger(enabled bool, logsDir string, configDir string, errorLogsMaxFiles int) *FileRequestLogger {

	if !filepath.IsAbs(logsDir) {

		if configDir != "" {
			logsDir = filepath.Join(configDir, logsDir)
		}
	}
	return &FileRequestLogger{
		enabled:           enabled,
		logsDir:           logsDir,
		errorLogsMaxFiles: errorLogsMaxFiles,
		homeEnabled:       false,
	}
}





func (l *FileRequestLogger) IsEnabled() bool {
	return l.enabled
}






func (l *FileRequestLogger) SetEnabled(enabled bool) {
	l.enabled = enabled
}


func (l *FileRequestLogger) SetErrorLogsMaxFiles(maxFiles int) {
	l.errorLogsMaxFiles = maxFiles
}


func (l *FileRequestLogger) NewFileBodySource(prefix string) (*FileBodySource, error) {
	if l == nil {
		return nil, fmt.Errorf("file request logger is nil")
	}
	if errEnsure := l.ensureLogsDir(); errEnsure != nil {
		return nil, errEnsure
	}
	return NewFileBodySourceInDir(l.logsDir, prefix)
}
