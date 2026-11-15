package helps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

const (
	claudeAnthropicVersion = "2023-06-01"

	claudeDefaultStainlessTimeout = "600"
)

var claudeCodeUserAgentPattern = regexp.MustCompile(`(?i)^claude-cli/`)

var claudeCodeSubclientByEntrypoint = map[string]string{
	"cli":                       "claude-code-cli",
	"mcp":                       "claude-code-mcp",
	"bench":                     "claude-code-bench",
	"sdk-cli":                   "claude-code-cli-sdk",
	"sdk-ts":                    "claude-code-sdk-ts",
	"sdk-py":                    "claude-code-sdk-py",
	"claude-vscode":             "claude-code-vscode",
	"claude-code-github-action": "claude-code-gh-action",
	"local-agent":               "claude-local-agent",
	"local_agent":               "claude-local-agent",
	"claude-desktop":            "claude-desktop",
	"claude-desktop-3p":         "claude-desktop-3p",
	"remote":                    "claude-remote",
	"remote_baku":               "claude-remote-baku",
	"remote_cowork":             "claude-remote-cowork",
	"remote_trigger":            "claude-remote-trigger",
	"remote_desktop":            "claude-remote-desktop",
	"remote_mobile":             "claude-remote-mobile",
	"claude_in_slack":           "claude-in-slack",
	"claude-in-slack":           "claude-in-slack",
	"claude-in-teams":           "claude-in-teams",
	"claude-security":           "claude-security",
	"ssh-remote":                "claude-ssh-remote",
	"claude-coworker":           "claude-coworker",
	"claude-coworker-terminal":  "claude-coworker-terminal",
}

var nativeClaudeEntrypoints = map[string]bool{
	"cli":           true,
	"sdk-cli":       true,
	"sdk-ts":        true,
	"claude-vscode": true,
}

func isNativeClaudeEntrypoint(entrypoint string, cfg *config.Config) bool {
	entrypoint = strings.ToLower(strings.TrimSpace(entrypoint))
	if entrypoint == "" {
		return false
	}
	if nativeClaudeEntrypoints[entrypoint] {
		return true
	}
	if cfg == nil {
		return false
	}
	for _, candidate := range cfg.ClaudeHeaderDefaults.NativeEntrypoints {
		if strings.EqualFold(strings.TrimSpace(candidate), entrypoint) {
			return true
		}
	}
	return false
}

func claudeSubclientForEntrypoint(entrypoint string, cfg *config.Config) string {
	entrypoint = strings.ToLower(strings.TrimSpace(entrypoint))
	if subclient := claudeCodeSubclientByEntrypoint[entrypoint]; subclient != "" {
		return subclient
	}
	if isNativeClaudeEntrypoint(entrypoint, cfg) {
		return "claude-code-" + entrypoint
	}
	return ""
}

type claudeCodeHelperShape uint8

const (
	claudeCodeHelperShapeNone claudeCodeHelperShape = iota
	claudeCodeHelperShapeMinimal
	claudeCodeHelperShapeStructured
	claudeCodeHelperShapeTitle280

	claudeCodeHelperModel = "claude-haiku-4-5-20251001"
)

var measuredClaudeCodeHelperBetaProfiles = map[string]claudeCodeHelperShape{
	claudeCodeHelperBetaProfile(true):  claudeCodeHelperShapeMinimal,
	claudeCodeHelperBetaProfile(false): claudeCodeHelperShapeMinimal,
	claudeCodeHelperBetaProfile(true,
		"advisor-tool-2026-03-01",
		"structured-outputs-2025-12-15",
		"cache-diagnosis-2026-04-07",
	): claudeCodeHelperShapeStructured,
	claudeCodeHelperBetaProfile(true,
		"structured-outputs-2025-12-15",
		"fallback-credit-2026-06-01",
	): claudeCodeHelperShapeStructured,
	claudeCodeHelperBetaProfile(true,
		"structured-outputs-2025-12-15",
	): claudeCodeHelperShapeStructured,
	claudeCodeHelperBetaProfile(false,
		"structured-outputs-2025-12-15",
	): claudeCodeHelperShapeStructured,
	claudeCodeHelperBetaProfile(true,
		"structured-outputs-2025-12-15",
		"server-side-fallback-2026-06-01",
		"fallback-credit-2026-06-01",
		"cache-diagnosis-2026-04-07",
	): claudeCodeHelperShapeTitle280,
}

