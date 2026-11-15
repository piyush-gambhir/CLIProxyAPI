


package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/cache"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	internalsignature "github.com/router-for-me/CLIProxyAPI/v8/internal/signature"
	antigravityclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/antigravity/claude"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/httptransport"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

const (
	antigravityBaseURLDaily                = "https://daily-cloudcode-pa.googleapis.com"
	antigravitySandboxBaseURLDaily         = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	antigravityBaseURLProd                 = "https://cloudcode-pa.googleapis.com"
	antigravityCountTokensPath             = "/v1internal:countTokens"
	antigravityStreamPath                  = "/v1internal:streamGenerateContent"
	antigravityGeneratePath                = "/v1internal:generateContent"
	antigravityClientID                    = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	antigravityClientSecret                = "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"
	antigravityAuthType                    = "antigravity"
	antigravityRequestTokenSafetyWindow    = 5 * time.Minute
	antigravityCreditsHintRefreshInterval  = cliproxyauth.AntigravityCreditsHintRefreshInterval
	antigravityCreditsHintRefreshTimeout   = 5 * time.Second
	antigravityShortQuotaCooldownThreshold = 5 * time.Minute
	antigravityInstantRetryThreshold       = 3 * time.Second

)


type AntigravityExecutor struct {
	cfg           *config.Config
	matcherMu     sync.RWMutex
	lastWordsKey  string
	cachedMatcher *helps.SensitiveWordMatcher
}








func NewAntigravityExecutor(cfg *config.Config) *AntigravityExecutor {
	return &AntigravityExecutor{cfg: cfg}
}

func (e *AntigravityExecutor) getSensitiveWordMatcher() *helps.SensitiveWordMatcher {
	if e == nil || e.cfg == nil {
		return nil
	}
	words := e.cfg.Antigravity.SensitiveWords
	replacements := e.cfg.Antigravity.SensitiveWordsReplace
	if len(words) == 0 && len(replacements) == 0 {
		return nil
	}
	key := helps.SensitiveWordConfigCacheKey(words, replacements)
	e.matcherMu.RLock()
	if e.lastWordsKey == key {
		m := e.cachedMatcher
		e.matcherMu.RUnlock()
		return m
	}
	e.matcherMu.RUnlock()

	e.matcherMu.Lock()
	defer e.matcherMu.Unlock()
	if e.lastWordsKey == key {
		return e.cachedMatcher
	}
	e.lastWordsKey = key
	e.cachedMatcher = helps.BuildAntigravitySensitiveWordMatcher(words, replacements)
	return e.cachedMatcher
}

func (e *AntigravityExecutor) obfuscateSensitiveWords(payload []byte) []byte {
	matcher := e.getSensitiveWordMatcher()
	if matcher == nil {
		return payload
	}
	return helps.ObfuscateSensitiveWordsInSystemInstruction(payload, matcher)
}

func (e *AntigravityExecutor) obfuscateSensitiveWordsDoc(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	matcher := e.getSensitiveWordMatcher()
	if matcher == nil {
		return false
	}
	return helps.ObfuscateSensitiveWordsInSystemInstructionDoc(doc, matcher)
}





var (
	antigravityBaseTransport = defaultAntigravityBaseTransport()
	antigravityTransports    = helps.NewTransportCache[antigravityTransportKey](antigravityTransportCacheCapacity)
)

const (















	antigravityTransportCacheCapacity = 8192




	antigravityDefaultMaxIdleConnsPerHost = 2



	antigravityMaxAllowedMaxIdleConnsPerHost = 100




	antigravityDefaultIdleConnTimeout = 30 * time.Second




	antigravityMaxAllowedIdleConnTimeout = 210 * time.Second






	antigravityAnonymousTransportScope = "anonymous"
)

type antigravityPoolSettings struct {
	shortMode           bool
	idleConnTimeout     time.Duration
	maxIdleConnsPerHost int
}

