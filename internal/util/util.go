


package util

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	log "github.com/sirupsen/logrus"
)

var functionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)





func SanitizeFunctionName(name string) string {
	if name == "" {
		return ""
	}


	sanitized := functionNameSanitizer.ReplaceAllString(name, "_")



	if len(sanitized) > 0 {
		first := sanitized[0]
		if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {




			if len(sanitized) >= 64 {
				sanitized = sanitized[:63]
			}
			sanitized = "_" + sanitized
		}
	} else {
		sanitized = "_"
	}


	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return sanitized
}



func SetLogLevel(cfg *config.Config) {
	currentLevel := log.GetLevel()
	var newLevel log.Level
	if cfg.Debug {
		newLevel = log.DebugLevel
	} else {
		newLevel = log.InfoLevel
	}

	if currentLevel != newLevel {
		log.SetLevel(newLevel)
		log.Infof("log level changed from %s to %s (debug=%t)", currentLevel, newLevel, cfg.Debug)
	}
}




func ResolveAuthDir(authDir string) (string, error) {
	if authDir == "" {
		authDir = config.DefaultAuthDir
	}
	if strings.HasPrefix(authDir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve auth dir: %w", err)
		}
		remainder := strings.TrimPrefix(authDir, "~")
		remainder = strings.TrimLeft(remainder, "/\\")
		if remainder == "" {
			return filepath.Clean(home), nil
		}
		normalized := strings.ReplaceAll(remainder, "\\", "/")
		return filepath.Clean(filepath.Join(home, filepath.FromSlash(normalized))), nil
	}
	return filepath.Clean(authDir), nil
}



func CountAuthFiles[T any](ctx context.Context, store interface {
	List(context.Context) ([]T, error)
}) int {
	if store == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	entries, err := store.List(ctx)
	if err != nil {
		log.Debugf("countAuthFiles: failed to list auth records: %v", err)
		return 0
	}
	return len(entries)
}



func WritablePath() string {
	for _, key := range []string{"WRITABLE_PATH", "writable_path"} {
		if value, ok := os.LookupEnv(key); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return filepath.Clean(trimmed)
			}
		}
	}
	return ""
}
