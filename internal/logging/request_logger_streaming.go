package logging

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	log "github.com/sirupsen/logrus"
)




type FileStreamingLogWriter struct {

	logsDir string


	logFilename string


	url string


	method string


	timestamp time.Time


	requestHeaders map[string][]string


	requestBodyPath string


	responseBodyPath string


	responseBodyFile *os.File


	chunkChan chan []byte


	closeChan chan struct{}


	errorChan chan error


	responseStatus int


	statusWritten bool


	responseHeaders map[string][]string


	apiRequest []byte


	apiRequestSource *FileBodySource


	apiResponse []byte


	apiResponseSource *FileBodySource


	apiWebsocketTimeline []byte


	apiResponseErrors []*interfaces.ErrorMessage


	apiResponseTimestamp time.Time
}





func (w *FileStreamingLogWriter) WriteChunkAsync(chunk []byte) {
	if w.chunkChan == nil {
		return
	}


	chunkCopy := make([]byte, len(chunk))
	copy(chunkCopy, chunk)


	select {
	case w.chunkChan <- chunkCopy:
	default:

	}
}









func (w *FileStreamingLogWriter) WriteStatus(status int, headers map[string][]string) error {
	if status == 0 {
		return nil
	}

	w.responseStatus = status
	if headers != nil {
		w.responseHeaders = make(map[string][]string, len(headers))
		for key, values := range headers {
			headerValues := make([]string, len(values))
			copy(headerValues, values)
			w.responseHeaders[key] = headerValues
		}
	}
	w.statusWritten = true
	return nil
}








func (w *FileStreamingLogWriter) WriteAPIRequest(apiRequest []byte) error {
	if len(apiRequest) == 0 {
		return nil
	}
	w.apiRequest = bytes.Clone(apiRequest)
	return nil
}


func (w *FileStreamingLogWriter) WriteAPIRequestSource(apiRequestSource *FileBodySource) error {
	if apiRequestSource == nil || !apiRequestSource.HasPayload() {
		return nil
	}
	w.apiRequestSource = apiRequestSource
	return nil
}








func (w *FileStreamingLogWriter) WriteAPIResponse(apiResponse []byte) error {
	if len(apiResponse) == 0 {
		return nil
	}
	w.apiResponse = bytes.Clone(apiResponse)
	return nil
}


func (w *FileStreamingLogWriter) WriteAPIResponseSource(apiResponseSource *FileBodySource) error {
	if apiResponseSource == nil || !apiResponseSource.HasPayload() {
		return nil
	}
	w.apiResponseSource = apiResponseSource
	return nil
}








func (w *FileStreamingLogWriter) WriteAPIWebsocketTimeline(apiWebsocketTimeline []byte) error {
	if len(apiWebsocketTimeline) == 0 {
		return nil
	}
	w.apiWebsocketTimeline = bytes.Clone(apiWebsocketTimeline)
	return nil
}


func (w *FileStreamingLogWriter) WriteAPIResponseErrors(errors []*interfaces.ErrorMessage) error {
	if len(errors) == 0 {
		return nil
	}
	w.apiResponseErrors = append([]*interfaces.ErrorMessage(nil), errors...)
	return nil
}

func (w *FileStreamingLogWriter) SetFirstChunkTimestamp(timestamp time.Time) {
	if !timestamp.IsZero() {
		w.apiResponseTimestamp = timestamp
	}
}







func (w *FileStreamingLogWriter) Close() error {
	if w.chunkChan != nil {
		close(w.chunkChan)
	}


	if w.closeChan != nil {
		<-w.closeChan
		w.chunkChan = nil
	}

	select {
	case errWrite := <-w.errorChan:
		w.cleanupTempFiles()
		return errWrite
	default:
	}

	if w.logFilename == "" {
		w.cleanupTempFiles()
		return nil
	}
	if w.responseStatus >= http.StatusBadRequest && w.responseStatus != 499 && !strings.HasPrefix(w.logFilename, "error-") {
		w.logFilename = "error-" + w.logFilename
	}

	logFile, _, errOpen := createUniqueLogFile(w.logsDir, w.logFilename)
	if errOpen != nil {
		w.cleanupTempFiles()
		return fmt.Errorf("failed to create log file: %w", errOpen)
	}

	writeErr := w.writeFinalLog(logFile)
	if errClose := logFile.Close(); errClose != nil {
		log.WithError(errClose).Warn("failed to close request log file")
		if writeErr == nil {
			writeErr = errClose
		}
	}

	w.cleanupTempFiles()
	return writeErr
}