type ClaudeCodeRequestDetection struct {
	Confirmed       bool
	ConfirmedShape  bool
	StrongSignals   bool
	NativeClient    bool
	XAppCLI         bool
	UserAgent       bool
	BetasPresent    bool
	MetadataUserID  bool
	HelperProfile   bool
	Entrypoint      string
	Subclient       string
	AgentSDKVersion string
}

func DetectClaudeCodeRequest(headers http.Header, payload []byte, countTokens bool, configs ...*config.Config) ClaudeCodeRequestDetection {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	userAgent, validUserAgent := parseClaudeCodeUserAgent(headerValue(headers, "User-Agent"))
	entrypoint := userAgent.entrypoint
	subclient := claudeSubclientForEntrypoint(entrypoint, cfg)
	detection := ClaudeCodeRequestDetection{
		XAppCLI:         isNativeClaudeXApp(headerValue(headers, "X-App")),
		UserAgent:       validUserAgent && plausibleClaudeCodeVersion(userAgent.version, cfg),
		BetasPresent:    headerContainsClaudeCodeBeta(headers),
		Entrypoint:      entrypoint,
		Subclient:       subclient,
		AgentSDKVersion: userAgent.agentSDKVersion,
	}

	metadataUserID := ojson.GetBytesNoCopy(payload, "metadata.user_id")
	detection.MetadataUserID = metadataUserID.Exists() && metadataUserID.Type == ojson.String && isValidUserID(metadataUserID.String())
	detection.NativeClient = isNativeClaudeEntrypoint(entrypoint, cfg)
	standardSignals := detection.XAppCLI && detection.UserAgent && detection.BetasPresent && (countTokens || detection.MetadataUserID)
	detection.HelperProfile = detection.NativeClient && matchesMeasuredClaudeCodeHelperProfile(headers, payload, countTokens, detection, cfg)
	detection.StrongSignals = standardSignals || detection.HelperProfile
	detection.Confirmed = detection.StrongSignals && detection.NativeClient
	detection.ConfirmedShape = detection.StrongSignals ||
		(!countTokens && detection.XAppCLI && detection.UserAgent && detection.MetadataUserID &&
			subclient != "" &&
			measuredClaudeCodeHaiku55HelperSessionMatches(headers, payload) &&
			measuredClaudeCodeBillingAndIdentityMatch(payload, entrypoint))
	return detection
}

func isNativeClaudeXApp(value string) bool {
	switch value {
	case "cli", "cli-bg":
		return true
	default:
		return false
	}
}

func claudeCodeHelperBetaProfile(redactThinking bool, trailing ...string) string {
	betas := []string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"}
	if redactThinking {
		betas = append(betas, "redact-thinking-2026-02-12")
	}
	betas = append(betas,
		"thinking-token-count-2026-05-13",
		"context-management-2025-06-27",
		"prompt-caching-scope-2026-01-05",
	)
	betas = append(betas, trailing...)
	return strings.Join(betas, ",")
}

func matchesMeasuredClaudeCodeHelperProfile(
	headers http.Header,
	payload []byte,
	countTokens bool,
	detection ClaudeCodeRequestDetection,
	cfg *config.Config,
) bool {
	if countTokens ||
		detection.BetasPresent ||
		!detection.XAppCLI ||
		!detection.UserAgent ||
		!detection.MetadataUserID {
		return false
	}

	if detection.Entrypoint == "cli" {
		shape := measuredClaudeCodeHelperBetaProfiles[normalizedClaudeBetaHeader(headers)]
		if shape != claudeCodeHelperShapeNone &&
			measuredClaudeCodeHelperBodyShape(payload) == shape &&
			measuredClaudeCodeHelperHeadersMatch(headers, cfg, shape) &&
			measuredClaudeCodeHelperSessionMatches(headers, payload) {
			return true
		}
	}
	return matchesMeasuredClaudeCodeHaiku55HelperProfile(headers, payload, detection, cfg)
}

func matchesMeasuredClaudeCodeHaiku55HelperProfile(
	headers http.Header,
	payload []byte,
	detection ClaudeCodeRequestDetection,
	cfg *config.Config,
) bool {
	if !detection.NativeClient || ojson.GetBytesNoCopy(payload, "model").String() != "claude-haiku-5-5" {
		return false
	}
	if !measuredClaudeCodeHaiku55HelperHeadersMatch(headers, cfg) {
		return false
	}
	if !measuredClaudeCodeHaiku55HelperSessionMatches(headers, payload) {
		return false
	}
	return measuredClaudeCodeHaiku55HelperBodyMatches(payload, detection.Entrypoint)
}

