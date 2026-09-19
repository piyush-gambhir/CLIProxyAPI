package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	consolecore "github.com/router-for-me/CLIProxyAPI/v7/internal/console"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	log "github.com/sirupsen/logrus"
)

type nativeConsole struct {
	store      *consolecore.Store
	err        error
	mutations  sync.Mutex
	file       string
	deployment consolecore.Deployment
	alias      *http.Server
	stop       chan struct{}
}

func (s *Server) setupConsole() {
	n := &nativeConsole{stop: make(chan struct{}), file: s.configFilePath + ".console.json"}
	s.console = n
	n.deployment, n.err = consolecore.LoadDeployment(s.configFilePath)
	if _, statErr := os.Stat(s.configFilePath); statErr != nil && os.Getenv("CLIPROXY_CONSOLE_DATA_DIR") == "" {
		n.err = fmt.Errorf("console requires an existing proxy configuration")
	}
	if n.err == nil {
		home, err := os.UserHomeDir()
		if err != nil {
			n.err = err
		} else {
			n.store, n.err = consolecore.Open(n.deployment.DataDir, home, s.cfg.AuthDir, s.configFilePath, fmt.Sprintf("http://127.0.0.1:%d", s.cfg.Port))
		}
	}
	if n.err != nil {
		log.WithError(n.err).Error("Native console storage unavailable")
	} else {
		s.accountGateway.nativeRoute = func(id string) (accountGatewayRoute, bool) {
			p, err := n.store.Profile(id)
			if err != nil || !strings.HasPrefix(p.AuthFile, "claude-") {
				return accountGatewayRoute{}, false
			}
			models := []accountGatewayModel{}
			for _, m := range n.store.Settings().Models {
				models = append(models, accountGatewayModel{ID: m.ID, Label: m.Label, ContextWindow: m.ContextWindow})
			}
			return accountGatewayRoute{ID: p.ID, AuthFile: p.AuthFile, Models: models}, true
		}
		// Once a console store owns profiles, route lookups never consult the legacy mirror.
		s.accountGateway.nativeManaged = n.store.Managed
		s.accountGateway.persistReceipt = func(r accountReceipt) error { return n.store.Record(r) }
		for _, receipt := range s.accountGateway.receipts {
			if err := n.store.Record(receipt); err != nil {
				log.WithError(err).Warn("Could not import legacy proxy receipt")
				break
			}
		}

	}
	s.engine.GET("/console", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/console/") })
	s.engine.GET("/console/*asset", s.serveConsole)
	s.engine.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "proxyVersion": buildinfo.Version, "backend": "CLIProxyAPI"})
	})
}
func (s *Server) serveConsole(c *gin.Context) {
	n := s.console
	if n.deployment.WebDir == "" {
		c.String(503, "Build the console frontend and configure its webDir in CLIProxyAPI")
		return
	}
	asset := strings.TrimPrefix(c.Param("asset"), "/")
	if asset == "" {
		asset = "index.html"
	}
	clean := filepath.Clean(asset)
	if clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
		c.Status(404)
		return
	}
	file := filepath.Join(n.deployment.WebDir, clean)
	real, err := filepath.EvalSymlinks(file)
	root, rootErr := filepath.EvalSymlinks(n.deployment.WebDir)
	if err != nil || rootErr != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		c.Status(404)
		return
	}
	if info, err := os.Stat(real); err != nil || info.IsDir() {
		c.Status(404)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
	c.File(real)
}
func (s *Server) startConsoleAlias() error {
	n := s.console
	if n == nil {
		return nil
	}
	if n.store != nil {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				if err := n.store.Prune(); err != nil {
					log.WithError(err).Warn("Console history retention failed")
				}
				select {
				case <-ticker.C:
				case <-n.stop:
					return
				}
			}
		}()
	}
	if n.deployment.CompatibilityPort == 0 {
		return nil
	}
	if n.deployment.CompatibilityPort == s.cfg.Port {
		return fmt.Errorf("console compatibility port must differ from proxy port")
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", n.deployment.CompatibilityPort))
	if err != nil {
		return err
	}
	n.alias = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, fmt.Sprintf("http://127.0.0.1:%d/console/", s.cfg.Port), http.StatusTemporaryRedirect)
			return
		}
		s.engine.ServeHTTP(w, r)
	})}
	go func() {
		if err := n.alias.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Error("Console compatibility listener failed")
		}
	}()
	return nil
}
func consoleLocal(c *gin.Context) bool {
	peer, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	ip := net.ParseIP(peer)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !consolecore.LoopbackURL(u.Scheme+"://"+u.Host) {
			return false
		}
	}
	return true
}

