package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)

var codexFileLocks sync.Map

func lockForFilePath(authFilePath string) *sync.Mutex {
	cleaned := canonicalLockPath(authFilePath)
	if actual, ok := codexFileLocks.Load(cleaned); ok {
		return actual.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := codexFileLocks.LoadOrStore(cleaned, mu)
	return actual.(*sync.Mutex)
}

func canonicalLockPath(authFilePath string) string {
	cleaned := filepath.Clean(strings.TrimSpace(authFilePath))
	if abs, errAbs := filepath.Abs(cleaned); errAbs == nil && strings.TrimSpace(abs) != "" {
		cleaned = filepath.Clean(abs)
	}
	if real, errEval := filepath.EvalSymlinks(cleaned); errEval == nil && strings.TrimSpace(real) != "" {
		cleaned = filepath.Clean(real)
	} else if parent, errParent := filepath.EvalSymlinks(filepath.Dir(cleaned)); errParent == nil && strings.TrimSpace(parent) != "" {
		cleaned = filepath.Clean(filepath.Join(parent, filepath.Base(cleaned)))
	}
	return cleaned
}

func WithFileLock(authFilePath string, fn func() error) error {
	mu := lockForFilePath(authFilePath)
	mu.Lock()
	defer mu.Unlock()

	if errDir := os.MkdirAll(filepath.Dir(authFilePath), 0o700); errDir != nil {
		return fmt.Errorf("failed to create credential directory: %w", errDir)
	}
	releaseLockFile, errLock := acquireInterProcessLockFile(authFilePath)
	if errLock != nil {
		return errLock
	}
	defer releaseLockFile()

	return fn()
}

func WriteJSONFileAtomic(authFilePath string, data map[string]any) error {
	dir := filepath.Dir(authFilePath)
	tmpFile, errTmp := os.CreateTemp(dir, filepath.Base(authFilePath)+".tmp.*")
	if errTmp != nil {
		return fmt.Errorf("failed to create temp credential file: %w", errTmp)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if errChmod := tmpFile.Chmod(0o600); errChmod != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to chmod temp credential file: %w", errChmod)
	}
	raw, errMarshal := json.Marshal(data)
	if errMarshal != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to marshal token data: %w", errMarshal)
	}
	if _, errWrite := tmpFile.Write(raw); errWrite != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write token to temp file: %w", errWrite)
	}
	if errClose := tmpFile.Close(); errClose != nil {
		log.Errorf("codex token storage: close temp token file error: %v", errClose)
		return fmt.Errorf("failed to close temp credential file: %w", errClose)
	}
	if errRename := os.Rename(tmpPath, authFilePath); errRename != nil {
		return fmt.Errorf("failed to replace credential file: %w", errRename)
	}
	cleanupTmp = false
	return nil
}

var codexOuterCredentialKeys = []string{
	"id_token",
	"access_token",
	"refresh_token",
	"account_id",
	"last_refresh",
	"email",
	"expired",
	"plan_type",
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch typed := v.(type) {
		case map[string]any:
			dst[k] = cloneStringAnyMap(typed)
		case []string:
			s := make([]string, len(typed))
			copy(s, typed)
			dst[k] = s
		case []any:
			s := make([]any, len(typed))
			copy(s, typed)
			dst[k] = s
		default:
			dst[k] = v
		}
	}
	return dst
}

func MergeWithDiskMap(diskMap, incomingMap map[string]any) map[string]any {
	merged := cloneStringAnyMap(diskMap)
	if merged == nil {
		merged = make(map[string]any)
	}
	for key, value := range incomingMap {
		if value == nil {
			continue
		}
		if key == "siwc" || key == "use_siwc" {
			if _, exists := diskMap[key]; exists {
				continue
			}
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
			if _, exists := diskMap[key]; exists {
				continue
			}
		}
		merged[key] = value
	}
	for _, key := range codexOuterCredentialKeys {
		if value, exists := diskMap[key]; exists {
			merged[key] = value
		} else if len(diskMap) > 0 {
			delete(merged, key)
		}
	}
	if _, exists := diskMap["siwc"]; !exists && len(diskMap) > 0 {
		delete(merged, "siwc")
	}
	return merged
}