func measuredClaudeCodeHaiku55HelperHeadersMatch(headers http.Header, cfg *config.Config) bool {
	profile := defaultClaudeDeviceProfile(cfg)
	expected := map[string]string{
		"Accept":                  "application/json",
		"Content-Type":            "application/json",
		"X-Stainless-Lang":        "js",
		"X-Stainless-Runtime":     "node",
		"X-Stainless-Retry-Count": "0",
		"X-Stainless-Timeout":     claudeDefaultStainlessTimeout,
		"Anthropic-Version":       claudeAnthropicVersion,
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
	}
	for name, want := range expected {
		if headerValue(headers, name) != want {
			return false
		}
	}
	for _, name := range []string{
		"X-Stainless-Package-Version",
		"X-Stainless-Runtime-Version",
		"X-Stainless-OS",
		"X-Stainless-Arch",
	} {
		if headerValue(headers, name) == "" {
			return false
		}
	}
	if !plausibleClaudeCodeUserAgent(headerValue(headers, "User-Agent"), cfg) ||
		!plausibleClaudePackageVersion(headerValue(headers, "X-Stainless-Package-Version"), profile.PackageVersion) ||
		!plausibleClaudeRuntimeVersion(headerValue(headers, "X-Stainless-Runtime-Version"), profile.RuntimeVersion) {
		return false
	}
	if headerValue(headers, "X-Stainless-Async") != "" {
		return false
	}
	if headerValue(headers, "Accept-Encoding") != "gzip, deflate, br, zstd" {
		return false
	}
	if requestID := headerValue(headers, "X-Client-Request-Id"); requestID != "" {
		if _, errRequestID := uuid.Parse(requestID); errRequestID != nil {
			return false
		}
	}
	betaHeader := normalizedClaudeBetaHeader(headers)
	return strings.Contains(betaHeader, "interleaved-thinking-2025-05-14") &&
		strings.Contains(betaHeader, "thinking-token-count-2026-05-13") &&
		strings.Contains(betaHeader, "context-management-2025-06-27") &&
		strings.Contains(betaHeader, "prompt-caching-scope-2026-01-05")
}

func measuredClaudeCodeHaiku55HelperSessionMatches(headers http.Header, payload []byte) bool {
	metadata := ojson.GetBytesNoCopy(payload, "metadata")
	if !metadata.IsObject() || !claudeJSONObjectHasKeys([]byte(metadata.Raw), []string{"user_id"}) {
		return false
	}
	userID := metadata.Get("user_id")
	if userID.Type != ojson.String || !isValidUserID(userID.String()) {
		return false
	}
	var identityMap map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal([]byte(userID.String()), &identityMap); errUnmarshal != nil {
		return false
	}
	for key := range identityMap {
		switch key {
		case "device_id", "account_uuid", "session_id", "parent_session_id":
		default:
			return false
		}
	}
	if len(identityMap) < 3 {
		return false
	}
	return headerValue(headers, ClaudeCodeSessionHeader) == ojson.GetBytesNoCopy([]byte(userID.String()), "session_id").String()
}

func measuredClaudeCodeHaiku55HelperBodyMatches(payload []byte, entrypoint string) bool {
	if ojson.GetBytesNoCopy(payload, "max_tokens").Type != ojson.Number {
		return false
	}
	messages := ojson.GetBytesNoCopy(payload, "messages")
	if !messages.IsArray() || len(messages.Array()) != 1 || messages.Get("0.role").String() != "user" {
		return false
	}
	if !measuredClaudeCodeBillingAndIdentityMatch(payload, entrypoint) {
		return false
	}
	tools := ojson.GetBytesNoCopy(payload, "tools")
	if !tools.IsArray() {
		return false
	}
	toolItems := tools.Array()
	if len(toolItems) == 0 {
		format := ojson.GetBytesNoCopy(payload, "output_config.format")
		required := format.Get("schema.required")
		return format.Get("type").String() == "json_schema" &&
			format.Get("schema.type").String() == "object" &&
			format.Get("schema.properties.title.type").String() == "string" &&
			required.IsArray() && len(required.Array()) == 1 && required.Get("0").String() == "title" &&
			format.Get("schema.additionalProperties").Type == ojson.False
	}
	if len(toolItems) == 1 {
		return toolItems[0].Get("name").String() == "web_search" &&
			strings.HasPrefix(toolItems[0].Get("type").String(), "web_search_")
	}
	return false
}