func resolveAntigravityPoolSettings(cfg *config.Config) antigravityPoolSettings {


	settings := antigravityPoolSettings{
		shortMode:           true,
		maxIdleConnsPerHost: -1,
		idleConnTimeout:     0,
	}
	if cfg == nil {
		return settings
	}


	if cfg.Antigravity.ConnectionPool.Enabled == nil || !*cfg.Antigravity.ConnectionPool.Enabled {
		return settings
	}


	settings.shortMode = false
	settings.idleConnTimeout = antigravityDefaultIdleConnTimeout
	settings.maxIdleConnsPerHost = antigravityDefaultMaxIdleConnsPerHost

	rawTimeout := strings.TrimSpace(cfg.Antigravity.ConnectionPool.IdleConnTimeout)
	if rawTimeout != "" {
		d, err := time.ParseDuration(rawTimeout)
		if err != nil {
			log.Warnf("antigravity executor: invalid idle-conn-timeout %q: %v, using default %v", rawTimeout, err, antigravityDefaultIdleConnTimeout)
		} else {
			if d <= 0 {
				settings.shortMode = true
				settings.maxIdleConnsPerHost = -1
				return settings
			}
			if d > antigravityMaxAllowedIdleConnTimeout {
				d = antigravityMaxAllowedIdleConnTimeout
			}
			settings.idleConnTimeout = d
		}
	}

	if cfg.Antigravity.ConnectionPool.MaxIdleConnsPerHost != nil {
		val := *cfg.Antigravity.ConnectionPool.MaxIdleConnsPerHost
		if val < 0 {
			settings.shortMode = true
			settings.maxIdleConnsPerHost = -1
			return settings
		}
		if val > antigravityMaxAllowedMaxIdleConnsPerHost {
			val = antigravityMaxAllowedMaxIdleConnsPerHost
		}
		settings.maxIdleConnsPerHost = val
	}

	if settings.maxIdleConnsPerHost < 0 {
		settings.shortMode = true
	}

	return settings
}



func ResetAntigravityTransports() {
	antigravityTransports.Purge()
}


func AntigravityTransportsLen() int {
	return antigravityTransports.Len()
}


func closeAntigravityAuthIdleTransports(auth *cliproxyauth.Auth) {
	if auth == nil {
		return
	}
	scope := antigravityTransportScope(auth)
	if scope == "" || scope == antigravityAnonymousTransportScope {
		return
	}
	antigravityTransports.CloseMatching(func(key antigravityTransportKey) bool {
		return key.credential == scope
	})
}






type antigravityTransportKey struct {
	credential          string
	proxy               string
	base                *http.Transport
	shortMode           bool
	idleConnTimeout     time.Duration
	maxIdleConnsPerHost int
}

func defaultAntigravityBaseTransport() *http.Transport {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		return transport
	}
	return &http.Transport{}
}

func cloneTransportWithHTTP11(base *http.Transport, cfgs ...*config.Config) *http.Transport {
	if base == nil {
		return nil
	}

	clone := base.Clone()
	clone.ForceAttemptHTTP2 = false

	clone.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	}


	clone.TLSClientConfig.NextProtos = nil
	applyAntigravityPoolLimits(clone, cfgs...)
	return clone
}







func applyAntigravityPoolLimits(transport *http.Transport, cfgs ...*config.Config) {
	if transport == nil {
		return
	}
	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	settings := resolveAntigravityPoolSettings(cfg)
	if settings.shortMode {
		transport.MaxIdleConnsPerHost = -1
		transport.DisableKeepAlives = false
		transport.IdleConnTimeout = 0
		return
	}


	if transport.MaxIdleConnsPerHost < 0 {
		return
	}


	if transport.MaxIdleConnsPerHost < settings.maxIdleConnsPerHost {
		transport.MaxIdleConnsPerHost = settings.maxIdleConnsPerHost
	}


	if transport.MaxIdleConns > 0 && transport.MaxIdleConns < transport.MaxIdleConnsPerHost {
		transport.MaxIdleConns = transport.MaxIdleConnsPerHost
	}






	rawTimeout := ""
	if cfg != nil {
		rawTimeout = strings.TrimSpace(cfg.Antigravity.ConnectionPool.IdleConnTimeout)
	}
	if rawTimeout != "" {
		transport.IdleConnTimeout = settings.idleConnTimeout
	} else if transport.IdleConnTimeout == 0 || transport.IdleConnTimeout == 90*time.Second {
		transport.IdleConnTimeout = settings.idleConnTimeout
	} else if transport.IdleConnTimeout > antigravityMaxAllowedIdleConnTimeout {
		transport.IdleConnTimeout = antigravityMaxAllowedIdleConnTimeout
	}
}




