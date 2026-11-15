package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DefaultInstancePrefix = "CPA-"
	instanceIDFilename    = "instance_id"
)

var (
	idMu      sync.Mutex
	cachedIDs = make(map[string]string)
)

func isValidHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func GetOrGenerateInstanceID(stateDir string) string {
	cleanDir := ""
	if stateDir != "" {
		cleanDir = filepath.Clean(stateDir)
	}

	idMu.Lock()
	defer idMu.Unlock()

	if id, ok := cachedIDs[cleanDir]; ok && isValidHex4(id) {
		return id
	}

	if cleanDir != "" {
		idPath := filepath.Join(cleanDir, instanceIDFilename)
		if data, err := os.ReadFile(idPath); err == nil {
			id := strings.TrimSpace(string(data))
			if isValidHex4(id) {
				cachedIDs[cleanDir] = strings.ToUpper(id)
				return cachedIDs[cleanDir]
			}
		}
	}

	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {

		return fmt.Sprintf("%04X", os.Getpid()&0xFFFF)
	}
	id := strings.ToUpper(hex.EncodeToString(buf))

	if cleanDir != "" {
		if errDir := os.MkdirAll(cleanDir, 0700); errDir == nil {
			idPath := filepath.Join(cleanDir, instanceIDFilename)
			if tmpFile, errTmp := os.CreateTemp(cleanDir, "instance_id_*.tmp"); errTmp == nil {
				tmpPath := tmpFile.Name()
				_ = tmpFile.Chmod(0600)
				_, _ = tmpFile.Write([]byte(id))
				_ = tmpFile.Sync()
				_ = tmpFile.Close()
				if errRename := os.Rename(tmpPath, idPath); errRename != nil {
					_ = os.Remove(tmpPath)
				}
			}
		}
	}

	cachedIDs[cleanDir] = id
	return id
}

func ResetCachedInstanceID() {
	idMu.Lock()
	defer idMu.Unlock()
	cachedIDs = make(map[string]string)
}

func FormatInstanceName(customName, instanceID string) string {
	if !isValidHex4(instanceID) {
		instanceID = "0001"
	}
	instanceID = strings.ToUpper(instanceID)
	suffix := "-" + instanceID
	base := sanitizeInstanceName(customName)
	if base == "" || strings.EqualFold(base, DefaultInstancePrefix+instanceID) {
		return DefaultInstancePrefix + instanceID
	}
	if len(base) >= len(suffix) && strings.EqualFold(base[len(base)-len(suffix):], suffix) {
		base = strings.TrimSpace(base[:len(base)-len(suffix)])
	}
	base = strings.TrimSpace(truncateRunesTo(base, 63-len(suffix)))
	if base == "" {
		return DefaultInstancePrefix + instanceID
	}
	return base + suffix
}
