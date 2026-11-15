package misc

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

var credentialSeparator = strings.Repeat("-", 67)

func LogSavingCredentials(path string) {
	if path == "" {
		return
	}

	fmt.Printf("Saving credentials to %q\n", filepath.Clean(path))
}

func LogCredentialSeparator() {
	log.Debug(credentialSeparator)
}

func MergeMetadata(source any, metadata map[string]any) (map[string]any, error) {
	var data map[string]any

	if srcMap, ok := source.(map[string]any); ok {
		data = make(map[string]any, len(srcMap)+len(metadata))
		for k, v := range srcMap {
			data[k] = v
		}
	} else if source != nil {

		temp, errMarshal := json.Marshal(source)
		if errMarshal != nil {
			return nil, fmt.Errorf("failed to marshal source: %w", errMarshal)
		}
		if errUnmarshal := json.Unmarshal(temp, &data); errUnmarshal != nil {
			return nil, fmt.Errorf("failed to unmarshal to map: %w", errUnmarshal)
		}
	}

	if metadata != nil {
		if data == nil {
			data = make(map[string]any)
		}
		for k, v := range metadata {
			data[k] = v
		}
	}

	return data, nil
}