// Calls the proxy's existing management handlers in process, with the authenticated
// request context. No network proxy, management token copy, or console server exists.
func (s *Server) consoleCall(parent *gin.Context, method, route string, body any) (consolecore.Object, error) {
	handlers := map[string]gin.HandlerFunc{
		"GET config": s.mgmt.GetConfig, "GET auth-files": s.mgmt.ListAuthFiles, "GET auth-files/models": s.mgmt.GetAuthFileModels, "PATCH auth-files/fields": s.mgmt.PatchAuthFileFields,
		"POST api-call": s.mgmt.APICall, "POST quota/fetch": s.mgmt.FetchCredentialQuota,
		"PUT routing/strategy": s.mgmt.PutRoutingStrategy, "PUT force-model-prefix": s.mgmt.PutForceModelPrefix, "PUT request-retry": s.mgmt.PutRequestRetry, "PUT max-retry-interval": s.mgmt.PutMaxRetryInterval, "PUT quota-exceeded/switch-project": s.mgmt.PutSwitchProject, "PUT quota-exceeded/switch-preview-model": s.mgmt.PutSwitchPreviewModel,
	}
	u, err := url.Parse("/" + route)
	if err != nil {
		return nil, err
	}
	fn := handlers[method+" "+strings.TrimPrefix(u.Path, "/")]
	if fn == nil {
		return nil, consolecore.Fail(404, "Unsupported internal management operation")
	}
	var b []byte
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = parent.Request.Clone(parent.Request.Context())
	c.Request.Method = method
	c.Request.URL = u
	c.Request.Body = io.NopCloser(bytes.NewReader(b))
	c.Request.ContentLength = int64(len(b))
	c.Request.Header = c.Request.Header.Clone()
	c.Request.Header.Set("Content-Type", "application/json")
	fn(c)
	if recorder.Code >= 400 {
		var payload consolecore.Object
		_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
		message := consolecore.Text(payload["error"])
		if message == "" {
			message = fmt.Sprintf("Proxy management operation returned %d", recorder.Code)
		}
		return nil, consolecore.Fail(recorder.Code, message)
	}
	var out consolecore.Object
	if json.Unmarshal(recorder.Body.Bytes(), &out) != nil {
		return nil, consolecore.Fail(502, "Invalid management response")
	}
	return out, nil
}
func (s *Server) consoleStatus() gin.H {
	n := s.console
	status := gin.H{"ready": n != nil && n.store != nil, "independent": true, "backend": "CLIProxyAPI", "error": nil, "storageError": nil, "syncedAt": nil}
	if n == nil || n.store == nil {
		status["error"] = "Console storage unavailable"
		return status
	}
	status["inferenceUrl"] = n.store.Origin
	status["receiptRetentionDays"] = n.store.Settings().Retention
	s.accountGateway.mu.RLock()
	status["storageError"] = s.accountGateway.storageError
	s.accountGateway.mu.RUnlock()
	return status
}
func (s *Server) consoleSelect(id, model string) error {
	p, err := s.console.store.Profile(id)
	if err != nil {
		return err
	}
	if strings.HasPrefix(p.AuthFile, "claude-") {
		a, err := s.gatewayAuth(p.AuthFile)
		if err != nil {
			return consolecore.Fail(409, err.Error())
		}
		allowed := false
		for _, m := range s.console.store.Settings().Models {
			if m.ID == model {
				allowed = true
			}
		}
		if !allowed {
			return consolecore.Fail(400, "Choose an allowed official 1M Claude model")
		}
		if !registry.GetGlobalRegistry().ClientSupportsModel(a.ID, gatewayUpstreamModel(a, model)) {
			return consolecore.Fail(409, "Selected account does not serve this model")
		}
		return nil
	}
	// Non-Claude legacy clients require a unique prefixed model route.
	if p.Prefix == "" || !strings.HasPrefix(model, p.Prefix+"/") {
		return consolecore.Fail(409, "Choose a unique prefixed model")
	}
	found := false
	for _, a := range s.handlers.AuthManager.List() {
		if registry.GetGlobalRegistry().ClientSupportsModel(a.ID, model) {
			if a.FileName != p.AuthFile && filepath.Base(a.FileName) != p.AuthFile && a.ID != p.AuthFile {
				return consolecore.Fail(409, "Model route is shared by another account")
			}
			found = !a.Disabled
		}
	}
	if !found {
		return consolecore.Fail(409, "Selected account does not serve this model")
	}
	return nil
}
func (s *Server) handleConsole(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !consoleLocal(c) {
		c.JSON(403, gin.H{"error": "Local client administration requires localhost"})
		return
	}
	if s.console == nil || s.console.store == nil {
		c.JSON(503, gin.H{"error": "Native console storage unavailable; inspect proxy logs"})
		return
	}
	var result any
	var err error
	method := c.Request.Method
	route := strings.Trim(c.Param("path"), "/")
	body := consolecore.Object{}
	if method != "GET" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
		if c.Request.ContentLength != 0 {
			if err = c.ShouldBindJSON(&body); err != nil || body == nil {
				c.JSON(400, gin.H{"error": "Expected a JSON object under 1 MB"})
				return
			}
		}
		s.console.mutations.Lock()
		defer s.console.mutations.Unlock()
	}
	store := s.console.store
	cfg := store.Settings()
	call := func(method, path string, body any) (consolecore.Object, error) {
		return s.consoleCall(c, method, path, body)
	}
	switch {
	case route == "settings" && method == "GET":
		result, err = store.PublicSettings()
	case route == "settings" && method == "PUT":
		err = store.UpdateSettings(body)
		if err == nil {
			result, err = store.PublicSettings()
		}
	case route == "health" && method == "GET":
		result = gin.H{"ok": true, "management": "ok", "proxyUrl": store.Origin, "proxyVersion": buildinfo.Version, "hasManagementKey": true}
	case route == "gateway" && method == "GET", route == "gateway/sync" && method == "POST":
		result = s.consoleStatus()
	case route == "requests" && method == "GET":
		var rows []json.RawMessage
		rows, err = store.Receipts(c.Query("profile"))
		result = gin.H{"receipts": rows, "gateway": s.consoleStatus(), "profiles": store.Profiles()}
	case route == "profiles" && method == "GET":
		result = gin.H{"profiles": store.Profiles()}
	case route == "profiles" && method == "POST":
		p := consolecore.Profile{ID: uuid.NewString(), Name: consolecore.Text(body["name"]), AuthFile: consolecore.Text(body["authFile"]), Color: consolecore.Text(body["color"]), Prefix: consolecore.Text(body["prefix"]), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if p.Color == "" {
			p.Color = "#6366f1"
		}
		err = store.AddProfile(p)
		result = p
	case strings.HasPrefix(route, "profiles/"):
		result, err = s.consoleProfile(c, strings.TrimPrefix(route, "profiles/"), method, body, call)
	case route == "routing" && (method == "GET" || method == "PUT"):
		result, err = s.consoleRouting(call, method, body)
	case route == "desktop" && method == "GET":
		var out consolecore.Object
		out, err = store.Desktop()
		var ce *consolecore.Error
		if errors.As(err, &ce) && ce.Status == 409 {
			err = nil
			out = consolecore.Object{"profile": "", "autoMode": false, "models": []any{}, "defaultEffort": "", "alwaysDefault": true, "gatewayUrl": "", "setupRequired": true}
		}
		if err == nil {
			catalog := []gin.H{}
			for _, p := range store.Profiles() {
				models, e := call("GET", "auth-files/models?name="+url.QueryEscape(p.AuthFile), nil)
				if e != nil {
					err = e
					break
				}
				ids := []string{}
				if rows, ok := models["models"].([]any); ok {
					for _, r := range rows {
						ids = append(ids, consolecore.Text(consolecore.Map(r)["id"]))
					}
				}
				catalog = append(catalog, gin.H{"profile": p, "models": ids})
			}
			main := cfg.Models[0].ID
			if rows, ok := out["models"].([]any); ok && len(rows) > 0 {
				if m := consolecore.Text(consolecore.Map(rows[0])["name"]); m != "" {
					main = m
				}
			}
			for k, v := range (consolecore.Object{"roleSettingsMatch": store.RoleMatch(main), "consoleUrl": store.Origin, "inferenceUrl": store.Origin, "gateway": s.consoleStatus(), "claudeBackgroundModel": cfg.Background, "claudeSubagentModel": cfg.Subagent, "claudeConfigDir": cfg.ClaudeDir, "allowedModels": cfg.Models, "cliEffort": cfg.Effort, "cliProfile": cfg.CLIProfile, "catalog": catalog}) {
				out[k] = v
			}
			result = out
		}
	case route == "cli-command" && method == "POST":
		id, model, effort := consolecore.Text(body["profile"]), consolecore.Text(body["model"]), consolecore.Text(body["effort"])
		err = s.consoleSelect(id, model)
		if err == nil {
			var command string
			command, err = store.Command(id, model, effort)
			result = gin.H{"command": command}
		}
	case route == "desktop" && method == "PUT":
		result, err = store.ApplyDesktop(body, s.consoleSelect)
	case (route == "wire" || route == "wire/preview") && method == "POST":
		result, err = store.Wire(body, route == "wire", s.consoleSelect)
	case route == "wire/snippet" && method == "GET":
		id, model := c.Query("profile"), c.Query("model")
		err = s.consoleSelect(id, model)
		if err == nil {
			var snippet string
			snippet, err = store.Snippet(id, model)
			result = gin.H{"snippet": snippet}
		}
	case route == "targets" && method == "GET":
		var recents consolecore.Object
		recents, err = store.Object("recents")
		items := []gin.H{}
		if paths, ok := recents["paths"].([]any); ok {
			for _, p := range paths {
				items = append(items, gin.H{"path": p, "display": p})
			}
		}
		info, e := os.Stat(cfg.ClaudeDir)
		result = gin.H{"globals": []gin.H{{"path": cfg.ClaudeDir, "display": cfg.ClaudeDir, "exists": e == nil && info.IsDir()}}, "recents": items}
	case route == "targets/check" && method == "GET":
		var p string
		p, err = store.UserPath(c.Query("path"))
		info, e := os.Stat(p)
		result = gin.H{"path": p, "display": p, "exists": e == nil && info.IsDir()}
	case route == "proxy/models" && method == "GET":
		result = gin.H{"data": registry.GetGlobalRegistry().GetAvailableModels("openai")}
	case route == "deployment" && (method == "GET" || method == "PUT"):
		if method == "PUT" {
			err = consolecore.SaveDeployment(s.configFilePath, s.console.deployment, body)
		}
		if err == nil {
			var next consolecore.Deployment
			next, err = consolecore.ReadDeployment(s.configFilePath)
			result = gin.H{"file": s.console.file, "active": s.console.deployment, "next": next, "restartRequired": next != s.console.deployment, "configFile": s.configFilePath, "database": store.File}
		}
	case route == "service-settings" && (method == "GET" || method == "PUT"):
		result, err = store.ServiceSettings(body, method == "PUT")
	default:
		err = consolecore.Fail(404, "Unknown console endpoint")
	}
	if err != nil {
		status := 500
		var ce *consolecore.Error
		if errors.As(err, &ce) {
			status = ce.Status
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, result)
}
func (s *Server) consoleProfile(c *gin.Context, route, method string, body consolecore.Object, call consolecore.ManagementCall) (any, error) {
	parts := strings.Split(route, "/")
	store := s.console.store
	p, err := store.Profile(parts[0])
	if err != nil {
		return nil, err
	}
	if len(parts) == 1 {
		switch method {
		case "GET":
			return p, nil
		case "PUT", "PATCH":
			return store.ChangeProfile(p.ID, body, false)
		case "DELETE":
			return store.ChangeProfile(p.ID, nil, true)
		}
	}
	if len(parts) == 2 && parts[1] == "usage" {
		key := "usage:" + p.AuthFile
		if method == "GET" {
			u, err := store.Object(key)
			if len(u) == 0 {
				return gin.H{"usage": nil}, err
			}
			return gin.H{"usage": u}, err
		}
		if method == "POST" {
			usage, err := consolecore.FetchUsage(call, p.AuthFile)
			if err != nil {
				return nil, err
			}
			if err = store.PutObject(key, usage); err != nil {
				return nil, err
			}
			return gin.H{"usage": usage}, nil
		}
	}
	if len(parts) == 2 && parts[1] == "prefix" && method == "PUT" {
		prefix, ok := body["prefix"].(string)
		if !ok || !consolecore.Prefix.MatchString(prefix) {
			return nil, consolecore.Fail(400, "Invalid account prefix")
		}
		if prefix != "" {
			for _, a := range s.handlers.AuthManager.List() {
				if a.Prefix == prefix && a.FileName != p.AuthFile && filepath.Base(a.FileName) != p.AuthFile && a.ID != p.AuthFile {
					return nil, consolecore.Fail(409, "Another account already uses this prefix")
				}
			}
		}
		if _, err = call("PATCH", "auth-files/fields", gin.H{"name": p.AuthFile, "prefix": prefix}); err != nil {
			return nil, err
		}
		return store.ChangeProfile(p.ID, consolecore.Object{"lastKnownPrefix": prefix}, false)
	}
	return nil, consolecore.Fail(404, "Unknown subscription operation")
}
func (s *Server) consoleRouting(call consolecore.ManagementCall, method string, body consolecore.Object) (any, error) {
	fields := map[string]string{"strategy": "routing/strategy", "forceModelPrefix": "force-model-prefix", "requestRetry": "request-retry", "maxRetryInterval": "max-retry-interval", "switchProject": "quota-exceeded/switch-project", "switchPreviewModel": "quota-exceeded/switch-preview-model"}
	if method == "PUT" {
		for k, v := range body {
			if fields[k] == "" {
				return nil, consolecore.Fail(400, "Unknown routing setting")
			}
			if k == "strategy" {
				if v != "fill-first" && v != "round-robin" && v != "weighted-round-robin" {
					return nil, consolecore.Fail(400, "Invalid routing strategy")
				}
			} else if k == "requestRetry" || k == "maxRetryInterval" {
				n, ok := v.(float64)
				if !ok || n < 0 || n > 3600 || n != float64(int(n)) {
					return nil, consolecore.Fail(400, "Invalid retry setting")
				}
			} else {
				if _, ok := v.(bool); !ok {
					return nil, consolecore.Fail(400, "Expected boolean")
				}
			}
		}
		applied := []string{}
		for k, v := range body {
			if _, err := call("PUT", fields[k], gin.H{"value": v}); err != nil {
				return nil, consolecore.Fail(502, "Could not save "+k+"; already saved: "+strings.Join(applied, ", "))
			}
			applied = append(applied, k)
		}
	}
	cfg, err := call("GET", "config", nil)
	if err != nil {
		return nil, err
	}
	routing, quota := consolecore.Map(cfg["routing"]), consolecore.Map(cfg["quota-exceeded"])
	return gin.H{"strategy": routing["strategy"], "sessionAffinity": routing["session-affinity"] == true, "sessionAffinityTtl": routing["session-affinity-ttl"], "sessionAffinitySubagents": routing["session-affinity-subagents"] != false, "forceModelPrefix": cfg["force-model-prefix"] == true, "requestRetry": cfg["request-retry"], "maxRetryInterval": cfg["max-retry-interval"], "switchProject": quota["switch-project"] == true, "switchPreviewModel": quota["switch-preview-model"] == true}, nil
}
