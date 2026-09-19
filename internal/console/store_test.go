package console

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	for _, env := range settingEnv {
		t.Setenv(env, "")
	}
	home := t.TempDir()
	s, err := Open(filepath.Join(home, "proxy-admin"), home, filepath.Join(home, "auths"), filepath.Join(home, "proxy.yaml"), "http://127.0.0.1:8317")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func add(t *testing.T, s *Store) Profile {
	t.Helper()
	p := Profile{ID: "account-one", Name: "Personal", AuthFile: "claude-one.json", Color: "#6366f1", Prefix: "chosen", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := s.AddProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestSettingsAndProfilePersistence(t *testing.T) {
	s := testStore(t)
	p := add(t, s)
	err := s.UpdateSettings(Object{"clientApiKey": "private-client-key", "cliProfile": p.ID, "claudeBackgroundModel": "claude-fable-5-1"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.PublicSettings()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(public)
	if strings.Contains(string(b), "private-client-key") {
		t.Fatal("key exposed")
	}
	if !s.Managed() {
		t.Fatal("profile ownership absent")
	}
	_, err = s.ChangeProfile(p.ID, Object{"subscriptionNote": "Keep this", "resetAt": "2026-09-25T04:00:00Z"}, false)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Open(s.Directory, s.Home, s.AuthDir, s.ConfigFile, s.Origin)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.Settings().CLIProfile != p.ID || len(other.Profiles()) != 1 || other.Profiles()[0].Note != "Keep this" {
		t.Fatal("data not persisted")
	}
	info, _ := os.Stat(s.File)
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}
func TestRejectInvalidSettingsAtomically(t *testing.T) {
	s := testStore(t)
	original := s.Settings()
	for _, patch := range []Object{{"managementKey": "no"}, {"claudeModels": []Model{{ID: "account/claude-opus-5", Label: "Opus", ContextWindow: 1000000, MaxEffort: "high"}}}, {"receiptRetentionDays": 0}, {"claudeSubagentModel": "unknown"}, {"cliProfile": "absent"}, {"claudeModels": []Model{{ID: "claude-opus-5", Label: "Opus", ContextWindow: 200000, MaxEffort: "high"}}}} {
		if err := s.UpdateSettings(patch); err == nil {
			t.Fatalf("accepted %+v", patch)
		}
	}
	if s.Settings().DisplayName != original.DisplayName || s.Settings().Retention != 30 {
		t.Fatal("partial update")
	}
}
func TestEnvironmentOverrideNotSaved(t *testing.T) {
	s := testStore(t)
	s.env["clientApiKey"] = "environment-secret"
	if err := s.UpdateSettings(Object{"displayName": "Saved name"}); err != nil {
		t.Fatal(err)
	}
	if s.Settings().ClientAPIKey != "environment-secret" {
		t.Fatal("override not used")
	}
	if err := s.UpdateSettings(Object{"clientApiKey": "other"}); err == nil {
		t.Fatal("overrode environment")
	}
	raw, err := s.stored()
	if err != nil || raw["clientApiKey"] != nil {
		t.Fatal("environment leaked into storage")
	}
}
func TestProfileUniquenessAndDeleteOwnership(t *testing.T) {
	s := testStore(t)
	p := add(t, s)
	copy := p
	copy.ID = "another"
	if err := s.AddProfile(copy); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := s.ChangeProfile(p.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(s.Profiles()) != 0 || !s.Managed() {
		t.Fatal("empty store would fall back to legacy routes")
	}
}
func TestClientPathsRejectSymlinksAndProtectedFiles(t *testing.T) {
	s := testStore(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Home, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClientFile(filepath.Join(s.Home, "escape"), "settings.json"); err == nil {
		t.Fatal("symlink escaped home")
	}
	for _, dir := range []string{s.Directory, s.AuthDir, s.ConfigFile, "/tmp"} {
		if _, err := s.UserPath(dir); err == nil {
			t.Fatalf("allowed protected path %s", dir)
		}
	}
}
func TestWirePreservesSettingsAndBacksUp(t *testing.T) {
	s := testStore(t)
	p := add(t, s)
	if err := s.UpdateSettings(Object{"clientApiKey": "key"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(s.Home, "project/.claude/settings.json")
	old := Object{"permissions": Object{"defaultMode": "acceptEdits"}, "env": Object{"OTHER": "keep", "ANTHROPIC_SMALL_FAST_MODEL": "old"}}
	if err := WriteJSON(file, old); err != nil {
		t.Fatal(err)
	}
	body := Object{"profile": p.ID, "model": "claude-opus-5", "path": filepath.Join(s.Home, "project"), "target": "project"}
	plan, err := s.Wire(body, false, func(string, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := ReadJSON(file)
	if Map(current["env"])["ANTHROPIC_MODEL"] != nil {
		t.Fatal("preview wrote files")
	}
	if Map(plan["env"])["ANTHROPIC_MODEL"] != "claude-opus-5[1m]" {
		t.Fatal("model")
	}
	result, err := s.Wire(body, true, func(string, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ = ReadJSON(file)
	if Map(current["env"])["OTHER"] != "keep" || current["permissions"] == nil || Map(current["env"])["ANTHROPIC_SMALL_FAST_MODEL"] != nil {
		t.Fatal("lost unrelated settings")
	}
	backup, _, _ := ReadJSON(Text(result["backupFile"]))
	if Map(backup["env"])["ANTHROPIC_SMALL_FAST_MODEL"] != "old" {
		t.Fatal("backup missing")
	}
}
func TestDesktopAndRolesPreserveHistory(t *testing.T) {
	s := testStore(t)
	p := add(t, s)
	cfg := s.Settings()
	if err := WriteJSON(filepath.Join(cfg.DesktopDir, "_meta.json"), Object{"appliedId": "local-gateway"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cfg.DesktopDir, "local-gateway.json")
	if err := WriteJSON(file, Object{"inferenceProvider": "gateway", "inferenceGatewayBaseUrl": "http://127.0.0.1:8320/inference/old", "inferenceGatewayApiKey": "client-secret", "unrelated": true}); err != nil {
		t.Fatal(err)
	}
	body := Object{"profile": p.ID, "models": []any{Object{"name": "claude-opus-5", "labelOverride": "Opus"}}, "alwaysDefault": true, "autoMode": true, "defaultEffort": "high"}
	result, err := s.ApplyDesktop(body, func(string, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	current, _, _ := ReadJSON(file)
	if current["unrelated"] != true || current["inferenceGatewayApiKey"] != "client-secret" || current["inferenceGatewayBaseUrl"] != s.Origin+"/inference/"+p.ID {
		t.Fatal("desktop fields")
	}
	if result["backup"] == "" || !s.RoleMatch("claude-opus-5") {
		t.Fatal("backup or role configuration")
	}
}
func TestCommandExecutedWithCanonicalModels(t *testing.T) {
	s := testStore(t)
	p := add(t, s)
	command, err := s.Command(p.ID, "claude-opus-5", "high")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(s.Home, "bin")
	_ = os.MkdirAll(bin, 0700)
	script := `#!/bin/sh
[ "$ANTHROPIC_MODEL" = 'claude-opus-5[1m]' ] && [ "$ANTHROPIC_AUTH_TOKEN" = 'session-key' ] && [ "$CLAUDE_CODE_DISABLE_1M_CONTEXT" = '0' ] && [ "$ANTHROPIC_DEFAULT_FABLE_MODEL" = 'claude-fable-5-1[1m]' ]
`
	if err = os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c := exec.Command("sh", "-c", command)
	c.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CLIPROXY_API_KEY=session-key")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("generated command failed %s: %v", out, err)
	}
}
func TestReceiptsPersistWithoutBrowserAndRespectRetention(t *testing.T) {
	s := testStore(t)
	for _, r := range []Object{{"id": "new", "startedAt": time.Now().Format(time.RFC3339Nano), "profile": "chosen", "inputTokens": nil}, {"id": "old", "startedAt": time.Now().AddDate(0, 0, -40).Format(time.RFC3339Nano), "profile": "chosen"}} {
		if err := s.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Receipts("chosen")
	if err != nil || len(rows) != 1 {
		t.Fatalf("receipts %v %v", rows, err)
	}
	if !strings.Contains(string(rows[0]), `"inputTokens":null`) {
		t.Fatal("missing counts became zero")
	}
	if err = s.Prune(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM receipts").Scan(&count); err != nil || count != 1 {
		t.Fatal("expired receipts retained")
	}
}
func TestUsageScopedLimitsAndUnknownValues(t *testing.T) {
	raw := Object{"limits": []any{Object{"kind": "session", "percent": float64(0)}, Object{"kind": "weekly_scoped", "group": "weekly", "percent": float64(89), "scope": Object{"model": Object{"display_name": "Fable"}}, "is_active": false}}, "seven_day": Object{"utilization": float64(50)}, "spend": Object{"enabled": false}}
	result := NormalizeUsage("claude", raw)
	windows := result["windows"].([]Object)
	if len(windows) != 2 || windows[0]["remainingPercent"] != float64(100) || windows[1]["remainingPercent"] != float64(11) || windows[1]["label"] != "Weekly limit · Fable" {
		t.Fatalf("bad windows %+v", windows)
	}
	if remaining(nil) != nil || remaining(float64(100)) != float64(0) {
		t.Fatal("unknown/zero confused")
	}
}