func hasDiskOAuthOuter(diskMap map[string]any) bool {
	if len(diskMap) == 0 {
		return false
	}
	if rt, ok := diskMap["refresh_token"].(string); ok && strings.TrimSpace(rt) != "" {
		return true
	}
	if lr, ok := diskMap["last_refresh"].(string); ok && strings.TrimSpace(lr) != "" {
		return true
	}
	return false
}

func MergeCodexCredentialsFromDisk(diskMap, incomingMetadata map[string]any) map[string]any {
	merged := cloneStringAnyMap(incomingMetadata)
	if merged == nil {
		merged = make(map[string]any)
	}
	if hasDiskOAuthOuter(diskMap) {
		for _, key := range codexOuterCredentialKeys {
			if value, exists := diskMap[key]; exists {
				merged[key] = value
			} else {
				delete(merged, key)
			}
		}
	} else {
		for _, key := range codexOuterCredentialKeys {
			if value, exists := diskMap[key]; exists {
				if inVal, hadIn := merged[key]; !hadIn || inVal == nil || (func() bool {
					s, ok := inVal.(string)
					return ok && strings.TrimSpace(s) == ""
				}()) {
					merged[key] = value
				}
			}
		}
	}
	if value, exists := diskMap["siwc"]; exists {
		if siwcMap, ok := value.(map[string]any); ok {
			merged["siwc"] = cloneStringAnyMap(siwcMap)
		} else {
			merged["siwc"] = value
		}
	}
	if value, exists := diskMap["use_siwc"]; exists {
		if _, specified := incomingMetadata["use_siwc"]; !specified {
			merged["use_siwc"] = value
		}
	}
	return merged
}

type CodexTokenStorage struct {
	IDToken string `json:"id_token"`

	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	AccountID string `json:"account_id"`

	LastRefresh string `json:"last_refresh"`

	Email string `json:"email"`

	Type string `json:"type"`

	Expire string `json:"expired"`

	PlanType string `json:"plan_type,omitempty"`

	UseSIWC *bool `json:"use_siwc,omitempty"`

	SIWC map[string]any `json:"siwc,omitempty"`

	ReplaceCredentials bool `json:"-"`

	RefreshPatch *CredentialRefreshPatch `json:"-"`

	Metadata map[string]any `json:"-"`
}

func (ts *CodexTokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

func (ts *CodexTokenStorage) applyWebsocketsDefault() {
	if ts == nil {
		return
	}
	if ts.Metadata == nil {
		ts.Metadata = make(map[string]any, 1)
	}
	if _, present := ts.Metadata["websockets"]; present {
		return
	}
	ts.Metadata["websockets"] = true
}

func (ts *CodexTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "codex"
	ts.applyWebsocketsDefault()
	_ = strconv.IntSize

	return WithFileLock(authFilePath, func() error {
		data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
		if errMerge != nil {
			return fmt.Errorf("failed to merge metadata: %w", errMerge)
		}

		existingBytes, errRead := os.ReadFile(authFilePath)
		if errRead != nil && !os.IsNotExist(errRead) {
			return fmt.Errorf("failed to read existing credentials: %w", errRead)
		}
		if errRead == nil {
			var diskMap map[string]any
			if errUnmarshal := json.Unmarshal(existingBytes, &diskMap); errUnmarshal != nil || diskMap == nil {
				return fmt.Errorf("invalid existing credential JSON")
			}
			if ts.RefreshPatch != nil {
				return WriteJSONFileAtomic(authFilePath, ts.RefreshPatch.Apply(diskMap))
			}
			merged := MergeWithDiskMap(diskMap, data)
			if ts.ReplaceCredentials {
				for _, key := range codexOuterCredentialKeys {
					if value, exists := data[key]; exists {
						merged[key] = value
					}
				}
			}
			data = merged
		}

		return WriteJSONFileAtomic(authFilePath, data)
	})
}
