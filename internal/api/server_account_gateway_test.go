package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

type gatewayTestExecutor struct {
	calls   []string
	payload []byte
	headers http.Header
	fail    bool
}

func (e *gatewayTestExecutor) Identifier() string { return "claude" }
func (e *gatewayTestExecutor) capture(a *coreauth.Auth, r coreexecutor.Request, o coreexecutor.Options) error {
	e.calls = append(e.calls, a.ID)
	e.payload = append([]byte(nil), r.Payload...)
	e.headers = o.Headers.Clone()
	if e.fail {
		return gatewayRateError{}
	}
	return nil
}
func (e *gatewayTestExecutor) Execute(_ context.Context, a *coreauth.Auth, r coreexecutor.Request, o coreexecutor.Options) (coreexecutor.Response, error) {
	if err := e.capture(a, r, o); err != nil {
		return coreexecutor.Response{}, err
	}
	return coreexecutor.Response{Headers: http.Header{"X-Should-Retry": {"false"}, "Anthropic-Ratelimit-Unified-Status": {"allowed"}, "Set-Cookie": {"secret=bad"}}, Payload: []byte(`{"type":"message","model":"claude-opus-5","content":[{"type":"text","text":"private response"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":7,"cache_read_input_tokens":210000,"cache_creation_input_tokens":30}}`)}, nil
}
func (e *gatewayTestExecutor) ExecuteStream(_ context.Context, a *coreauth.Auth, r coreexecutor.Request, o coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	if err := e.capture(a, r, o); err != nil {
		return nil, err
	}
	ch := make(chan coreexecutor.StreamChunk, 4)
	for _, s := range []string{"event: ping\ndata: {\"type\":\"ping\"}\n\n", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-5\",\"usage\":{\"input_tokens\":42,\"cache_read_input_tokens\":240000}}}\n\n", "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n", "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"} {
		ch <- coreexecutor.StreamChunk{Payload: []byte(s)}
	}
	close(ch)
	return &coreexecutor.StreamResult{Headers: http.Header{"X-Should-Retry": {"false"}}, Chunks: ch}, nil
}
func (e *gatewayTestExecutor) CountTokens(ctx context.Context, a *coreauth.Auth, r coreexecutor.Request, o coreexecutor.Options) (coreexecutor.Response, error) {
	if err := e.capture(a, r, o); err != nil {
		return coreexecutor.Response{}, err
	}
	return coreexecutor.Response{Payload: []byte(`{"input_tokens":200001}`)}, nil
}
func (e *gatewayTestExecutor) Refresh(_ context.Context, a *coreauth.Auth) (*coreauth.Auth, error) {
	return a, nil
}
func (e *gatewayTestExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func gatewayTestServer(t *testing.T) (*Server, *gatewayTestExecutor) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("CLIPROXY_ACCOUNT_GATEWAY_DIR", filepath.Join(t.TempDir(), "gateway"))
	e := &gatewayTestExecutor{}
	m := coreauth.NewManager(nil, nil, nil)
	m.RegisterExecutor(e)
	for _, id := range []string{"gateway-selected", "gateway-other"} {
		a := &coreauth.Auth{ID: id, FileName: id + ".json", Provider: "claude", Status: coreauth.StatusActive, Prefix: id}
		if _, err := m.Register(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "claude", []*registry.ModelInfo{{ID: id + "/claude-opus-5"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	s := &Server{engine: gin.New(), handlers: handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, m)}
	s.registerAccountGateway(claude.NewClaudeCodeAPIHandler(s.handlers))
	s.engine.PUT("/gateway", s.putAccountGateway)
	r := httptest.NewRecorder()
	s.engine.ServeHTTP(r, httptest.NewRequest("PUT", "/gateway", strings.NewReader(`{"routes":[{"id":"profile-a","authFile":"gateway-selected.json","models":[{"id":"claude-opus-5","label":"Opus","contextWindow":1000000}]}]}`)))
	if r.Code != 200 {
		t.Fatalf("config %d: %s", r.Code, r.Body)
	}
	return s, e
}
func gatewayRequest(s *Server, endpoint, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/inference/profile-a/v1/"+endpoint, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Claude-Code-Session-Id", "session-a")
	req.Header.Set("X-Claude-Code-Agent-Id", "agent-a")
	req.Header.Set("Anthropic-Beta", "future-capability")
	r := httptest.NewRecorder()
	s.engine.ServeHTTP(r, req)
	return r
}
func TestAccountGatewayPinnedAndPersistent(t *testing.T) {
	s, e := gatewayTestServer(t)
	r := gatewayRequest(s, "messages", `{"model":"claude-opus-5[1m]","max_tokens":10,"output_config":{"effort":"high"},"messages":[{"role":"user","content":"private prompt"}],"unknown_future_field":{"cache_control":{"type":"ephemeral"}}}`)
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if len(e.calls) != 1 || e.calls[0] != "gateway-selected" {
		t.Fatalf("wrong auth: %v", e.calls)
	}
	if !gjson.GetBytes(e.payload, "unknown_future_field.cache_control").Exists() {
		t.Fatal("dropped body fields")
	}
	if e.headers.Get("X-Claude-Code-Agent-Id") != "agent-a" || !strings.Contains(e.headers.Get("Anthropic-Beta"), "future-capability") {
		t.Fatalf("headers lost: %v", e.headers)
	}
	if r.Header().Get("X-Should-Retry") != "false" || r.Header().Get("Set-Cookie") != "" {
		t.Fatal("response header contract")
	}
	rows := s.accountGateway.receipts
	if len(rows) != 1 || !rows[0].AccountConfirmed || !rows[0].Completed || !rows[0].Over200KObserved || rows[0].Effort != "high" || rows[0].OutputTokens == nil || *rows[0].OutputTokens != 7 {
		t.Fatalf("bad receipt: %+v", rows)
	}
	data, err := os.ReadFile(filepath.Join(s.accountGateway.directory, "receipts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") {
		t.Fatal("content persisted")
	}
	loaded := newAccountGateway("")
	if len(loaded.receipts) != 1 || len(loaded.config.Routes) != 1 {
		t.Fatal("restart lost routes or receipts")
	}
}
func TestAccountGatewayNoFallback(t *testing.T) {
	s, e := gatewayTestServer(t)
	e.fail = true
	r := gatewayRequest(s, "messages", `{"model":"claude-opus-5","messages":[]}`)
	if r.Code != 429 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	for _, id := range e.calls {
		if id != "gateway-selected" {
			t.Fatalf("fallback to %s", id)
		}
	}
	if len(e.calls) != 1 {
		t.Fatalf("unexpected retries %d", len(e.calls))
	}
	if r.Header().Get("X-Should-Retry") != "false" {
		t.Fatalf("error retry header lost: %v", r.Header())
	}
	if s.accountGateway.receipts[0].Completed {
		t.Fatal("error marked complete")
	}
}
func TestAccountGatewayStreamingAndCount(t *testing.T) {
	s, _ := gatewayTestServer(t)
	r := gatewayRequest(s, "messages", `{"model":"claude-opus-5","stream":true,"messages":[]}`)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "event: ping") {
		t.Fatalf("stream lost: %s", r.Body)
	}
	row := s.accountGateway.receipts[0]
	if !row.AccountConfirmed || !row.Completed || !row.Over200KObserved || row.OutputTokens == nil || *row.OutputTokens != 13 {
		t.Fatalf("stream receipt %+v", row)
	}
	r = gatewayRequest(s, "messages/count_tokens", `{"model":"claude-opus-5","messages":[]}`)
	if r.Code != 200 {
		t.Fatalf("count %d %s", r.Code, r.Body)
	}
	row = s.accountGateway.receipts[1]
	if !row.AccountConfirmed || row.Over200KObserved {
		t.Fatalf("count is not proof of long generation %+v", row)
	}
}
func TestAccountGatewayRejectsUnconfiguredModel(t *testing.T) {
	s, e := gatewayTestServer(t)
	r := gatewayRequest(s, "messages", `{"model":"claude-haiku-4-5","messages":[]}`)
	if r.Code != 400 || len(e.calls) != 0 {
		t.Fatalf("unsupported model: %d %v", r.Code, e.calls)
	}
}
func TestAccountGatewayFragmentedReceipt(t *testing.T) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Header("Content-Type", "text/event-stream")
	row := accountReceipt{Endpoint: "/v1/messages", Status: 200}
	w := receiptWriter{ResponseWriter: c.Writer, receipt: &row}
	for _, b := range []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":123}}}\r\n\r\ndata: {\"type\":\"message_stop\"}\n\n") {
		_, _ = w.Write([]byte{b})
	}
	w.finish()
	if !row.Completed || row.InputTokens == nil || *row.InputTokens != 123 || row.Over200KObserved {
		t.Fatalf("fragmented %+v", row)
	}
}

