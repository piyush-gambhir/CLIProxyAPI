package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/misc"
	log "github.com/sirupsen/logrus"
)

type ClaudeTokenStorage struct {
	IDToken string `json:"id_token"`

	AccessToken string `json:"access_token"`

	RefreshToken string `json:"refresh_token"`

	LastRefresh string `json:"last_refresh"`

	Email string `json:"email"`

	AccountUUID string `json:"account_uuid,omitempty"`

	OrganizationUUID string `json:"organization_uuid,omitempty"`

	OrganizationName string `json:"organization_name,omitempty"`

	DeviceIDs []string `json:"claude_device_ids,omitempty"`

	Type string `json:"type"`

	Expire string `json:"expired"`

	Metadata map[string]any `json:"-"`
}

func (ts *ClaudeTokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

func (ts *ClaudeTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "claude"

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0700); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	data, errMerge := misc.MergeMetadata(ts, ts.Metadata)
	if errMerge != nil {
		return fmt.Errorf("failed to merge metadata: %w", errMerge)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("failed to create token file: %w", err)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			log.Errorf("claude token storage: close token file error: %v", errClose)
		}
	}()

	if err = json.NewEncoder(f).Encode(data); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}
