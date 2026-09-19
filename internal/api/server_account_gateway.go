package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type accountGatewayModel struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	ContextWindow int    `json:"contextWindow"`
}
type accountGatewayRoute struct {
	ID       string                `json:"id"`
	AuthFile string                `json:"authFile"`
	Models   []accountGatewayModel `json:"models"`
}
type accountGatewayConfig struct {
	Routes []accountGatewayRoute `json:"routes"`
}
type accountReceipt struct {
	ID                  string `json:"id"`
	StartedAt           string `json:"startedAt"`
	Profile             string `json:"profile"`
	SelectedAuth        string `json:"selectedAuth,omitempty"`
	AccountConfirmed    bool   `json:"accountConfirmed"`
	Model               string `json:"model"`
	ResponseModel       string `json:"responseModel,omitempty"`
	Effort              string `json:"effort,omitempty"`
	Session             string `json:"session,omitempty"`
	Agent               string `json:"agent,omitempty"`
	ParentAgent         string `json:"parentAgent,omitempty"`
	Endpoint            string `json:"endpoint"`
	Status              int    `json:"status"`
	DurationMs          int64  `json:"durationMs"`
	InputTokens         *int64 `json:"inputTokens"`
	OutputTokens        *int64 `json:"outputTokens"`
	CacheReadTokens     *int64 `json:"cacheReadTokens"`
	CacheCreationTokens *int64 `json:"cacheCreationTokens"`
	ContextConfigured   int    `json:"contextConfigured"`
	Over200KObserved    bool   `json:"over200kObserved"`
	Completed           bool   `json:"completed"`
	ErrorType           string `json:"errorType,omitempty"`
}
type accountGateway struct {
	nativeRoute    func(string) (accountGatewayRoute, bool)
	nativeManaged  func() bool
	persistReceipt func(accountReceipt) error

	mu           sync.RWMutex
	journalMu    sync.Mutex
	directory    string
	config       accountGatewayConfig
	receipts     []accountReceipt
	storageError string
}