func measuredClaudeCodeBillingAndIdentityMatch(payload []byte, entrypoint string) bool {
	system := ojson.GetBytesNoCopy(payload, "system")
	if !system.IsArray() || len(system.Array()) < 2 {
		return false
	}
	billing := system.Get("0.text").String()
	if !strings.HasPrefix(billing, "x-anthropic-billing-header:") || !strings.Contains(billing, "cc_version=") {
		return false
	}
	if entrypoint != "" && !strings.Contains(billing, "cc_entrypoint="+entrypoint) {
		return false
	}
	if strings.Contains(billing, " cch=") && !measuredClaudeBillingCCH(billing) {
		return false
	}
	identity := strings.TrimSpace(system.Get("1.text").String())
	return identity == "You are Claude Code, Anthropic's official CLI for Claude." ||
		identity == "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK." ||
		identity == "You are a Claude agent, built on Anthropic's Claude Agent SDK."
}

func normalizedClaudeBetaHeader(headers http.Header) string {
	if headers == nil {
		return ""
	}
	values := headers.Values("Anthropic-Beta")
	if len(values) == 0 {
		keys := make([]string, 0, 2)
		for key := range headers {
			if strings.EqualFold(key, "Anthropic-Beta") {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			values = append(values, headers[key]...)
		}
	}
	betas := make([]string, 0, 12)
	for _, value := range values {
		for _, beta := range strings.Split(value, ",") {
			if beta = strings.TrimSpace(beta); beta != "" {
				betas = append(betas, beta)
			}
		}
	}
	return strings.Join(betas, ",")
}

func measuredClaudeCodeHelperHeadersMatch(headers http.Header, cfg *config.Config, shape claudeCodeHelperShape) bool {
	profile := defaultClaudeDeviceProfile(cfg)
	expected := map[string]string{
		"Accept":                  "application/json",
		"Content-Type":            "application/json",
		"X-Stainless-Lang":        "js",
		"X-Stainless-Runtime":     "node",
		"X-Stainless-Retry-Count": "0",
		"X-Stainless-Timeout":     claudeDefaultStainlessTimeout,
		"Anthropic-Version":       claudeAnthropicVersion,
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
	}
	for name, want := range expected {
		if headerValue(headers, name) != want {
			return false
		}
	}

	for _, name := range []string{
		"X-Stainless-Package-Version",
		"X-Stainless-Runtime-Version",
		"X-Stainless-OS",
		"X-Stainless-Arch",
	} {
		if headerValue(headers, name) == "" {
			return false
		}
	}
	candidate := ClaudeDeviceProfile{
		UserAgent:      headerValue(headers, "User-Agent"),
		PackageVersion: headerValue(headers, "X-Stainless-Package-Version"),
		RuntimeVersion: headerValue(headers, "X-Stainless-Runtime-Version"),
	}
	if version, ok := parseClaudeCLIVersion(candidate.UserAgent); ok {
		candidate.version = version
		candidate.hasVersion = true
	}
	if !meetsClaudeDeviceProfileBaseline(candidate, profile) {
		return false
	}

	if headerValue(headers, "X-Stainless-Async") != "" {
		return false
	}
	if headerValue(headers, "Accept-Encoding") != "gzip, deflate, br, zstd" {
		return false
	}
	if requestID := headerValue(headers, "X-Client-Request-Id"); requestID != "" {
		if _, errRequestID := uuid.Parse(requestID); errRequestID != nil {
			return false
		}
	}
	return true
}

func measuredClaudeCodeHelperSessionMatches(headers http.Header, payload []byte) bool {
	metadata := ojson.GetBytesNoCopy(payload, "metadata")
	if !metadata.IsObject() || !claudeJSONObjectHasKeys([]byte(metadata.Raw), []string{"user_id"}) {
		return false
	}
	userID := metadata.Get("user_id")
	if userID.Type != ojson.String || !isValidUserID(userID.String()) {
		return false
	}

	identityRaw := []byte(userID.String())
	if !claudeJSONObjectHasKeys(identityRaw, []string{"device_id", "account_uuid", "session_id"}) &&
		!claudeJSONObjectHasKeys(identityRaw, []string{"device_id", "account_uuid", "session_id", "parent_session_id"}) {
		return false
	}
	return headerValue(headers, ClaudeCodeSessionHeader) == ojson.GetBytesNoCopy(identityRaw, "session_id").String()
}

func measuredClaudeCodeHelperBodyShape(payload []byte) claudeCodeHelperShape {
	minimalKeys := []string{"model", "max_tokens", "messages", "metadata"}
	structuredKeys := []string{"model", "messages", "system", "tools", "metadata", "max_tokens", "thinking", "temperature", "output_config", "stream"}
	structuredTitle280Keys := []string{"model", "max_tokens", "messages", "metadata", "output_config"}
	shape := claudeCodeHelperShapeNone
	isTitle280 := false
	switch {
	case claudeJSONObjectHasKeys(payload, minimalKeys):
		shape = claudeCodeHelperShapeMinimal
	case claudeJSONObjectHasKeys(payload, structuredKeys):
		shape = claudeCodeHelperShapeStructured
	case claudeJSONObjectHasKeys(payload, structuredTitle280Keys):
		shape = claudeCodeHelperShapeTitle280
		isTitle280 = true
	default:
		return claudeCodeHelperShapeNone
	}

	maxTokens := ojson.GetBytesNoCopy(payload, "max_tokens")
	if ojson.GetBytesNoCopy(payload, "model").String() != claudeCodeHelperModel ||
		maxTokens.Type != ojson.Number {
		return claudeCodeHelperShapeNone
	}
	messages := ojson.GetBytesNoCopy(payload, "messages")
	if !messages.IsArray() || len(messages.Array()) != 1 {
		return claudeCodeHelperShapeNone
	}
	message := messages.Get("0")
	if !claudeJSONObjectHasKeys([]byte(message.Raw), []string{"role", "content"}) ||
		message.Get("role").String() != "user" {
		return claudeCodeHelperShapeNone
	}

	if shape == claudeCodeHelperShapeMinimal {
		if maxTokens.Raw != "1" || message.Get("content").Type != ojson.String {
			return claudeCodeHelperShapeNone
		}
		return shape
	}

	if isTitle280 {
		outputConfig := ojson.GetBytesNoCopy(payload, "output_config")
		format := outputConfig.Get("format")
		schema := format.Get("schema")
		if maxTokens.Raw != "80" ||
			message.Get("content").Type != ojson.String ||
			!claudeJSONObjectHasKeys([]byte(outputConfig.Raw), []string{"format"}) ||
			!claudeJSONObjectHasKeys([]byte(format.Raw), []string{"type", "schema"}) ||
			format.Get("type").String() != "json_schema" ||
			!claudeJSONObjectHasKeys([]byte(schema.Raw), []string{"type"}) ||
			schema.Get("type").String() != "object" {
			return claudeCodeHelperShapeNone
		}
		return shape
	}

	content := message.Get("content")
	if !content.IsArray() || len(content.Array()) != 1 {
		return claudeCodeHelperShapeNone
	}
	contentBlock := content.Get("0")
	if !claudeJSONObjectHasKeys([]byte(contentBlock.Raw), []string{"type", "text"}) ||
		contentBlock.Get("type").String() != "text" {
		return claudeCodeHelperShapeNone
	}
	if !measuredClaudeCodeHelperSystemMatches(ojson.GetBytesNoCopy(payload, "system")) {
		return claudeCodeHelperShapeNone
	}
	if tools := ojson.GetBytesNoCopy(payload, "tools"); !tools.IsArray() || len(tools.Array()) != 0 {
		return claudeCodeHelperShapeNone
	}
	thinking := ojson.GetBytesNoCopy(payload, "thinking")
	outputConfig := ojson.GetBytesNoCopy(payload, "output_config")
	if !claudeJSONObjectHasKeys([]byte(thinking.Raw), []string{"type"}) ||
		thinking.Get("type").String() != "disabled" {
		return claudeCodeHelperShapeNone
	}
	format := outputConfig.Get("format")
	schema := format.Get("schema")
	properties := schema.Get("properties")
	titleProperty := properties.Get("title")
	required := schema.Get("required")
	additionalProperties := schema.Get("additionalProperties")
	if !claudeJSONObjectHasKeys([]byte(outputConfig.Raw), []string{"format"}) ||
		!claudeJSONObjectHasKeys([]byte(format.Raw), []string{"type", "schema"}) ||
		format.Get("type").String() != "json_schema" ||
		!claudeJSONObjectHasKeys([]byte(schema.Raw), []string{"type", "properties", "required", "additionalProperties"}) ||
		schema.Get("type").String() != "object" ||
		!claudeJSONObjectHasKeys([]byte(properties.Raw), []string{"title"}) ||
		!claudeJSONObjectHasKeys([]byte(titleProperty.Raw), []string{"type"}) ||
		titleProperty.Get("type").String() != "string" ||
		!required.IsArray() || len(required.Array()) != 1 || required.Get("0").String() != "title" ||
		additionalProperties.Type != ojson.False {
		return claudeCodeHelperShapeNone
	}
	temperature := ojson.GetBytesNoCopy(payload, "temperature")
	if maxTokens.Raw != "32000" ||
		temperature.Raw != "1" ||
		ojson.GetBytesNoCopy(payload, "stream").Type != ojson.True {
		return claudeCodeHelperShapeNone
	}
	return shape
}

func measuredClaudeCodeHelperSystemMatches(system ojson.Result) bool {
	if !system.IsArray() || len(system.Array()) != 3 {
		return false
	}
	for _, block := range system.Array() {
		if !claudeJSONObjectHasKeys([]byte(block.Raw), []string{"type", "text"}) || block.Get("type").String() != "text" {
			return false
		}
	}
	billing := system.Get("0.text").String()
	identity := system.Get("1.text").String()
	return strings.HasPrefix(billing, "x-anthropic-billing-header:") && measuredClaudeBillingCCH(billing) && strings.HasPrefix(identity, "You are Claude Code")
}

func measuredClaudeBillingCCH(billing string) bool {
	marker := strings.Index(billing, " cch=")
	if marker < 0 {
		return false
	}
	valueStart := marker + len(" cch=")
	valueEnd := valueStart + 5
	if valueEnd >= len(billing) || billing[valueEnd] != ';' {
		return false
	}
	for _, character := range billing[valueStart:valueEnd] {
		decimal := character >= '0' && character <= '9'
		lowerHex := character >= 'a' && character <= 'f'
		if !decimal && !lowerHex {
			return false
		}
	}
	return true
}

func claudeJSONObjectHasKeys(raw []byte, want []string) bool {
	if !json.Valid(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, errOpening := decoder.Token()
	if errOpening != nil || opening != json.Delim('{') {
		return false
	}
	keyIndex := 0
	for decoder.More() {
		token, errToken := decoder.Token()
		if errToken != nil {
			return false
		}
		key, okKey := token.(string)
		if !okKey || keyIndex >= len(want) || key != want[keyIndex] {
			return false
		}
		keyIndex++
		var value json.RawMessage
		if errValue := decoder.Decode(&value); errValue != nil {
			return false
		}
	}
	closing, errClosing := decoder.Token()
	return errClosing == nil && closing == json.Delim('}') && keyIndex == len(want)
}

func IsClaudeCodeUserAgent(userAgent string) bool {
	return claudeCodeUserAgentPattern.MatchString(strings.TrimSpace(userAgent))
}

func plausibleClaudeCodeUserAgent(userAgent string, cfg *config.Config) bool {
	parsed, ok := parseClaudeCodeUserAgent(userAgent)
	return ok && plausibleClaudeCodeVersion(parsed.version, cfg)
}

func plausibleClaudeCodeVersion(candidate claudeCLIVersion, cfg *config.Config) bool {
	baseline, okBaseline := parseClaudeCLIVersion(defaultClaudeDeviceProfile(cfg).UserAgent)

	return okBaseline && plausibleClaudeCLIVersion(candidate, baseline)
}

func parseClaudeCodeUserAgentDetails(userAgent string) (entrypoint, agentSDKVersion string) {
	parsed, _ := parseClaudeCodeUserAgent(userAgent)
	return parsed.entrypoint, parsed.agentSDKVersion
}

func headerValue(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	if value := headers.Get(name); value != "" {
		return value
	}
	for key, values := range headers {
		if !strings.EqualFold(key, name) || len(values) == 0 {
			continue
		}
		return values[0]
	}
	return ""
}

func headerContainsClaudeCodeBeta(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for key, values := range headers {
		if !strings.EqualFold(key, "Anthropic-Beta") {
			continue
		}
		for _, value := range values {
			for _, beta := range strings.Split(value, ",") {
				if strings.TrimSpace(beta) == "claude-code-20250219" {
					return true
				}
			}
		}
	}
	return false
}
