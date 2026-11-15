package executor

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

var (
	xaiDataTag  = []byte("data:")
	xaiEventTag = []byte("event:")
)

const (
	xaiImageHandlerType        = "openai-image"
	xaiVideoHandlerType        = "openai-video"
	xaiSpeechHandlerType       = "openai-speech"
	xaiCustomToolType          = "custom"
	xaiFunctionToolType        = "function"
	xaiImageGenerationToolType = "image_generation"
	xaiNamespaceToolType       = "namespace"
	xaiToolSearchType          = "tool_search"
	xaiWebSearchToolType       = "web_search"
	xaiClientWebSearchAlias    = "clientfn_web_search"
	xaiXSearchToolType         = "x_search"
	xaiMaxTools                = 200



	xaiCodexAppNamespaceName    = "codex_app"
	xaiAutomationUpdateToolName = "automation_update"

	xaiSafeFunctionParameters   = `{"type":"object","properties":{},"additionalProperties":true}`
	xaiImagesGenerationsPath    = "/images/generations"
	xaiImagesEditsPath          = "/images/edits"
	xaiDefaultImageEndpointPath = xaiImagesGenerationsPath
	xaiVideosGenerationsPath    = "/videos/generations"
	xaiVideosEditsPath          = "/videos/edits"
	xaiVideosExtensionsPath     = "/videos/extensions"
	xaiVideosPath               = "/videos"
	xaiTTSPath                  = "/tts"
	xaiIdempotencyKeyMetaKey    = "idempotency_key"
	xaiComposerModelPrefix      = "grok-composer-"
	xaiTokenAuthHeader          = "X-XAI-Token-Auth"
	xaiTokenAuthValue           = "xai-grok-cli"
	xaiClientVersionHeader      = "x-grok-client-version"




	xaiClientVersionFallback      = helps.DefaultXAIFallbackClientVersion
	xaiClientVersionValue         = xaiClientVersionFallback
	xaiClientIdentifierHeader     = "x-grok-client-identifier"
	xaiClientIdentifierValue      = "grok-shell"
	xaiAuthenticateResponseHeader = "x-authenticateresponse"
	xaiAuthenticateResponseValue  = "authenticate-response"

	xaiUsingAPIAttr = "using_api"
)



var xaiXSearchToolJSON = []byte(`{"type":"x_search"}`)


type XAIExecutor struct {
	cfg *config.Config
}


func NewXAIExecutor(cfg *config.Config) *XAIExecutor {
	return &XAIExecutor{cfg: cfg}
}



func xaiClientVersion() string {
	return helps.GetXAIClientVersion()
}



func StartXAIVersionUpdater(ctx context.Context, proxyURL string) {
	helps.StartXAIVersionUpdater(ctx, proxyURL)
}


func (e *XAIExecutor) Identifier() string {
	return "xai"
}


func (e *XAIExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	token, _ := xaiCreds(auth)
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Del("Authorization")
	}
	ensureXAIUserAgent(req)
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	return nil
}


func (e *XAIExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("xai executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if errPrepare := e.PrepareRequest(httpReq, auth); errPrepare != nil {
		return nil, errPrepare
	}
	httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}


func (e *XAIExecutor) SupportsApplyPatch() bool { return e != nil }