var gatewayRouteID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`)
var gatewayModelID = regexp.MustCompile(`^claude-[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

func newAccountGateway(configFile string) *accountGateway {
	dir := os.Getenv("CLIPROXY_ACCOUNT_GATEWAY_DIR")
	if dir == "" {
		dir = configFile + ".gateway"
	}
	g := &accountGateway{directory: dir, config: accountGatewayConfig{Routes: []accountGatewayRoute{}}}
	data, err := os.ReadFile(filepath.Join(dir, "routes.json"))
	if err == nil {
		if err = json.Unmarshal(data, &g.config); err != nil {
			g.storageError = "Could not read account routes"
			g.config.Routes = nil
		}
	} else if !os.IsNotExist(err) {
		g.storageError = "Could not read account routes"
	}
	for _, name := range []string{"receipts.previous.jsonl", "receipts.jsonl"} {
		file, errOpen := os.Open(filepath.Join(dir, name))
		if errOpen != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 64*1024)
		for scanner.Scan() {
			var r accountReceipt
			if json.Unmarshal(scanner.Bytes(), &r) == nil && r.ID != "" {
				g.receipts = append(g.receipts, r)
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			g.storageError = "Receipt journal could not be fully read"
		}
		if errClose := file.Close(); errClose != nil {
			log.Warn("account gateway journal close failed")
		}
	}
	if len(g.receipts) > 5000 {
		g.receipts = g.receipts[len(g.receipts)-5000:]
	}
	return g
}
func (g *accountGateway) route(id string) (accountGatewayRoute, bool) {
	if g.nativeRoute != nil && g.nativeManaged != nil && g.nativeManaged() {
		return g.nativeRoute(id)
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, route := range g.config.Routes {
		if route.ID == id {
			return route, true
		}
	}
	return accountGatewayRoute{}, false
}
func (g *accountGateway) save(cfg accountGatewayConfig) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := os.MkdirAll(g.directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(g.directory, "routes-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), filepath.Join(g.directory, "routes.json")); err != nil {
		return err
	}
	g.config = cfg
	return nil
}
func (g *accountGateway) record(r accountReceipt) {
	if g.persistReceipt != nil {
		if err := g.persistReceipt(r); err != nil {
			g.mu.Lock()
			g.storageError = "Request history could not be saved"
			g.mu.Unlock()
			log.WithError(err).Warn("Native console receipt save failed")
		}
	}
	g.mu.Lock()
	g.receipts = append(g.receipts, r)
	if len(g.receipts) > 5000 {
		g.receipts = g.receipts[len(g.receipts)-5000:]
	}
	g.mu.Unlock()
	if g.persistReceipt != nil {
		return
	}
	g.journalMu.Lock()
	defer g.journalMu.Unlock()
	if err := g.appendReceipt(r); err != nil {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.storageError = "Request journal could not be saved; check proxy storage permissions and free space"
		log.Warn(g.storageError)
	}
}
func (g *accountGateway) appendReceipt(r accountReceipt) error {
	if err := os.MkdirAll(g.directory, 0700); err != nil {
		return err
	}
	name := filepath.Join(g.directory, "receipts.jsonl")
	if info, err := os.Stat(name); err == nil && info.Size() > 8*1024*1024 {
		if err = os.Rename(name, filepath.Join(g.directory, "receipts.previous.jsonl")); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func (s *Server) getAccountGateway(c *gin.Context) {
	if s.console != nil && s.console.store != nil && s.console.store.Managed() {
		routes := []accountGatewayRoute{}
		for _, p := range s.console.store.Profiles() {
			if route, ok := s.accountGateway.route(p.ID); ok {
				routes = append(routes, route)
			}
		}
		c.JSON(200, gin.H{"version": 1, "routes": routes, "owner": "console", "directory": s.console.store.Directory})
		return
	}

	g := s.accountGateway
	g.mu.RLock()
	defer g.mu.RUnlock()
	c.JSON(200, gin.H{"version": 1, "routes": g.config.Routes, "storageError": g.storageError, "directory": g.directory, "receiptCapacity": 5000})
}
func (s *Server) putAccountGateway(c *gin.Context) {
	if s.accountGateway.nativeManaged != nil && s.accountGateway.nativeManaged() {
		c.JSON(409, gin.H{"error": "Routes are owned by console profiles and model settings. Update those through the native management API."})
		return
	}
	var cfg accountGatewayConfig
	if err := c.ShouldBindJSON(&cfg); err != nil || len(cfg.Routes) > 100 {
		c.JSON(400, gin.H{"error": "Invalid account routes"})
		return
	}
	ids := map[string]bool{}
	for _, route := range cfg.Routes {
		if !gatewayRouteID.MatchString(route.ID) || ids[route.ID] || route.AuthFile == "" || len(route.Models) == 0 || len(route.Models) > 50 {
			c.JSON(400, gin.H{"error": "Routes need unique IDs, a Claude account and 1–50 models"})
			return
		}
		ids[route.ID] = true
		auth, err := s.gatewayAuth(route.AuthFile)
		if err != nil {
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		models := map[string]bool{}
		for _, model := range route.Models {
			if !gatewayModelID.MatchString(model.ID) || models[model.ID] || model.ContextWindow != 1000000 || len(model.Label) > 100 {
				c.JSON(400, gin.H{"error": "Use unique official 1M Claude models"})
				return
			}
			models[model.ID] = true
			if !registry.GetGlobalRegistry().ClientSupportsModel(auth.ID, gatewayUpstreamModel(auth, model.ID)) {
				c.JSON(409, gin.H{"error": "A configured model is not registered on the selected account"})
				return
			}
		}
	}
	if err := s.accountGateway.save(cfg); err != nil {
		c.JSON(500, gin.H{"error": "Could not save account routes"})
		return
	}
	s.getAccountGateway(c)
}
func (s *Server) getAccountReceipts(c *gin.Context) {
	if s.console != nil && s.console.store != nil {
		rows, err := s.console.store.RecentReceipts("", 5000)
		if err != nil {
			c.JSON(500, gin.H{"error": "Request history could not be read"})
			return
		}
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
		c.JSON(200, gin.H{"receipts": rows, "storageError": s.consoleStatus()["storageError"], "capacity": 5000})
		return
	}

	g := s.accountGateway
	g.mu.RLock()
	defer g.mu.RUnlock()
	rows := make([]accountReceipt, len(g.receipts))
	copy(rows, g.receipts)
	c.JSON(200, gin.H{"receipts": rows, "storageError": g.storageError, "capacity": 5000})
}
func (s *Server) gatewayAuth(file string) (*coreauth.Auth, error) {
	var selected *coreauth.Auth
	for _, auth := range s.handlers.AuthManager.List() {
		if auth.FileName == file || filepath.Base(auth.FileName) == file || auth.ID == file {
			if selected != nil {
				return nil, fmt.Errorf("Account reference is ambiguous")
			}
			selected = auth
		}
	}
	if selected == nil || selected.Provider != "claude" {
		return nil, fmt.Errorf("Selected Claude account is missing")
	}
	if selected.Disabled || selected.Status == coreauth.StatusDisabled {
		return nil, fmt.Errorf("Selected Claude account is disabled")
	}
	return selected, nil
}
func gatewayUpstreamModel(auth *coreauth.Auth, model string) string {
	if auth.Prefix != "" {
		return auth.Prefix + "/" + model
	}
	return model
}
func (s *Server) registerAccountGateway(claudeHandler *claude.ClaudeCodeAPIHandler) {
	s.accountGateway = newAccountGateway(s.configFilePath)
	group := s.engine.Group("/inference/:profile")
	group.Use(AuthMiddleware(s.accessManager))
	group.GET("/v1/models", func(c *gin.Context) {
		route, ok := s.accountGateway.route(c.Param("profile"))
		if !ok {
			c.JSON(404, gin.H{"error": gin.H{"type": "not_found_error", "message": "Account route is not configured"}})
			return
		}
		auth, err := s.gatewayAuth(route.AuthFile)
		if err != nil {
			c.JSON(409, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
			return
		}
		data := []gin.H{}
		for _, model := range route.Models {
			if registry.GetGlobalRegistry().ClientSupportsModel(auth.ID, gatewayUpstreamModel(auth, model.ID)) {
				data = append(data, gin.H{"id": model.ID, "type": "model", "display_name": model.Label + " · 1M", "supports_1m": true, "max_input_tokens": model.ContextWindow})
			}
		}
		c.JSON(200, gin.H{"data": data, "has_more": false})
	})
	group.POST("/v1/messages", s.accountRequest(claudeHandler.ClaudeMessages))
	group.POST("/v1/messages/count_tokens", s.accountRequest(claudeHandler.ClaudeCountTokens))
	group.HEAD("/api/hello", func(c *gin.Context) { c.Status(200) })
}
func (s *Server) accountRequest(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		r := accountReceipt{ID: uuid.NewString(), StartedAt: started.UTC().Format(time.RFC3339Nano), Profile: c.Param("profile"), Endpoint: c.Request.URL.Path, Session: boundedMetadata(c.GetHeader("X-Claude-Code-Session-Id")), Agent: boundedMetadata(c.GetHeader("X-Claude-Code-Agent-Id")), ParentAgent: boundedMetadata(c.GetHeader("X-Claude-Code-Parent-Agent-Id"))}
		writer := &receiptWriter{ResponseWriter: c.Writer, receipt: &r}
		c.Writer = writer
		var selectedMu sync.Mutex
		defer func() {
			r.Status = c.Writer.Status()
			writer.finish()
			selectedMu.Lock()
			defer selectedMu.Unlock()
			r.Status = c.Writer.Status()
			r.DurationMs = time.Since(started).Milliseconds()
			if c.Request.Context().Err() != nil {
				r.Completed = false
				r.ErrorType = "client_disconnected"
			}
			s.accountGateway.record(r)
		}()
		reject := func(status int, message string) {
			r.ErrorType = "gateway_validation"
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
		}
		route, ok := s.accountGateway.route(r.Profile)
		if !ok {
			reject(404, "Account route is not configured")
			return
		}
		auth, err := s.gatewayAuth(route.AuthFile)
		if err != nil {
			reject(409, err.Error())
			return
		}
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024*1024+1))
		if err != nil || len(raw) > 64*1024*1024 || !gjson.ValidBytes(raw) {
			reject(400, "Invalid request JSON or request exceeds 64 MB")
			return
		}
		model := strings.TrimSuffix(gjson.GetBytes(raw, "model").String(), "[1m]")
		r.Model = boundedMetadata(model)
		r.Effort = boundedMetadata(gjson.GetBytes(raw, "output_config.effort").String())
		var allowed bool
		for _, option := range route.Models {
			if option.ID == model {
				allowed = true
				r.ContextConfigured = option.ContextWindow
				break
			}
		}
		if !allowed {
			reject(400, "Choose a configured official 1M model")
			return
		}
		upstreamModel := gatewayUpstreamModel(auth, model)
		if !registry.GetGlobalRegistry().ClientSupportsModel(auth.ID, upstreamModel) {
			reject(409, "Selected account does not serve this model")
			return
		}
		raw, err = sjson.SetBytes(raw, "model", upstreamModel)
		if err != nil {
			reject(400, "Invalid model field")
			return
		}
		ctx := handlers.WithGatewayResponseHeaders(c.Request.Context())
		ctx = handlers.WithPinnedAuthID(ctx, auth.ID)
		ctx = handlers.WithSelectedAuthIDCallback(ctx, func(id string) {
			selectedMu.Lock()
			defer selectedMu.Unlock()
			r.SelectedAuth = id
			r.AccountConfirmed = id == auth.ID
		})
		c.Request = c.Request.WithContext(ctx)
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		c.Request.ContentLength = int64(len(raw))
		// Preserve caller capability values, adding only the configured context capability.
		if !strings.Contains(c.GetHeader("Anthropic-Beta"), "context-1m-2025-08-07") {
			beta := c.GetHeader("Anthropic-Beta")
			if beta != "" {
				beta += ","
			}
			c.Request.Header.Set("Anthropic-Beta", beta+"context-1m-2025-08-07")
		}
		c.Header("X-CLIProxy-Subscription", route.ID)
		c.Header("X-CLIProxy-Receipt", r.ID)
		next(c)
	}
}
func boundedMetadata(value string) string {
	if len(value) > 200 {
		return value[:200]
	}
	return value
}

// Inspect metadata while writing every byte straight through, including SSE pings.
// Neither response content nor prompts are persisted.
type receiptWriter struct {
	gin.ResponseWriter
	receipt  *accountReceipt
	pending  []byte
	overflow bool
}

func (w *receiptWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.observe(data[:n])
	return n, err
}
func (w *receiptWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *receiptWriter) observe(data []byte) {
	if !strings.Contains(w.Header().Get("Content-Type"), "text/event-stream") {
		if !w.overflow && len(w.pending)+len(data) <= 8*1024*1024 {
			w.pending = append(w.pending, data...)
		} else {
			w.overflow = true
			w.pending = nil
		}
		return
	}
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			if !w.overflow && len(w.pending)+len(data) < 2*1024*1024 {
				w.pending = append(w.pending, data...)
			} else {
				w.overflow = true
				w.pending = nil
			}
			return
		}
		if !w.overflow && len(w.pending)+end < 2*1024*1024 {
			w.pending = append(w.pending, data[:end]...)
			line := bytes.TrimSpace(w.pending)
			if bytes.HasPrefix(line, []byte("data:")) {
				w.parse(bytes.TrimSpace(line[5:]))
			}
		}
		w.pending = nil
		w.overflow = false
		data = data[end+1:]
	}
}
func (w *receiptWriter) finish() {
	if !w.overflow && len(w.pending) > 0 {
		line := bytes.TrimSpace(w.pending)
		if bytes.HasPrefix(line, []byte("data:")) {
			line = bytes.TrimSpace(line[5:])
		}
		w.parse(line)
	}
	w.pending = nil
	if w.receipt.Status >= 400 {
		w.receipt.Completed = false
	}
	total := int64(0)
	for _, v := range []*int64{w.receipt.InputTokens, w.receipt.CacheReadTokens, w.receipt.CacheCreationTokens} {
		if v != nil {
			total += *v
		}
	}
	w.receipt.Over200KObserved = w.receipt.Completed && total > 200000 && strings.HasSuffix(w.receipt.Endpoint, "/messages")
}
func (w *receiptWriter) parse(data []byte) {
	if !gjson.ValidBytes(data) {
		return
	}
	root := gjson.ParseBytes(data)
	kind := root.Get("type").String()
	if kind == "message_stop" && w.receipt.ErrorType == "" {
		w.receipt.Completed = true
	}
	if kind == "message_start" {
		root = root.Get("message")
	}
	if model := root.Get("model").String(); model != "" {
		w.receipt.ResponseModel = boundedMetadata(model)
	}
	if strings.HasSuffix(w.receipt.Endpoint, "/count_tokens") && root.Get("input_tokens").Type == gjson.Number {
		n := root.Get("input_tokens").Int()
		w.receipt.InputTokens = &n
		w.receipt.Completed = true
	}
	usage := root.Get("usage")
	if usage.Exists() {
		for key, dest := range map[string]**int64{"input_tokens": &w.receipt.InputTokens, "output_tokens": &w.receipt.OutputTokens, "cache_read_input_tokens": &w.receipt.CacheReadTokens, "cache_creation_input_tokens": &w.receipt.CacheCreationTokens} {
			if v := usage.Get(key); v.Type == gjson.Number {
				n := v.Int()
				*dest = &n
			}
		}
	}
	if kind == "message" && root.Get("stop_reason").String() != "" {
		w.receipt.Completed = true
	}
	if kind == "error" {
		w.receipt.ErrorType = boundedMetadata(root.Get("error.type").String())
		w.receipt.Completed = false
	}
}