func (w *FileStreamingLogWriter) Discard() error {
	if w == nil {
		return nil
	}
	w.logFilename = ""
	return w.Close()
}



func (w *FileStreamingLogWriter) asyncWriter() {
	defer close(w.closeChan)

	for chunk := range w.chunkChan {
		if w.responseBodyFile == nil {
			continue
		}
		if _, errWrite := w.responseBodyFile.Write(chunk); errWrite != nil {
			select {
			case w.errorChan <- errWrite:
			default:
			}
			if errClose := w.responseBodyFile.Close(); errClose != nil {
				select {
				case w.errorChan <- errClose:
				default:
				}
			}
			w.responseBodyFile = nil
		}
	}

	if w.responseBodyFile == nil {
		return
	}
	if errClose := w.responseBodyFile.Close(); errClose != nil {
		select {
		case w.errorChan <- errClose:
		default:
		}
	}
	w.responseBodyFile = nil
}

func (w *FileStreamingLogWriter) writeFinalLog(logFile *os.File) error {
	if errWrite := writeRequestInfoWithBody(logFile, w.url, w.method, w.requestHeaders, nil, w.requestBodyPath, w.timestamp, "http", inferUpstreamTransport(w.apiRequest, w.apiRequestSource, w.apiResponse, w.apiResponseSource, w.apiWebsocketTimeline, nil, nil), true); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPISection(logFile, "=== API WEBSOCKET TIMELINE ===\n", "=== API WEBSOCKET TIMELINE", w.apiWebsocketTimeline, time.Time{}); errWrite != nil {
		return errWrite
	}
	if errWrite := writePreformattedAPISectionWithSource(logFile, "=== API REQUEST ===\n", "=== API REQUEST", w.apiRequest, w.apiRequestSource, time.Time{}); errWrite != nil {
		return errWrite
	}
	if errWrite := writeAPIErrorResponses(logFile, w.apiResponseErrors); errWrite != nil {
		return errWrite
	}
	if errWrite := writePreformattedAPISectionWithSource(logFile, "=== API RESPONSE ===\n", "=== API RESPONSE", w.apiResponse, w.apiResponseSource, w.apiResponseTimestamp); errWrite != nil {
		return errWrite
	}

	responseBodyFile, errOpen := os.Open(w.responseBodyPath)
	if errOpen != nil {
		return errOpen
	}
	defer func() {
		if errClose := responseBodyFile.Close(); errClose != nil {
			log.WithError(errClose).Warn("failed to close response body temp file")
		}
	}()

	return writeResponseSection(logFile, w.responseStatus, w.statusWritten, w.responseHeaders, responseBodyFile, nil, false)
}

func (w *FileStreamingLogWriter) cleanupTempFiles() {
	if w.requestBodyPath != "" {
		if errRemove := os.Remove(w.requestBodyPath); errRemove != nil {
			log.WithError(errRemove).Warn("failed to remove request body temp file")
		}
		w.requestBodyPath = ""
	}

	if w.responseBodyPath != "" {
		if errRemove := os.Remove(w.responseBodyPath); errRemove != nil {
			log.WithError(errRemove).Warn("failed to remove response body temp file")
		}
		w.responseBodyPath = ""
	}
}



type NoOpStreamingLogWriter struct{}





func (w *NoOpStreamingLogWriter) WriteChunkAsync(_ []byte) {}









func (w *NoOpStreamingLogWriter) WriteStatus(_ int, _ map[string][]string) error {
	return nil
}








func (w *NoOpStreamingLogWriter) WriteAPIRequest(_ []byte) error {
	return nil
}








func (w *NoOpStreamingLogWriter) WriteAPIResponse(_ []byte) error {
	return nil
}








func (w *NoOpStreamingLogWriter) WriteAPIWebsocketTimeline(_ []byte) error {
	return nil
}

func (w *NoOpStreamingLogWriter) WriteAPIResponseErrors(_ []*interfaces.ErrorMessage) error {
	return nil
}

func (w *NoOpStreamingLogWriter) SetFirstChunkTimestamp(_ time.Time) {}





func (w *NoOpStreamingLogWriter) Close() error { return nil }
