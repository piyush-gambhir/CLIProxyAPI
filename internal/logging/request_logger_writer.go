package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/interfaces"
	log "github.com/sirupsen/logrus"
)

var requestLogID atomic.Uint64

func (l *FileRequestLogger) LogRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline, apiRequest, apiResponse, apiWebsocketTimeline []byte, apiResponseErrors []*interfaces.ErrorMessage, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequest(url, method, requestHeaders, body, statusCode, responseHeaders, response, websocketTimeline, apiRequest, apiResponse, apiWebsocketTimeline, apiResponseErrors, false, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) LogRequestWithOptions(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline, apiRequest, apiResponse, apiWebsocketTimeline []byte, apiResponseErrors []*interfaces.ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequestWithSources(url, method, requestHeaders, body, statusCode, responseHeaders, response, websocketTimeline, nil, apiRequest, nil, apiResponse, nil, apiWebsocketTimeline, nil, apiResponseErrors, force, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) logRequest(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline, apiRequest, apiResponse, apiWebsocketTimeline []byte, apiResponseErrors []*interfaces.ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequestWithSources(url, method, requestHeaders, body, statusCode, responseHeaders, response, websocketTimeline, nil, apiRequest, nil, apiResponse, nil, apiWebsocketTimeline, nil, apiResponseErrors, force, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) LogRequestWithOptionsAndSources(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline []byte, websocketTimelineSource *FileBodySource, apiRequest, apiResponse, apiWebsocketTimeline []byte, apiWebsocketTimelineSource *FileBodySource, apiResponseErrors []*interfaces.ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequestWithSources(url, method, requestHeaders, body, statusCode, responseHeaders, response, websocketTimeline, websocketTimelineSource, apiRequest, nil, apiResponse, nil, apiWebsocketTimeline, apiWebsocketTimelineSource, apiResponseErrors, force, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) LogRequestWithOptionsAndAllSources(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline []byte, websocketTimelineSource *FileBodySource, apiRequest []byte, apiRequestSource *FileBodySource, apiResponse []byte, apiResponseSource *FileBodySource, apiWebsocketTimeline []byte, apiWebsocketTimelineSource *FileBodySource, apiResponseErrors []*interfaces.ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	return l.logRequestWithSources(url, method, requestHeaders, body, statusCode, responseHeaders, response, websocketTimeline, websocketTimelineSource, apiRequest, apiRequestSource, apiResponse, apiResponseSource, apiWebsocketTimeline, apiWebsocketTimelineSource, apiResponseErrors, force, requestID, requestTimestamp, apiResponseTimestamp)
}

func (l *FileRequestLogger) logRequestWithSources(url, method string, requestHeaders map[string][]string, body []byte, statusCode int, responseHeaders map[string][]string, response, websocketTimeline []byte, websocketTimelineSource *FileBodySource, apiRequest []byte, apiRequestSource *FileBodySource, apiResponse []byte, apiResponseSource *FileBodySource, apiWebsocketTimeline []byte, apiWebsocketTimelineSource *FileBodySource, apiResponseErrors []*interfaces.ErrorMessage, force bool, requestID string, requestTimestamp, apiResponseTimestamp time.Time) error {
	defer cleanupFileBodySources(websocketTimelineSource, apiRequestSource, apiResponseSource, apiWebsocketTimelineSource)

	if !l.enabled && !force {
		return nil
	}

	if l.homeEnabled && l.enabled {
		responseToWrite, decompressErr := l.decompressResponse(responseHeaders, response)
		if decompressErr != nil {
			responseToWrite = response
		}

		var buf bytes.Buffer
		writeErr := l.writeNonStreamingLog(
			&buf,
			url,
			method,
			requestHeaders,
			body,
			"",
			websocketTimeline,
			websocketTimelineSource,
			apiRequest,
			apiRequestSource,
			apiResponse,
			apiResponseSource,
			apiWebsocketTimeline,
			apiWebsocketTimelineSource,
			apiResponseErrors,
			statusCode,
			responseHeaders,
			responseToWrite,
			decompressErr,
			requestTimestamp,
			apiResponseTimestamp,
		)
		if writeErr != nil {
			return fmt.Errorf("failed to build request log content: %w", writeErr)
		}
		return l.forwardRequestLogToHome(context.Background(), requestHeaders, requestID, buf.String())
	}

	if errEnsure := l.ensureLogsDir(); errEnsure != nil {
		return fmt.Errorf("failed to create logs directory: %w", errEnsure)
	}

	filename := l.generateFilename(url, requestID)
	if (force && !l.enabled) || (statusCode >= 400 && statusCode != 499) {
		filename = l.generateErrorFilename(url, requestID)
	}

	requestBodyPath, errTemp := l.writeRequestBodyTempFile(body)
	if errTemp != nil {
		log.WithError(errTemp).Warn("failed to create request body temp file, falling back to direct write")
	}
	if requestBodyPath != "" {
		defer func() {
			if errRemove := os.Remove(requestBodyPath); errRemove != nil {
				log.WithError(errRemove).Warn("failed to remove request body temp file")
			}
		}()
	}

	responseToWrite, decompressErr := l.decompressResponse(responseHeaders, response)
	if decompressErr != nil {

		responseToWrite = response
	}

	logFile, _, errOpen := createUniqueLogFile(l.logsDir, filename)
	if errOpen != nil {
		return fmt.Errorf("failed to create log file: %w", errOpen)
	}

	writeErr := l.writeNonStreamingLog(
		logFile,
		url,
		method,
		requestHeaders,
		body,
		requestBodyPath,
		websocketTimeline,
		websocketTimelineSource,
		apiRequest,
		apiRequestSource,
		apiResponse,
		apiResponseSource,
		apiWebsocketTimeline,
		apiWebsocketTimelineSource,
		apiResponseErrors,
		statusCode,
		responseHeaders,
		responseToWrite,
		decompressErr,
		requestTimestamp,
		apiResponseTimestamp,
	)
	if errClose := logFile.Close(); errClose != nil {
		log.WithError(errClose).Warn("failed to close request log file")
		if writeErr == nil {
			return errClose
		}
	}
	if writeErr != nil {
		return fmt.Errorf("failed to write log file: %w", writeErr)
	}

	if force && !l.enabled {
		if errCleanup := l.cleanupOldErrorLogs(); errCleanup != nil {
			log.WithError(errCleanup).Warn("failed to clean up old error logs")
		}
	}

	return nil
}

func (l *FileRequestLogger) LogStreamingRequest(url, method string, headers map[string][]string, body []byte, requestID string) (StreamingLogWriter, error) {
	if !l.enabled {
		return &NoOpStreamingLogWriter{}, nil
	}

	if l.homeEnabled {
		client := currentHomeRequestLogClient()
		if client == nil || !client.HeartbeatOK() {
			return &NoOpStreamingLogWriter{}, nil
		}
		return newHomeStreamingLogWriter(url, method, headers, body, requestID), nil
	}

	if err := l.ensureLogsDir(); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	filename := l.generateFilename(url, requestID)

	requestHeaders := make(map[string][]string, len(headers))
	for key, values := range headers {
		headerValues := make([]string, len(values))
		copy(headerValues, values)
		requestHeaders[key] = headerValues
	}

	requestBodyPath, errTemp := l.writeRequestBodyTempFile(body)
	if errTemp != nil {
		return nil, fmt.Errorf("failed to create request body temp file: %w", errTemp)
	}

	responseBodyFile, errCreate := os.CreateTemp(l.logsDir, "response-body-*.tmp")
	if errCreate != nil {
		_ = os.Remove(requestBodyPath)
		return nil, fmt.Errorf("failed to create response body temp file: %w", errCreate)
	}
	responseBodyPath := responseBodyFile.Name()

	writer := &FileStreamingLogWriter{
		logsDir:          l.logsDir,
		logFilename:      filename,
		url:              url,
		method:           method,
		timestamp:        time.Now(),
		requestHeaders:   requestHeaders,
		requestBodyPath:  requestBodyPath,
		responseBodyPath: responseBodyPath,
		responseBodyFile: responseBodyFile,
		chunkChan:        make(chan []byte, 100),
		closeChan:        make(chan struct{}),
		errorChan:        make(chan error, 1),
	}

	go writer.asyncWriter()

	return writer, nil
}

func (l *FileRequestLogger) generateErrorFilename(url string, requestID ...string) string {
	return fmt.Sprintf("error-%s", l.generateFilename(url, requestID...))
}

func (l *FileRequestLogger) ensureLogsDir() error {
	if _, err := os.Stat(l.logsDir); os.IsNotExist(err) {
		return os.MkdirAll(l.logsDir, 0755)
	}
	return nil
}

func (l *FileRequestLogger) generateFilename(url string, requestID ...string) string {

	path := url
	if strings.Contains(url, "?") {
		path = strings.Split(url, "?")[0]
	}

	if strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	sanitized := l.sanitizeForFilename(path)

	timestamp := time.Now().Format("2006-01-02T150405")

	var idPart string
	if len(requestID) > 0 && requestID[0] != "" {
		idPart = ShortRequestID(requestID[0])
	} else {
		id := requestLogID.Add(1)
		idPart = fmt.Sprintf("%d", id)
	}

	name := fmt.Sprintf("%s-%s-%s.log", sanitized, timestamp, idPart)
	return filepath.Base(name)
}

func createUniqueLogFile(dir, filename string) (*os.File, string, error) {
	if filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\`) || strings.Contains(filename, "..") || filepath.Base(filename) != filename {
		return nil, "", fmt.Errorf("invalid log filename: %q", filename)
	}
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	idx := strings.LastIndex(base, "-")
	prefix := base
	idPart := ""
	if idx > 0 {
		prefix = base[:idx]
		idPart = base[idx+1:]
	}

	target := filepath.Join(dir, filename)
	logFile, errOpen := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if errOpen == nil {
		return logFile, target, nil
	}
	if !os.IsExist(errOpen) {
		return nil, "", errOpen
	}

	for seq := 1; seq <= 1000; seq++ {
		candidateName := fmt.Sprintf("%s_%d-%s%s", prefix, seq, idPart, ext)
		candidatePath := filepath.Join(dir, candidateName)
		logCandidate, errCandidate := os.OpenFile(candidatePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if errCandidate == nil {
			return logCandidate, candidatePath, nil
		}
		if !os.IsExist(errCandidate) {
			return nil, "", errCandidate
		}
	}
	return nil, "", fmt.Errorf("too many conflicting log files for %s", filename)
}

func (l *FileRequestLogger) sanitizeForFilename(path string) string {

	sanitized := strings.ReplaceAll(path, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")

	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	reg := regexp.MustCompile(`[<>:"|?*\s]`)
	sanitized = reg.ReplaceAllString(sanitized, "-")

	for strings.Contains(sanitized, "..") {
		sanitized = strings.ReplaceAll(sanitized, "..", "-")
	}

	reg = regexp.MustCompile(`-+`)
	sanitized = reg.ReplaceAllString(sanitized, "-")

	sanitized = strings.Trim(sanitized, "-.")

	if sanitized == "" {
		sanitized = "root"
	}

	return sanitized
}

func (l *FileRequestLogger) cleanupOldErrorLogs() error {
	if l.errorLogsMaxFiles <= 0 {
		return nil
	}

	entries, errRead := os.ReadDir(l.logsDir)
	if errRead != nil {
		return errRead
	}

	type logFile struct {
		name    string
		modTime time.Time
	}

	var files []logFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "error-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			log.WithError(errInfo).Warn("failed to read error log info")
			continue
		}
		files = append(files, logFile{name: name, modTime: info.ModTime()})
	}

	if len(files) <= l.errorLogsMaxFiles {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, file := range files[l.errorLogsMaxFiles:] {
		if errRemove := os.Remove(filepath.Join(l.logsDir, file.name)); errRemove != nil {
			log.WithError(errRemove).Warnf("failed to remove old error log: %s", file.name)
		}
	}

	return nil
}

func (l *FileRequestLogger) writeRequestBodyTempFile(body []byte) (string, error) {
	tmpFile, errCreate := os.CreateTemp(l.logsDir, "request-body-*.tmp")
	if errCreate != nil {
		return "", errCreate
	}
	tmpPath := tmpFile.Name()

	if _, errCopy := io.Copy(tmpFile, bytes.NewReader(body)); errCopy != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", errCopy
	}
	if errClose := tmpFile.Close(); errClose != nil {
		_ = os.Remove(tmpPath)
		return "", errClose
	}
	return tmpPath, nil
}
