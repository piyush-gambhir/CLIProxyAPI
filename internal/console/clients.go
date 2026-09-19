package console

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func canonical(name string) (string, error) {
	name = filepath.Clean(name)
	resolved, err := filepath.EvalSymlinks(name)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(name)
	if parent == name {
		return "", err
	}
	base, err := canonical(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.Base(name)), nil
}
func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}
func (s *Store) UserPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", Fail(400, "A local directory is required")
	}
	if value == "~" {
		value = s.Home
	} else if strings.HasPrefix(value, "~/") {
		value = filepath.Join(s.Home, value[2:])
	}
	if !filepath.IsAbs(value) {
		return "", Fail(400, "Use an absolute path or ~/ path")
	}
	resolved, err := canonical(value)
	if err != nil {
		return "", err
	}
	home, err := canonical(s.Home)
	if err != nil {
		return "", err
	}
	if !inside(home, resolved) {
		return "", Fail(400, "Client folders must be inside your home directory")
	}
	for _, p := range []string{s.Directory, s.AuthDir, s.ConfigFile, filepath.Join(s.Home, ".cli-proxy-api")} {
		if p == "" {
			continue
		}
		p, err = canonical(p)
		if err != nil {
			return "", err
		}
		if inside(p, resolved) {
			return "", Fail(400, "Client settings cannot overwrite proxy storage or credentials")
		}
	}
	return resolved, nil
}
func ReadJSON(file string) (Object, bool, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return Object{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var obj Object
	if json.Unmarshal(b, &obj) != nil || obj == nil {
		return nil, true, Fail(409, "Invalid JSON object in "+file)
	}
	if v, ok := obj["env"]; ok {
		if _, ok = v.(map[string]any); !ok {
			return nil, true, Fail(409, "Settings env must be an object")
		}
	}
	return obj, true, nil
}
func WriteJSON(file string, value any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".proxy-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), file)
}
func Backup(file string) (string, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := file + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return name, closeErr
}
func (s *Store) ClientFile(dir, name string) (string, error) {
	base, err := s.UserPath(dir)
	if err != nil {
		return "", err
	}
	file := filepath.Join(base, name)
	return s.UserPath(file)
}
func (s *Store) DesktopFile() (string, error) {
	cfg := s.Settings()
	file, err := s.ClientFile(cfg.DesktopDir, "_meta.json")
	if err != nil {
		return "", err
	}
	meta, _, err := ReadJSON(file)
	if err != nil {
		return "", err
	}
	id := Text(meta["appliedId"])
	if !ID.MatchString(id) {
		return "", Fail(409, "Configure a local Gateway connection in Claude Desktop first")
	}
	return s.ClientFile(cfg.DesktopDir, id+".json")
}
func (s *Store) Desktop() (Object, error) {
	file, err := s.DesktopFile()
	if err != nil {
		return nil, err
	}
	cfg, _, err := ReadJSON(file)
	if err != nil {
		return nil, err
	}
	profile := ""
	if parts := strings.Split(Text(cfg["inferenceGatewayBaseUrl"]), "/inference/"); len(parts) == 2 {
		profile = parts[1]
	}
	models, ok := cfg["inferenceModels"].([]any)
	if !ok {
		models = []any{}
	}
	return Object{"file": file, "profile": profile, "oneMillionConfigured": cfg["modelDiscoveryEnabled"] == false && cfg["modelPrefer1mContext"] == true, "autoMode": cfg["autoModeEnabled"] == true, "models": models, "defaultEffort": Text(cfg["defaultModelEffort"]), "alwaysDefault": cfg["alwaysStartWithDefaultModel"] == true, "gatewayUrl": Text(cfg["inferenceGatewayBaseUrl"])}, nil
}
func RoleEnv(main, background, subagent string) Object {
	main = strings.TrimSuffix(main, "[1m]")
	if background == "" {
		background = main
	}
	if subagent == "" {
		subagent = main
	}
	return Object{"ANTHROPIC_DEFAULT_HAIKU_MODEL": background + "[1m]", "CLAUDE_CODE_SUBAGENT_MODEL": subagent + "[1m]"}
}
func (s *Store) RoleMatch(main string) bool {
	cfg := s.Settings()
	file, err := s.ClientFile(cfg.ClaudeDir, "settings.json")
	if err != nil {
		return false
	}
	obj, _, err := ReadJSON(file)
	if err != nil {
		return false
	}
	env := Map(obj["env"])
	for k, v := range RoleEnv(main, cfg.Background, cfg.Subagent) {
		if env[k] != v {
			return false
		}
	}
	return env["ANTHROPIC_SMALL_FAST_MODEL"] == nil
}

// ApplyDesktop validates and backs up both files before either is changed. On failure
// it restores the first write; unrelated settings and all session files are retained.
func (s *Store) ApplyDesktop(body Object, validate func(string, string) error) (Object, error) {
	cfg := s.Settings()
	id := Text(body["profile"])
	p, err := s.Profile(id)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(p.AuthFile, "claude-") {
		return nil, Fail(400, "Choose a Claude subscription")
	}
	for _, key := range []string{"autoMode", "alwaysDefault"} {
		if _, ok := body[key].(bool); !ok {
			return nil, Fail(400, key+" must be a boolean")
		}
	}
	effort, ok := body["defaultEffort"].(string)
	if !ok || (effort != "" && EffortIndex(effort) < 0) {
		return nil, Fail(400, "Invalid default effort")
	}
	entries, ok := body["models"].([]any)
	if !ok || len(entries) == 0 || len(entries) > 50 {
		return nil, Fail(400, "Select 1–50 models")
	}
	models := []Object{}
	seen := map[string]bool{}
	for _, entry := range entries {
		m := Map(entry)
		name := Text(m["name"])
		var option *Model
		for i := range cfg.Models {
			if cfg.Models[i].ID == name {
				option = &cfg.Models[i]
				break
			}
		}
		if option == nil || seen[name] {
			return nil, Fail(400, "Choose unique allowed 1M models")
		}
		seen[name] = true
		cap := Text(m["maxEffort"])
		if cap == "" {
			cap = option.MaxEffort
		}
		if EffortIndex(cap) < 0 || EffortIndex(cap) > EffortIndex(option.MaxEffort) {
			return nil, Fail(400, "Invalid model effort cap")
		}
		if err = validate(id, name); err != nil {
			return nil, err
		}
		label := Text(m["labelOverride"])
		if strings.TrimSpace(label) == "" || len(label) > 200 {
			return nil, Fail(400, "A model label is required")
		}
		models = append(models, Object{"name": name, "labelOverride": label, "maxEffort": cap, "supports1m": true, "prefer1m": true})
	}
	if effort != "" && EffortIndex(effort) > EffortIndex(Text(models[0]["maxEffort"])) {
		return nil, Fail(400, "Default effort exceeds the selected model cap")
	}
	file, err := s.DesktopFile()
	if err != nil {
		return nil, err
	}
	old, _, err := ReadJSON(file)
	if err != nil {
		return nil, err
	}
	if old["inferenceProvider"] != "gateway" {
		return nil, Fail(409, "Configure a local Gateway connection first")
	}
	// Never change a gateway belonging to another host.
	if !LoopbackURL(Text(old["inferenceGatewayBaseUrl"])) {
		return nil, Fail(409, "Only a local gateway configuration can be changed")
	}
	roleFile, err := s.ClientFile(cfg.ClaudeDir, "settings.json")
	if err != nil {
		return nil, err
	}
	roles, _, err := ReadJSON(roleFile)
	if err != nil {
		return nil, err
	}
	env := Map(roles["env"])
	for k, v := range RoleEnv(Text(models[0]["name"]), cfg.Background, cfg.Subagent) {
		env[k] = v
	}
	delete(env, "ANTHROPIC_SMALL_FAST_MODEL")
	roles["env"] = env
	next := Clone(old)
	next["inferenceGatewayBaseUrl"] = s.Origin + "/inference/" + id
	next["autoModeEnabled"] = body["autoMode"]
	next["alwaysStartWithDefaultModel"] = body["alwaysDefault"]
	next["modelDiscoveryEnabled"] = false
	next["modelPrefer1mContext"] = true
	next["inferenceModels"] = models
	delete(next, "defaultModelEffort")
	if effort != "" {
		next["defaultModelEffort"] = effort
	}
	backup, err := Backup(file)
	if err != nil {
		return nil, err
	}
	roleBackup, err := Backup(roleFile)
	if err != nil {
		return nil, err
	}
	if err = WriteJSON(file, next); err != nil {
		return nil, err
	}
	if err = WriteJSON(roleFile, roles); err != nil {
		if restoreErr := WriteJSON(file, old); restoreErr != nil {
			return nil, fmt.Errorf("role update failed and rollback failed; restore %s", backup)
		}
		return nil, err
	}
	out, err := s.Desktop()
	if err != nil {
		return nil, err
	}
	out["backup"] = backup
	out["roleSettingsBackup"] = roleBackup
	out["roleSettingsMatch"] = true
	out["restartRequired"] = true
	return out, nil
}
func (s *Store) Env(profile, model, key string) (Object, error) {
	cfg := s.Settings()
	p, err := s.Profile(profile)
	if err != nil {
		return nil, err
	}
	model = strings.TrimSuffix(model, "[1m]")
	claude := strings.HasPrefix(p.AuthFile, "claude-")
	url := s.Origin
	if claude {
		found := false
		for _, m := range cfg.Models {
			if m.ID == model {
				found = true
			}
		}
		if !found {
			return nil, Fail(400, "Choose an allowed official model")
		}
		url += "/inference/" + p.ID
		model += "[1m]"
	}
	if key == "" {
		key = cfg.ClientAPIKey
	}
	if key == "" {
		return nil, Fail(400, "Save a proxy client API key first")
	}
	env := Object{"ANTHROPIC_BASE_URL": url, "ANTHROPIC_AUTH_TOKEN": key, "ANTHROPIC_MODEL": model}
	for _, family := range []string{"OPUS", "FABLE", "SONNET"} {
		selected := model
		if claude {
			for _, m := range cfg.Models {
				if strings.HasPrefix(m.ID, "claude-"+strings.ToLower(family)+"-") {
					selected = m.ID + "[1m]"
					break
				}
			}
		}
		env["ANTHROPIC_DEFAULT_"+family+"_MODEL"] = selected
	}
	if claude {
		env["CLAUDE_CODE_DISABLE_1M_CONTEXT"] = "0"
		for k, v := range RoleEnv(model, cfg.Background, cfg.Subagent) {
			env[k] = v
		}
	} else {
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = model
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = model
	}
	return env, nil
}
func (s *Store) Wire(body Object, apply bool, validate func(string, string) error) (Object, error) {
	target := Text(body["target"])
	if target != "project" && target != "profile-global" {
		return nil, Fail(400, "Choose project or profile-global")
	}
	dir, err := s.UserPath(Text(body["path"]))
	if err != nil {
		return nil, err
	}
	name := "settings.json"
	if target == "project" {
		name = filepath.Join(".claude", name)
	}
	file, err := s.ClientFile(dir, name)
	if err != nil {
		return nil, err
	}
	id, model := Text(body["profile"]), Text(body["model"])
	if err = validate(id, model); err != nil {
		return nil, err
	}
	env, err := s.Env(id, model, Text(body["apiKey"]))
	if err != nil {
		return nil, err
	}
	existing, exists, err := ReadJSON(file)
	if err != nil {
		return nil, err
	}
	previous := Map(existing["env"])
	overwrites := []string{}
	for k, v := range env {
		if old, ok := previous[k]; ok && old != v {
			overwrites = append(overwrites, k)
		}
		previous[k] = v
	}
	delete(previous, "ANTHROPIC_SMALL_FAST_MODEL")
	existing["env"] = previous
	out := Object{"file": file, "displayFile": file, "env": env, "merged": existing, "fileExists": exists, "overwrites": overwrites}
	if apply {
		backup, err := Backup(file)
		if err != nil {
			return nil, err
		}
		if err = WriteJSON(file, existing); err != nil {
			return nil, err
		}
		out["backupFile"] = nil
		out["displayBackupFile"] = nil
		if backup != "" {
			out["backupFile"] = backup
			out["displayBackupFile"] = backup
		}
		if target == "project" {
			if err = s.PutObject("recents", Object{"paths": []string{dir}}); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
func ShellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
func (s *Store) Snippet(id, model string) (string, error) {
	env, err := s.Env(id, model, "$CLIENT_KEY$")
	if err != nil {
		return "", err
	}
	lines := []string{"# CLIProxyAPI: selected subscription", "claude-proxy() {"}
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "CLAUDE_CODE_DISABLE_1M_CONTEXT", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
		if v, ok := env[key]; ok {
			lines = append(lines, "  "+key+"="+ShellQuote(Text(v))+" \\")
		}
	}
	lines = append(lines, `  ANTHROPIC_AUTH_TOKEN="${CLIPROXY_API_KEY:?Set a proxy client key first}" \`, `  command claude "$@"`, "}")
	return strings.Join(lines, "\n"), nil
}
func (s *Store) Launch(args []string) error {
	cfg := s.Settings()
	id, model := cfg.CLIProfile, ""
	effort := false
	pass := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--list-subscriptions" {
			for _, p := range s.Profiles() {
				if strings.HasPrefix(p.AuthFile, "claude-") {
					fmt.Printf("%s [%s]\n", p.Name, p.ID)
				}
			}
			return nil
		}
		if a == "--subscription" || a == "--model" {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return Fail(400, "Missing "+a+" value")
			}
			if a == "--subscription" {
				id = args[i]
			} else {
				model = args[i]
			}
			continue
		}
		if strings.HasPrefix(a, "--subscription=") {
			id = strings.TrimPrefix(a, "--subscription=")
			continue
		}
		if strings.HasPrefix(a, "--model=") {
			model = strings.TrimPrefix(a, "--model=")
			continue
		}
		name := strings.SplitN(a, "=", 2)[0]
		switch name {
		case "remote", "remote-control", "web", "--remote", "--remote-control", "--teleport", "--resume-url", "--fallback-model":
			return Fail(400, "Cloud, Remote Control and automatic fallback are disabled")
		}
		if name == "--effort" {
			effort = true
		}
		pass = append(pass, a)
	}
	var desktop Object
	if id == "" || cfg.ClientAPIKey == "" {
		file, err := s.DesktopFile()
		if err != nil {
			return err
		}
		desktop, _, err = ReadJSON(file)
		if err != nil {
			return err
		}
		if id == "" {
			parts := strings.Split(Text(desktop["inferenceGatewayBaseUrl"]), "/inference/")
			if len(parts) == 2 {
				id = parts[1]
			}
		}
	}
	candidates := []Profile{}
	for _, p := range s.Profiles() {
		if strings.HasPrefix(p.AuthFile, "claude-") && (p.ID == id || strings.EqualFold(p.Name, id)) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) != 1 {
		return Fail(400, "Choose a unique subscription in Claude setup")
	}
	p := candidates[0]
	model = strings.TrimSuffix(model, "[1m]")
	if model == "" {
		model = cfg.Models[0].ID
	}
	matches := []string{}
	for _, m := range cfg.Models {
		if m.ID == model {
			matches = []string{m.ID}
			break
		}
		if strings.HasPrefix(m.ID, "claude-"+model+"-") {
			matches = append(matches, m.ID)
		}
	}
	if len(matches) != 1 {
		return Fail(400, "Choose a configured official 1M model")
	}
	model = matches[0]
	if !LoopbackURL(s.Origin) {
		return Fail(400, "The local launcher requires a loopback proxy")
	}
	key := cfg.ClientAPIKey
	if key == "" {
		key = Text(desktop["inferenceGatewayApiKey"])
	}
	env, err := s.Env(p.ID, model, key)
	if err != nil {
		return err
	}
	env["CLAUDE_CONFIG_DIR"] = cfg.ClaudeDir
	clean := []string{}
	for _, v := range os.Environ() {
		k := strings.SplitN(v, "=", 2)[0]
		if _, ok := env[k]; ok || k == "ANTHROPIC_API_KEY" || k == "CLAUDE_CODE_OAUTH_TOKEN" || k == "ANTHROPIC_SMALL_FAST_MODEL" {
			continue
		}
		clean = append(clean, v)
	}
	for k, v := range env {
		clean = append(clean, k+"="+Text(v))
	}
	pass = append(pass, "--model", model+"[1m]")
	if !effort {
		pass = append(pass, "--effort", cfg.Effort)
	}
	command := exec.Command("claude", pass...)
	command.Env = clean
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

// Command returns an invocation with a required client-key environment reference.
// It never returns the stored key, and validates effort against the selected model.
func (s *Store) Command(id, model, effort string) (string, error) {
	cfg := s.Settings()
	rank := EffortIndex(effort)
	if effort == "ultracode" {
		rank = EffortIndex("xhigh")
	}
	valid := false
	for _, m := range cfg.Models {
		if m.ID == model && rank >= 0 && rank <= EffortIndex(m.MaxEffort) {
			valid = true
		}
	}
	if !valid {
		return "", Fail(400, "Choose an allowed model and supported effort")
	}
	env, err := s.Env(id, model, "$CLIENT_KEY$")
	if err != nil {
		return "", err
	}
	lines := []string{"CLAUDE_CONFIG_DIR=" + ShellQuote(cfg.ClaudeDir) + ` \`}
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "CLAUDE_CODE_DISABLE_1M_CONTEXT", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
		if v, ok := env[key]; ok {
			lines = append(lines, key+"="+ShellQuote(Text(v))+" \\")
		}
	}
	lines = append(lines, `ANTHROPIC_AUTH_TOKEN="${CLIPROXY_API_KEY:?Set a proxy client key first}" \`, "command claude --model "+ShellQuote(model+"[1m]")+" --effort "+ShellQuote(effort))
	return strings.Join(lines, "\n"), nil
}
