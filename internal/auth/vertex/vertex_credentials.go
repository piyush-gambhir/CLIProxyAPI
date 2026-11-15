package vertex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)

type VertexCredentialStorage struct {
	ServiceAccount map[string]any `json:"service_account"`

	ProjectID string `json:"project_id"`

	Email string `json:"email"`

	Location string `json:"location,omitempty"`

	Type string `json:"type"`

	Prefix string `json:"prefix,omitempty"`

	Metadata map[string]any `json:"-"`
}

func (s *VertexCredentialStorage) SetMetadata(meta map[string]any) {
	s.Metadata = meta
}

func (s *VertexCredentialStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	if s == nil {
		return fmt.Errorf("vertex credential: storage is nil")
	}
	if s.ServiceAccount == nil {
		return fmt.Errorf("vertex credential: service account content is empty")
	}

	s.Type = "vertex"

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("vertex credential: create directory failed: %w", err)
	}

	data, errMerge := misc.MergeMetadata(s, s.Metadata)
	if errMerge != nil {
		return fmt.Errorf("vertex credential: merge metadata failed: %w", errMerge)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("vertex credential: create file failed: %w", err)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			log.Errorf("vertex credential: failed to close file: %v", errClose)
		}
	}()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err = enc.Encode(data); err != nil {
		return fmt.Errorf("vertex credential: encode failed: %w", err)
	}
	return nil
}