func antigravityHTTP11Transport(auth *cliproxyauth.Auth, base *http.Transport, cfgs ...*config.Config) *http.Transport {
	if base == nil {
		return nil
	}
	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	settings := resolveAntigravityPoolSettings(cfg)
	key := antigravityTransportKey{
		credential:          antigravityTransportScope(auth),
		base:                base,
		shortMode:           settings.shortMode,
		idleConnTimeout:     settings.idleConnTimeout,
		maxIdleConnsPerHost: settings.maxIdleConnsPerHost,
	}
	transport, errGet := antigravityTransports.Get(key, func() (*http.Transport, error) {
		return cloneTransportWithHTTP11(base, cfgs...), nil
	})
	if errGet != nil {



		log.Debugf("antigravity executor: cache HTTP/1.1 transport failed: %v", errGet)
		return cloneTransportWithHTTP11(base, cfgs...)
	}
	return transport
}





func antigravityProxiedHTTP11Transport(auth *cliproxyauth.Auth, proxyURL string, cfgs ...*config.Config) *http.Transport {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	settings := resolveAntigravityPoolSettings(cfg)
	key := antigravityTransportKey{
		credential:          antigravityTransportScope(auth),
		proxy:               proxyURL,
		shortMode:           settings.shortMode,
		idleConnTimeout:     settings.idleConnTimeout,
		maxIdleConnsPerHost: settings.maxIdleConnsPerHost,
	}
	transport, errGet := antigravityTransports.Get(key, func() (*http.Transport, error) {
		base, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
		if errBuild != nil {
			return nil, errBuild
		}
		if base == nil {
			return nil, fmt.Errorf("antigravity executor: proxy setting produced no transport")
		}
		return cloneTransportWithHTTP11(base, cfgs...), nil
	})
	if errGet != nil {


		return nil
	}
	return transport
}






func antigravityTransportScope(auth *cliproxyauth.Auth) string {
	if auth == nil {
		return antigravityAnonymousTransportScope
	}
	if id := strings.TrimSpace(auth.ID); id != "" {
		return "id:" + id
	}
	if auth.Attributes != nil {
		if path := strings.TrimSpace(auth.Attributes[cliproxyauth.AttributePath]); path != "" {
			return "path:" + path
		}
		if source := strings.TrimSpace(auth.Attributes[cliproxyauth.AttributeSource]); source != "" {
			return "source:" + source
		}
	}









	if refresh := strings.TrimSpace(metaStringValue(auth.Metadata, "refresh_token")); refresh != "" {
		return antigravityCredentialScope("refresh:", refresh)
	}
	if access := strings.TrimSpace(metaStringValue(auth.Metadata, "access_token")); access != "" {
		return antigravityCredentialScope("token:", access)
	}
	return antigravityAnonymousTransportScope
}




func antigravityCredentialScope(prefix, secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return prefix + hex.EncodeToString(digest[:8])
}






func newAntigravityHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) (clientOut *http.Client) {
	defer func() { clientOut = httptransport.Decorate(ctx, clientOut) }()



	if proxyURL := antigravityProxyURL(ctx, cfg, auth); proxyURL != "" {
		if transport := antigravityProxiedHTTP11Transport(auth, proxyURL, cfg); transport != nil {
			return &http.Client{Transport: transport, Timeout: timeout}
		}


	}

	client := helps.NewProxyAwareHTTPClient(httptransport.WithoutDecorator(ctx), cfg, auth, timeout)

	if client.Transport == nil {
		client.Transport = antigravityHTTP11Transport(auth, antigravityBaseTransport, cfg)
		return client
	}



	transport, ok := client.Transport.(*http.Transport)
	if !ok {

		return client
	}
	if transport == nil {




		transport = antigravityBaseTransport
	}
	client.Transport = antigravityHTTP11Transport(auth, transport, cfg)
	return client
}

func antigravityProxyURL(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth) string {
	if proxyURL := cliproxyexecutor.RequestProxyURL(ctx); proxyURL != "" {
		return proxyURL
	}
	if auth != nil {
		if proxyURL := strings.TrimSpace(auth.ProxyURL); proxyURL != "" {
			return proxyURL
		}
	}
	if cfg != nil {
		return strings.TrimSpace(cfg.ProxyURL)
	}
	return ""
}

