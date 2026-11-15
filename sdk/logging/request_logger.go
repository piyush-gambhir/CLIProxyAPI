package logging

import internallogging "github.com/router-for-me/CLIProxyAPI/v8/internal/logging"

const defaultErrorLogsMaxFiles = 10

type RequestLogger = internallogging.RequestLogger

type StreamingLogWriter = internallogging.StreamingLogWriter

type FileRequestLogger = internallogging.FileRequestLogger

func NewFileRequestLogger(enabled bool, logsDir string, configDir string) *FileRequestLogger {
	return internallogging.NewFileRequestLogger(enabled, logsDir, configDir, defaultErrorLogsMaxFiles)
}

func NewFileRequestLoggerWithOptions(enabled bool, logsDir string, configDir string, errorLogsMaxFiles int) *FileRequestLogger {
	return internallogging.NewFileRequestLogger(enabled, logsDir, configDir, errorLogsMaxFiles)
}
