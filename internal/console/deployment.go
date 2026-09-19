package console

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Deployment struct {
	DataDir           string `json:"dataDir"`
	WebDir            string `json:"webDir"`
	CompatibilityPort int    `json:"compatibilityPort"`
	MigrateFrom       string `json:"migrateFrom,omitempty"`
}

func ReadDeployment(configFile string) (Deployment, error) {
	d := Deployment{DataDir: configFile + ".console"}
	b, err := os.ReadFile(configFile + ".console.json")
	if err == nil {
		err = json.Unmarshal(b, &d)
	}
	if err != nil && !os.IsNotExist(err) {
		return d, err
	}
	if value := os.Getenv("CLIPROXY_CONSOLE_DATA_DIR"); value != "" {
		d.DataDir = value
	}
	if value := os.Getenv("CLIPROXY_CONSOLE_WEB_DIR"); value != "" {
		d.WebDir = value
	}
	return d, nil
}
func LoadDeployment(configFile string) (Deployment, error) {
	d, err := ReadDeployment(configFile)
	if err != nil {
		return d, err
	}
	if d.MigrateFrom != "" && d.MigrateFrom != d.DataDir {
		if _, err = os.Stat(filepath.Join(d.DataDir, "console.sqlite")); err == nil {
			return d, fmt.Errorf("console relocation destination already exists; original data remains at %s", d.MigrateFrom)
		} else if !os.IsNotExist(err) {
			return d, err
		}
		if err = os.MkdirAll(d.DataDir, 0700); err != nil {
			return d, err
		}
		db, err := sql.Open("sqlite", filepath.Join(d.MigrateFrom, "console.sqlite"))
		if err != nil {
			return d, err
		}
		_, err = db.Exec("VACUUM INTO ?", filepath.Join(d.DataDir, "console.sqlite"))
		closeErr := db.Close()
		if err != nil {
			return d, err
		}
		if closeErr != nil {
			return d, closeErr
		}
		if err = os.Chmod(filepath.Join(d.DataDir, "console.sqlite"), 0600); err != nil {
			return d, err
		}
		for _, name := range []string{"service-settings.json", "management.key"} {
			b, e := os.ReadFile(filepath.Join(d.MigrateFrom, name))
			if e == nil {
				if e = os.WriteFile(filepath.Join(d.DataDir, name), b, 0600); e != nil {
					return d, e
				}
			} else if !os.IsNotExist(e) {
				return d, e
			}
		}
		d.MigrateFrom = ""
		if err = WriteJSON(configFile+".console.json", d); err != nil {
			return d, err
		}
	}
	return d, nil
}
func SaveDeployment(configFile string, active Deployment, patch Object) error {
	next := active
	for k, v := range patch {
		switch k {
		case "dataDir", "webDir":
			str, ok := v.(string)
			if !ok || !filepath.IsAbs(str) || strings.ContainsAny(str, "\x00\r\n") {
				return Fail(400, "Use an absolute directory path")
			}
			if k == "dataDir" {
				if os.Getenv("CLIPROXY_CONSOLE_DATA_DIR") != "" {
					return Fail(400, "Data directory is controlled by the proxy environment")
				}
				next.DataDir = filepath.Clean(str)
			} else {
				if os.Getenv("CLIPROXY_CONSOLE_WEB_DIR") != "" {
					return Fail(400, "Frontend directory is controlled by the proxy environment")
				}
				if _, err := os.Stat(filepath.Join(str, "index.html")); err != nil {
					return Fail(400, "Frontend directory must contain index.html")
				}
				next.WebDir = filepath.Clean(str)
			}
		case "compatibilityPort":
			n, ok := v.(float64)
			if !ok || n < 0 || n > 65535 || n != float64(int(n)) {
				return Fail(400, "Compatibility port must be 0 or a valid port")
			}
			next.CompatibilityPort = int(n)
		default:
			return Fail(400, "Unknown deployment setting")
		}
	}
	if next.DataDir != active.DataDir {
		if _, err := os.Stat(next.DataDir); err == nil {
			return Fail(409, "Choose a new, unused data directory")
		} else if !os.IsNotExist(err) {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		resolved, err := canonical(next.DataDir)
		if err != nil {
			return err
		}
		if !inside(home, resolved) {
			return Fail(400, "New data directory must be inside your home")
		}
		next.MigrateFrom = active.DataDir
	} else {
		next.MigrateFrom = ""
	}
	_, err := Backup(configFile + ".console.json")
	if err != nil {
		return err
	}
	return WriteJSON(configFile+".console.json", next)
}
func (s *Store) ServiceSettings(patch Object, update bool) (Object, error) {
	file := filepath.Join(s.Directory, "service-settings.json")
	values, _, err := ReadJSON(file)
	if err != nil {
		return nil, err
	}
	defaults := Object{"label": "local.cliproxyapi", "brewLabel": "homebrew.mxcl.cliproxyapi", "brewBinary": "/opt/homebrew/bin/brew", "brewPrefix": "/opt/homebrew", "config": s.ConfigFile, "repository": "https://github.com/router-for-me/CLIProxyAPI", "consoleUrl": s.Origin}
	envs := map[string]string{"label": "CLIPROXY_SERVICE_LABEL", "brewLabel": "CLIPROXY_BREW_LABEL", "brewBinary": "CLIPROXY_BREW", "brewPrefix": "CLIPROXY_BREW_PREFIX", "config": "CLIPROXY_CONFIG", "repository": "CLIPROXY_RELEASE_REPOSITORY", "consoleUrl": "CLIPROXY_CONSOLE_URL"}
	if update {
		for k, v := range patch {
			str, ok := v.(string)
			if !ok || str == "" || strings.ContainsAny(str, "\r\n\x00") || defaults[k] == nil {
				return nil, Fail(400, "Invalid service setting")
			}
			if os.Getenv(envs[k]) != "" {
				return nil, Fail(400, "Service setting controlled by environment")
			}
			if (k == "label" || k == "brewLabel") && !Prefix.MatchString(str) {
				return nil, Fail(400, "Invalid service label")
			}
			if k == "consoleUrl" && !LoopbackURL(str) {
				return nil, Fail(400, "Service checks must stay on localhost")
			}
			if k == "repository" && !strings.HasPrefix(str, "https://") {
				return nil, Fail(400, "Use an HTTPS repository")
			}
			if (k == "config" || k == "brewBinary" || k == "brewPrefix") && !filepath.IsAbs(str) {
				return nil, Fail(400, "Use an absolute service path")
			}
			values[k] = str
		}
		if err = WriteJSON(file, values); err != nil {
			return nil, err
		}
	}
	sources := Object{}
	for k, v := range defaults {
		sources[k] = "default"
		if values[k] == nil {
			values[k] = v
		} else {
			sources[k] = "file"
		}
		if e := os.Getenv(envs[k]); e != "" {
			values[k] = e
			sources[k] = "env"
		}
	}
	return Object{"file": file, "values": values, "sources": sources}, nil
}