func sanitizeAntigravityGeminiRequestSignatures(modelName string, rawJSON []byte) []byte {
	if !antigravityUsesReasoningReplayCache(modelName) {
		return rawJSON
	}
	rawJSON = internalsignature.SanitizeNonVertexGeminiRequestThoughtSignatures(rawJSON, "request.contents")
	return normalizeAntigravityGeminiFunctionResponseRoles(rawJSON)
}

func sanitizeAntigravityGeminiRequestSignaturesDoc(modelName string, doc *ojson.Doc) {
	if doc == nil || !antigravityUsesReasoningReplayCache(modelName) {
		return
	}
	internalsignature.SanitizeNonVertexGeminiRequestThoughtSignaturesDoc(doc, "request.contents")
	normalizeAntigravityGeminiFunctionResponseRolesDoc(doc)
}




func ensureAntigravityGeminiLeadingUserContent(modelName string, payload []byte) []byte {
	if strings.Contains(strings.ToLower(modelName), "claude") {
		return payload
	}
	return helps.EnsureGeminiLeadingUserContent(payload, "request.contents")
}

func ensureAntigravityGeminiLeadingUserContentDoc(modelName string, doc *ojson.Doc) bool {
	if doc == nil || strings.Contains(strings.ToLower(modelName), "claude") {
		return false
	}
	return helps.EnsureGeminiLeadingUserContentDoc(doc, "request.contents")
}




func ensureAntigravityGeminiTrailingUserContent(modelName string, payload []byte) []byte {
	if strings.Contains(strings.ToLower(modelName), "claude") {
		return payload
	}
	return helps.EnsureGeminiTrailingUserContent(payload, "request.contents")
}

func ensureAntigravityGeminiTrailingUserContentDoc(modelName string, doc *ojson.Doc) bool {
	if doc == nil || strings.Contains(strings.ToLower(modelName), "claude") {
		return false
	}
	return helps.EnsureGeminiTrailingUserContentDoc(doc, "request.contents")
}





func ensureAntigravityGeminiBoundaryUserContent(modelName string, payload []byte) []byte {
	if strings.Contains(strings.ToLower(modelName), "claude") {
		return payload
	}
	payload = helps.DropGeminiTrailingThoughtOnlyContent(payload, "request.contents")
	return helps.EnsureGeminiBoundaryUserContent(payload, "request.contents")
}

func ensureAntigravityGeminiBoundaryUserContentDoc(modelName string, doc *ojson.Doc) bool {
	if doc == nil || strings.Contains(strings.ToLower(modelName), "claude") {
		return false
	}
	changed := helps.DropGeminiTrailingThoughtOnlyContentDoc(doc, "request.contents")
	if helps.EnsureGeminiBoundaryUserContentDoc(doc, "request.contents") {
		changed = true
	}
	return changed
}

type antigravityContentEdit struct {
	index       int64
	path        string
	start       int
	end         int
	replacement []byte
}

func normalizeAntigravityGeminiFunctionResponseRolesDoc(doc *ojson.Doc) bool {
	if doc == nil {
		return false
	}
	contents := doc.Get("request.contents")
	if !contents.IsArray() {
		return false
	}
	wrapped := []byte(`{"request":{"contents":` + contents.Raw + `}}`)
	normalized := normalizeAntigravityGeminiFunctionResponseRoles(wrapped)
	if bytes.Equal(normalized, wrapped) {
		return false
	}
	updatedContents := util.GetGJSONBytesNoCopy(normalized, "request.contents")
	if !updatedContents.IsArray() || updatedContents.Raw == contents.Raw {
		return false
	}
	return doc.SetRawBytes("request.contents", []byte(updatedContents.Raw)) == nil
}