type gatewayRateError struct{}

func (gatewayRateError) Error() string {
	return `{"type":"error","error":{"type":"rate_limit_error","message":"limited"}}`
}
func (gatewayRateError) StatusCode() int { return 429 }
func (gatewayRateError) Headers() http.Header {
	return http.Header{"X-Should-Retry": {"false"}, "Retry-After": {"60"}}
}

func TestAccountGatewayPinSurvivesSharedModel(t *testing.T) {
	s, e := gatewayTestServer(t)
	for _, a := range s.handlers.AuthManager.List() {
		a.Prefix = "shared"
		if _, err := s.handlers.AuthManager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(a.ID, "claude", []*registry.ModelInfo{{ID: "shared/claude-opus-5"}})
	}
	for i := 0; i < 2; i++ {
		r := gatewayRequest(s, "messages", `{"model":"claude-opus-5","messages":[]}`)
		if r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body)
		}
	}
	for _, id := range e.calls {
		if id != "gateway-selected" {
			t.Fatalf("account pin lost: %v", e.calls)
		}
	}
	var a *coreauth.Auth
	// Disable the selected account, regardless of list order.
	for _, item := range s.handlers.AuthManager.List() {
		if item.ID == "gateway-selected" {
			a = item
		}
	}
	a.Disabled = true
	a.Status = coreauth.StatusDisabled
	if _, err := s.handlers.AuthManager.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	r := gatewayRequest(s, "messages", `{"model":"claude-opus-5","messages":[]}`)
	if r.Code != 409 || len(e.calls) != 2 {
		t.Fatalf("disabled fallback: %d %v", r.Code, e.calls)
	}
}
