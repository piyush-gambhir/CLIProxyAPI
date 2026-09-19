// Package console owns local account administration and Claude client setup.
// The browser is a stateless management client; this package never calls a console server.
package console

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Object = map[string]any
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string              { return e.Message }
func Fail(status int, message string) error { return &Error{status, message} }
func Text(v any) string                     { s, _ := v.(string); return s }
func Map(v any) Object {
	m, _ := v.(map[string]any)
	if m == nil {
		return Object{}
	}
	return m
}
func Clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }

var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

func EffortIndex(v string) int {
	for i, s := range Efforts {
		if s == v {
			return i
		}
	}
	return -1
}

var ModelID = regexp.MustCompile(`^claude-[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
var ID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`)
var Prefix = regexp.MustCompile(`^[A-Za-z0-9._-]*$`)

type Model struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ContextWindow int    `json:"contextWindow"`
	MaxEffort     string `json:"maxEffort"`
}
type Settings struct {
	DisplayName  string  `json:"displayName"`
	ClientAPIKey string  `json:"clientApiKey"`
	Models       []Model `json:"claudeModels"`
	Effort       string  `json:"cliEffort"`
	CLIProfile   string  `json:"cliProfile"`
	Background   string  `json:"claudeBackgroundModel"`
	Subagent     string  `json:"claudeSubagentModel"`
	Retention    int     `json:"receiptRetentionDays"`
	ClaudeDir    string  `json:"claudeConfigDir"`
	DesktopDir   string  `json:"desktopConfigDir"`
}
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AuthFile  string `json:"authFile"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
	Prefix    string `json:"lastKnownPrefix,omitempty"`
	ResetAt   string `json:"resetAt,omitempty"`
	Note      string `json:"subscriptionNote,omitempty"`
}
type Store struct {
	managed                                            bool
	mu                                                 sync.RWMutex
	db                                                 *sql.DB
	File, Directory, Home, AuthDir, ConfigFile, Origin string
	settings                                           Settings
	profiles                                           []Profile
	env                                                map[string]string
}

var settingEnv = map[string]string{"displayName": "CLIPROXY_DISPLAY_NAME", "clientApiKey": "CLIPROXY_API_KEY", "claudeModels": "CLIPROXY_CLAUDE_MODELS", "cliEffort": "CLIPROXY_CLI_EFFORT", "cliProfile": "CLIPROXY_CLI_PROFILE", "claudeBackgroundModel": "CLIPROXY_CLAUDE_BACKGROUND_MODEL", "claudeSubagentModel": "CLIPROXY_CLAUDE_SUBAGENT_MODEL", "claudeConfigDir": "CLAUDE_CONFIG_DIR", "desktopConfigDir": "CLIPROXY_DESKTOP_CONFIG_DIR"}

func DefaultSettings(home string) Settings {
	return Settings{DisplayName: "CLIProxy Console", Models: []Model{{"claude-opus-5", "Claude Opus 5", 1000000, "max"}, {"claude-fable-5-1", "Claude Fable 5.1", 1000000, "max"}}, Effort: "high", Retention: 30, ClaudeDir: filepath.Join(home, ".claude"), DesktopDir: filepath.Join(home, "Library/Application Support/Claude-3p/configLibrary")}
}
func Open(directory, home, authDir, configFile, origin string) (*Store, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	file := filepath.Join(directory, "console.sqlite")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(file, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", file)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, File: file, Directory: directory, Home: home, AuthDir: authDir, ConfigFile: configFile, Origin: origin, env: map[string]string{}}
	for key, name := range settingEnv {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			s.env[key] = v
		}
	}
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA secure_delete=ON; CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS objects(key TEXT PRIMARY KEY,value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS receipts(id TEXT PRIMARY KEY,started_at TEXT NOT NULL,profile TEXT NOT NULL,data TEXT NOT NULL); CREATE INDEX IF NOT EXISTS receipts_time ON receipts(started_at DESC);`)
	if err == nil {
		err = s.reloadLocked()
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Managed() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.managed }
func (s *Store) Close() error  { return s.db.Close() }
func (s *Store) stored() (Object, error) {
	rows, err := s.db.Query("SELECT key,value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := Object{}
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(v), newValue(out, k)); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

// Decode through a temporary map to retain JSON types and reject corrupt saved values.
func newValue(m Object, k string) *rawValue { return &rawValue{m: m, k: k} }

type rawValue struct {
	m Object
	k string
}

func (r *rawValue) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	r.m[r.k] = v
	return nil
}
func (s *Store) resolve(raw Object) (Settings, error) {
	b, _ := json.Marshal(DefaultSettings(s.Home))
	out := Object{}
	_ = json.Unmarshal(b, &out)
	for k, v := range raw {
		out[k] = v
	}
	for k, v := range s.env {
		if k == "claudeModels" {
			var a any
			if json.Unmarshal([]byte(v), &a) != nil {
				return Settings{}, Fail(400, "CLIPROXY_CLAUDE_MODELS must be JSON")
			}
			out[k] = a
		} else {
			out[k] = v
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return Settings{}, err
	}
	var cfg Settings
	if err = json.Unmarshal(b, &cfg); err != nil {
		return cfg, Fail(400, "Invalid settings types")
	}
	if err = s.validate(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
func (s *Store) validate(c Settings) error {
	if strings.TrimSpace(c.DisplayName) == "" || len(c.DisplayName) > 80 {
		return Fail(400, "Console name must contain 1–80 characters")
	}
	if strings.ContainsAny(c.ClientAPIKey, "\r\n\x00") {
		return Fail(400, "Invalid client key")
	}
	if len(c.Models) < 1 || len(c.Models) > 50 {
		return Fail(400, "Configure 1–50 official 1M models")
	}
	ids := map[string]bool{}
	for _, m := range c.Models {
		if !ModelID.MatchString(m.ID) || ids[m.ID] || m.ContextWindow != 1000000 || strings.TrimSpace(m.Label) == "" || len(m.Label) > 100 || EffortIndex(m.MaxEffort) < 0 {
			return Fail(400, "Use unique official Claude model IDs, 1M context and valid effort caps")
		}
		ids[m.ID] = true
	}
	for _, m := range []string{c.Background, c.Subagent} {
		if m != "" && !ids[m] {
			return Fail(400, "Role models must be in the allowed model list")
		}
	}
	if EffortIndex(c.Effort) < 0 || EffortIndex(c.Effort) > EffortIndex(c.Models[0].MaxEffort) {
		return Fail(400, "Default effort exceeds the first model's cap")
	}
	if c.Retention < 1 || c.Retention > 365 {
		return Fail(400, "History retention must be 1–365 days")
	}
	if c.CLIProfile != "" && !ID.MatchString(c.CLIProfile) {
		return Fail(400, "Invalid CLI subscription")
	}
	for _, dir := range []string{c.ClaudeDir, c.DesktopDir} {
		if _, err := s.UserPath(dir); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) reloadLocked() error {
	raw, err := s.stored()
	if err != nil {
		return err
	}
	cfg, err := s.resolve(raw)
	if err != nil {
		return err
	}
	var profiles []Profile
	if err = s.object("profiles", &profiles); err != nil {
		return err
	}
	if profiles == nil {
		profiles = []Profile{}
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM objects WHERE key='profiles'").Scan(&count); err != nil {
		return err
	}
	s.managed = count > 0
	s.settings = cfg
	s.profiles = profiles
	return nil
}
func (s *Store) Settings() Settings  { s.mu.RLock(); defer s.mu.RUnlock(); return Clone(s.settings) }
func (s *Store) Profiles() []Profile { s.mu.RLock(); defer s.mu.RUnlock(); return Clone(s.profiles) }
func (s *Store) Profile(id string) (Profile, error) {
	for _, p := range s.Profiles() {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, Fail(404, "Subscription not found")
}
func (s *Store) object(key string, out any) error {
	var v string
	err := s.db.QueryRow("SELECT value FROM objects WHERE key=?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(v), out)
}
func (s *Store) Object(key string) (Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := Object{}
	err := s.object(key, &v)
	return v, err
}
func (s *Store) PutObject(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO objects VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return err
}
func (s *Store) UpdateSettings(patch Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := s.stored()
	if err != nil {
		return err
	}
	allowed := MapFromSettings(DefaultSettings(s.Home))
	allowed["receiptRetentionDays"] = 30
	for k, v := range patch {
		if _, ok := allowed[k]; !ok {
			return Fail(400, "Unknown or proxy-owned setting: "+k)
		}
		if s.env[k] != "" {
			return Fail(400, settingEnv[k]+" controls this setting")
		}
		raw[k] = v
	}
	cfg, err := s.resolve(raw)
	if err != nil {
		return err
	}
	if cfg.CLIProfile != "" {
		found := false
		for _, p := range s.profiles {
			if p.ID == cfg.CLIProfile && strings.HasPrefix(p.AuthFile, "claude-") {
				found = true
			}
		}
		if !found {
			return Fail(400, "Choose a saved Claude subscription")
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range patch {
		b, _ := json.Marshal(v)
		if _, err = tx.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, string(b)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.settings = cfg
	return nil
}
func MapFromSettings(c Settings) Object {
	b, _ := json.Marshal(c)
	var o Object
	_ = json.Unmarshal(b, &o)
	return o
}
func (s *Store) PublicSettings() (Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, err := s.stored()
	if err != nil {
		return nil, err
	}
	out := MapFromSettings(s.settings)
	delete(out, "clientApiKey")
	sources := Object{}
	for k := range settingEnv {
		source := "default"
		if _, ok := raw[k]; ok {
			source = "sqlite"
		}
		if s.env[k] != "" {
			source = "env"
		}
		sources[k] = source
	}
	out["sources"] = sources
	out["proxyUrl"] = s.Origin
	out["consoleUrl"] = s.Origin
	out["routingMode"] = "manual"
	out["configFile"] = s.File
	out["hasManagementKey"] = true
	out["hasStoredManagementKey"] = false
	out["keySource"] = "proxy"
	out["hasClientApiKey"] = s.settings.ClientAPIKey != ""
	out["hasStoredClientApiKey"] = Text(raw["clientApiKey"]) != ""
	out["clientKeySource"] = sources["clientApiKey"]
	out["backend"] = "CLIProxyAPI"
	return out, nil
}
func (s *Store) ChangeProfile(id string, patch Object, remove bool) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := Clone(s.profiles)
	idx := -1
	for i, p := range rows {
		if p.ID == id {
			idx = i
		}
	}
	if idx < 0 {
		return Profile{}, Fail(404, "Subscription not found")
	}
	p := rows[idx]
	if remove {
		rows = append(rows[:idx], rows[idx+1:]...)
	} else {
		b, _ := json.Marshal(p)
		m := Object{}
		_ = json.Unmarshal(b, &m)
		for k, v := range patch {
			switch k {
			case "name", "authFile", "color", "lastKnownPrefix", "resetAt", "subscriptionNote":
				m[k] = v
			default:
				return p, Fail(400, "Unknown profile field: "+k)
			}
		}
		b, _ = json.Marshal(m)
		if err := json.Unmarshal(b, &p); err != nil {
			return p, Fail(400, "Invalid profile values")
		}
		if err := validProfile(p, rows); err != nil {
			return p, err
		}
		rows[idx] = p
	}
	b, _ := json.Marshal(rows)
	if _, err := s.db.Exec("INSERT INTO objects VALUES('profiles',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(b)); err != nil {
		return p, err
	}
	s.managed = true
	s.profiles = rows
	return p, nil
}
func validProfile(p Profile, rows []Profile) error {
	if !ID.MatchString(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || p.AuthFile == "" || filepath.Base(p.AuthFile) != p.AuthFile || !Prefix.MatchString(p.Prefix) || !regexp.MustCompile(`^#[a-fA-F0-9]{6}$`).MatchString(p.Color) || len(p.Note) > 500 {
		return Fail(400, "Invalid subscription name, account, prefix, color or note")
	}
	if p.ResetAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.ResetAt); err != nil {
			return Fail(400, "Reset date must be valid")
		}
	}
	for _, r := range rows {
		if r.ID != p.ID && (r.AuthFile == p.AuthFile || strings.EqualFold(r.Name, p.Name)) {
			return Fail(409, "Subscription name or account is already assigned")
		}
	}
	return nil
}
func (s *Store) AddProfile(p Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validProfile(p, s.profiles); err != nil {
		return err
	}
	rows := append(Clone(s.profiles), p)
	b, _ := json.Marshal(rows)
	_, err := s.db.Exec("INSERT INTO objects VALUES('profiles',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(b))
	if err == nil {
		s.managed = true
		s.profiles = rows
	}
	return err
}
func (s *Store) Record(receipt any) error {
	b, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	var row struct {
		ID        string `json:"id"`
		StartedAt string `json:"startedAt"`
		Profile   string `json:"profile"`
	}
	if err = json.Unmarshal(b, &row); err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339Nano, row.StartedAt)
	if err != nil || row.ID == "" {
		return fmt.Errorf("invalid receipt metadata")
	}
	_, err = s.db.Exec("INSERT OR IGNORE INTO receipts VALUES(?,?,?,?)", row.ID, t.UTC().Format("2006-01-02T15:04:05.000Z"), row.Profile, string(b))
	return err
}
func (s *Store) Receipts(profile string) ([]json.RawMessage, error) {
	return s.RecentReceipts(profile, 200)
}
func (s *Store) RecentReceipts(profile string, limit int) ([]json.RawMessage, error) {
	if limit < 1 || limit > 5000 {
		return nil, Fail(400, "Invalid receipt limit")
	}
	cut := time.Now().AddDate(0, 0, -s.Settings().Retention).UTC().Format("2006-01-02T15:04:05.000Z")
	rows, err := s.db.Query("SELECT data FROM receipts WHERE started_at>=? AND (?='' OR profile=?) ORDER BY started_at DESC LIMIT ?", cut, profile, profile, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(b))
	}
	return out, rows.Err()
}
func (s *Store) Prune() error {
	_, err := s.db.Exec("DELETE FROM receipts WHERE started_at < ?", time.Now().AddDate(0, 0, -s.Settings().Retention).UTC().Format("2006-01-02T15:04:05.000Z"))
	return err
}