func normalizeAntigravityGeminiFunctionResponseRoles(rawJSON []byte) []byte {
	rawJSON = repairAntigravityGeminiFunctionResponseNames(rawJSON)
	contents := util.GetGJSONBytesNoCopy(rawJSON, "request.contents")
	if !contents.IsArray() {
		return rawJSON
	}
	type functionRef struct {
		id   string
		name string
	}

	edits := make([]antigravityContentEdit, 0)
	var pending []functionRef
	validOffsets := true
	contents.ForEach(func(contentIndex, content ojson.Result) bool {
		parts := content.Get("parts")
		if !parts.IsArray() {
			pending = nil
			return true
		}

		var calls, responses []functionRef
		var responseParts, otherParts []json.RawMessage
		partCount := 0
		hasOtherPart := false
		parts.ForEach(func(_, part ojson.Result) bool {
			partCount++
			switch {
			case part.Get("functionCall").Exists():
				calls = append(calls, functionRef{id: part.Get("functionCall.id").String(), name: part.Get("functionCall.name").String()})
			case part.Get("functionResponse").Exists():
				responses = append(responses, functionRef{id: part.Get("functionResponse.id").String(), name: part.Get("functionResponse.name").String()})
				responseParts = append(responseParts, json.RawMessage(part.Raw))
			default:
				hasOtherPart = true
				otherParts = append(otherParts, json.RawMessage(part.Raw))
			}
			return true
		})
		if partCount == 0 {
			pending = nil
			return true
		}
		if len(calls) > 0 && len(responses) == 0 {
			pending = calls
			return true
		}
		if len(responses) == 0 {
			if hasOtherPart {
				pending = nil
			}
			return true
		}
		if len(calls) > 0 {
			pending = nil
			return true
		}

		var contentDoc *ojson.Doc
		contentChanged := false
		if len(pending) > 0 && len(responses) > 0 {
			ordered := make([]json.RawMessage, 0, partCount)
			used := make([]bool, len(responses))
			for _, call := range pending {
				for responseIndex, response := range responses {
					if used[responseIndex] {
						continue
					}
					if (call.id != "" && response.id == call.id) || (call.id == "" && call.name != "" && response.name == call.name) {
						used[responseIndex] = true
						ordered = append(ordered, responseParts[responseIndex])
						break
					}
				}
			}
			for responseIndex := range responses {
				if !used[responseIndex] {
					ordered = append(ordered, responseParts[responseIndex])
				}
			}
			if len(ordered) == len(responseParts) {
				ordered = append(ordered, otherParts...)
				encoded, errMarshal := json.Marshal(ordered)
				if errMarshal == nil && !bytes.Equal(encoded, []byte(parts.Raw)) {
					contentDoc = ojson.NewDoc([]byte(content.Raw))
					if errSet := contentDoc.SetRawBytes("parts", encoded); errSet == nil {
						contentChanged = true
					}
				}
			}
		}
		pending = nil
		if !hasOtherPart && content.Get("role").String() != "model" {
			if contentDoc == nil {
				contentDoc = ojson.NewDoc([]byte(content.Raw))
			}
			if errSet := contentDoc.Set("role", "model"); errSet == nil {
				contentChanged = true
			}
		}
		if !contentChanged || contentDoc == nil {
			return true
		}
		contentJSON := contentDoc.Bytes()

		start := content.Index
		end := start + len(content.Raw)
		if start < 0 || end < start || end > len(rawJSON) || !bytes.Equal(rawJSON[start:end], []byte(content.Raw)) {
			validOffsets = false
		}
		edits = append(edits, antigravityContentEdit{
			index:       contentIndex.Int(),
			start:       start,
			end:         end,
			replacement: contentJSON,
		})
		return true
	})
	return applyAntigravityIndexedEdits(rawJSON, edits, validOffsets)
}




func applyAntigravityIndexedEdits(rawJSON []byte, edits []antigravityContentEdit, validOffsets bool) []byte {
	if len(edits) == 0 {
		return rawJSON
	}
	if !validOffsets || !ojson.ValidBytes(rawJSON) {
		return applyAntigravityContentEditsWithSJSON(rawJSON, edits)
	}

	finalSize := len(rawJSON)
	cursor := 0
	for _, edit := range edits {
		if edit.start < cursor {
			return applyAntigravityContentEditsWithSJSON(rawJSON, edits)
		}
		finalSize += len(edit.replacement) - (edit.end - edit.start)
		if finalSize < 0 {
			return applyAntigravityContentEditsWithSJSON(rawJSON, edits)
		}
		cursor = edit.end
	}
	out := make([]byte, 0, finalSize)
	cursor = 0
	for _, edit := range edits {
		out = append(out, rawJSON[cursor:edit.start]...)
		out = append(out, edit.replacement...)
		cursor = edit.end
	}
	return append(out, rawJSON[cursor:]...)
}



func applyAntigravityContentEditsWithSJSON(rawJSON []byte, edits []antigravityContentEdit) []byte {
	doc := ojson.NewDoc(rawJSON)
	changed := false
	for _, edit := range edits {
		path := edit.path
		if path == "" {
			path = fmt.Sprintf("request.contents.%d", edit.index)
		}
		if errSet := doc.SetRawBytes(path, edit.replacement); errSet == nil {
			changed = true
		}
	}
	if !changed {
		return rawJSON
	}
	return doc.Bytes()
}




func repairAntigravityGeminiFunctionResponseNames(rawJSON []byte) []byte {
	contents := util.GetGJSONBytesNoCopy(rawJSON, "request.contents")
	if !contents.IsArray() {
		return rawJSON
	}
	callIDToName := make(map[string]string)
	contents.ForEach(func(_, content ojson.Result) bool {
		parts := content.Get("parts")
		if !parts.IsArray() {
			return true
		}
		parts.ForEach(func(_, part ojson.Result) bool {
			fc := part.Get("functionCall")
			if fc.Exists() {
				id := strings.TrimSpace(fc.Get("id").String())
				name := strings.TrimSpace(fc.Get("name").String())
				if id != "" && name != "" && name != "unknown" {
					callIDToName[id] = name
				}
			}
			return true
		})
		return true
	})
	if len(callIDToName) == 0 {
		return rawJSON
	}

	edits := make([]antigravityContentEdit, 0)
	validOffsets := true
	contents.ForEach(func(contentIdx, content ojson.Result) bool {
		parts := content.Get("parts")
		if !parts.IsArray() {
			return true
		}
		parts.ForEach(func(partIdx, part ojson.Result) bool {
			fr := part.Get("functionResponse")
			if !fr.Exists() {
				return true
			}
			id := strings.TrimSpace(fr.Get("id").String())
			name := strings.TrimSpace(fr.Get("name").String())
			if id == "" || (name != "" && name != "unknown") {
				return true
			}
			realName, ok := callIDToName[id]
			if !ok {
				return true
			}
			updatedPart, errSet := ojson.SetBytes([]byte(part.Raw), "functionResponse.name", realName)
			if errSet != nil {
				return true
			}
			start := part.Index
			end := start + len(part.Raw)
			if start < 0 || end < start || end > len(rawJSON) || !bytes.Equal(rawJSON[start:end], []byte(part.Raw)) {
				validOffsets = false
			}
			edits = append(edits, antigravityContentEdit{
				index:       contentIdx.Int(),
				path:        fmt.Sprintf("request.contents.%d.parts.%d", contentIdx.Int(), partIdx.Int()),
				start:       start,
				end:         end,
				replacement: updatedPart,
			})
			return true
		})
		return true
	})
	return applyAntigravityIndexedEdits(rawJSON, edits, validOffsets)
}

func validateAntigravityRequestSignatures(ctx context.Context, modelName string, from sdktranslator.Format, rawJSON []byte) ([]byte, error) {
	if from.String() != "claude" {
		return rawJSON, nil
	}
	before := countClaudeThinkingBlocks(rawJSON)
	if antigravityUsesReasoningReplayCache(modelName) {
		rawJSON = antigravityclaude.StripInvalidGeminiSignatureThinkingBlocks(rawJSON)
		logAntigravitySignatureStrip(before, countClaudeThinkingBlocks(rawJSON), "provider_cleanup", "empty_or_non_gemini_signature")
		return rawJSON, nil
	}

	rawJSON = antigravityclaude.StripEmptySignatureThinkingBlocks(rawJSON)
	logAntigravitySignatureStrip(before, countClaudeThinkingBlocks(rawJSON), "prefix_cleanup", "empty_or_non_claude_signature")
	if cache.SignatureCacheEnabled() {
		return rawJSON, nil
	}
	if !cache.SignatureBypassStrictMode() {


		return rawJSON, nil
	}
	before = countClaudeThinkingBlocks(rawJSON)
	rawJSON = antigravityclaude.StripInvalidBypassSignatureThinkingBlocks(rawJSON)
	logAntigravitySignatureStrip(before, countClaudeThinkingBlocks(rawJSON), "strict_bypass", "invalid_antigravity_claude_signature")
	return rawJSON, nil
}

func hasAntigravityClaudeTypedWebSearchTool(payload []byte) bool {
	tools := util.GetGJSONBytesNoCopy(payload, "tools")
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		switch tool.Get("type").String() {
		case "web_search_20250305", "web_search_20260209":
			return true
		}
	}
	return false
}

func hasAntigravityGoogleSearchTool(payload []byte) bool {
	tools := util.GetGJSONBytesNoCopy(payload, "request.tools")
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		if tool.Get("googleSearch").Exists() {
			return true
		}
	}
	return false
}

func hasAntigravityResponsesWebSearchTool(rawJSON []byte) bool {
	tools := util.GetGJSONBytesNoCopy(rawJSON, "tools")
	if !tools.IsArray() {
		return false
	}
	for _, tool := range tools.Array() {
		switch tool.Get("type").String() {
		case "web_search", "web_search_2025_08_26", "web_search_preview", "web_search_preview_2025_03_11":
			return true
		}
	}
	return false
}

func shouldResolveAntigravityWebSearchGroundingURLs(from sdktranslator.Format, originalRequestRawJSON, requestRawJSON []byte) bool {
	if !hasAntigravityGoogleSearchTool(requestRawJSON) {
		return false
	}
	switch from {
	case sdktranslator.FormatClaude:
		return hasAntigravityClaudeTypedWebSearchTool(originalRequestRawJSON)
	case sdktranslator.FormatOpenAIResponse:
		return hasAntigravityResponsesWebSearchTool(originalRequestRawJSON)
	default:
		return false
	}
}

func (e *AntigravityExecutor) resolveWebSearchGroundingURLs(ctx context.Context, auth *cliproxyauth.Auth, from sdktranslator.Format, originalRequestRawJSON, requestRawJSON, responseRawJSON []byte) []byte {
	if !shouldResolveAntigravityWebSearchGroundingURLs(from, originalRequestRawJSON, requestRawJSON) {
		return responseRawJSON
	}
	return helps.ResolveAntigravityGroundingURLs(ctx, e.cfg, auth, responseRawJSON)
}

func countClaudeThinkingBlocks(rawJSON []byte) int {
	messages := util.GetGJSONBytesNoCopy(rawJSON, "messages")
	if !messages.IsArray() {
		return 0
	}

	count := 0
	messages.ForEach(func(_, message ojson.Result) bool {
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, part ojson.Result) bool {
			if part.Get("type").String() == "thinking" {
				count++
			}
			return true
		})
		return true
	})
	return count
}

func logAntigravitySignatureStrip(before, after int, stage, reason string) {
	removed := before - after
	if removed <= 0 {
		return
	}
	log.WithFields(log.Fields{
		"component":       "signature_sanitizer",
		"executor":        "antigravity",
		"target_provider": "claude",
		"action":          "drop_thinking_blocks",
		"stage":           stage,
		"reason":          reason,
		"count":           removed,
	}).Debug("antigravity executor: dropped Claude thinking blocks with invalid signatures")
}


func (e *AntigravityExecutor) Identifier() string { return antigravityAuthType }


func (e *AntigravityExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	token, _, errToken := e.ensureAccessToken(req.Context(), auth)
	if errToken != nil {
		return errToken
	}
	if strings.TrimSpace(token) == "" {
		return statusErr{code: http.StatusUnauthorized, msg: "missing access token"}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}




func (e *AntigravityExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("antigravity executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)





	httpReq.Close = false


	contentType := httpReq.Header.Get("Content-Type")


	for k := range httpReq.Header {
		delete(httpReq.Header, k)
	}


	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	httpReq.Header.Set("User-Agent", resolveUserAgent(auth))


	if err := e.PrepareRequest(httpReq, auth); err != nil {
		return nil, err
	}

	httpClient := newAntigravityHTTPClient(ctx, e.cfg, auth, 0)
	return helps.WithAntigravityHTTPClientTrace(httpClient, auth, "http_request").Do(httpReq)
}


func (e *AntigravityExecutor) SupportsApplyPatch() bool { return e != nil }
