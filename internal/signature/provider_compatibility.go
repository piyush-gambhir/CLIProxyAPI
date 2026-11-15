package signature

import "strings"

type SignatureProvider string

const (
	SignatureProviderUnknown      SignatureProvider = "unknown"
	SignatureProviderClaude       SignatureProvider = "claude"
	SignatureProviderGemini       SignatureProvider = "gemini"
	SignatureProviderGeminiBypass SignatureProvider = "gemini_bypass"
	SignatureProviderGPT          SignatureProvider = "gpt"

	SignatureProviderKimi SignatureProvider = "kimi"

	SignatureProviderGrok SignatureProvider = "grok"

	SignatureProviderSWE SignatureProvider = "swe"

	SignatureProviderMeta SignatureProvider = "meta"
)

type SignatureBlockKind string

const (
	SignatureBlockKindUnknown            SignatureBlockKind = "unknown"
	SignatureBlockKindClaudeThinking     SignatureBlockKind = "claude_thinking"
	SignatureBlockKindGeminiModelPart    SignatureBlockKind = "gemini_model_part"
	SignatureBlockKindGeminiFunctionCall SignatureBlockKind = "gemini_function_call"
	SignatureBlockKindGPTReasoning       SignatureBlockKind = "gpt_reasoning"
)

type SignatureCompatibilityAction string

const (
	SignatureActionPreserve                SignatureCompatibilityAction = "preserve"
	SignatureActionDropBlock               SignatureCompatibilityAction = "drop_block"
	SignatureActionDropSignature           SignatureCompatibilityAction = "drop_signature"
	SignatureActionReplaceWithGeminiBypass SignatureCompatibilityAction = "replace_with_gemini_bypass"
	SignatureActionNoCompatibleReplacement SignatureCompatibilityAction = "no_compatible_replacement"
)

type SignatureCompatibilityDecision struct {
	TargetProvider       SignatureProvider
	DetectedProvider     SignatureProvider
	BlockKind            SignatureBlockKind
	Compatible           bool
	Action               SignatureCompatibilityAction
	ReplacementSignature string
	NormalizedSignature  string
	Reason               string
}

func SignatureProviderFromModelName(modelName string) SignatureProvider {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return SignatureProviderUnknown
	}

	candidate := lower
	if idx := strings.LastIndexAny(lower, "/:"); idx >= 0 && idx < len(lower)-1 {
		candidate = lower[idx+1:]
	}
	if provider := signatureProviderFromModelNameValue(candidate); provider != SignatureProviderUnknown {
		return provider
	}
	if candidate != lower {
		return signatureProviderFromModelNameValue(lower)
	}
	return SignatureProviderUnknown
}

func signatureProviderFromModelNameValue(lower string) SignatureProvider {
	switch {
	case strings.Contains(lower, "claude"):
		return SignatureProviderClaude
	case strings.Contains(lower, "gemini"):
		return SignatureProviderGemini
	case strings.Contains(lower, "gpt"),
		strings.Contains(lower, "openai"),
		strings.Contains(lower, "codex"),
		strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"):
		return SignatureProviderGPT
	case strings.Contains(lower, "kimi"),
		strings.Contains(lower, "moonshot"),
		strings.HasPrefix(lower, "k2"),
		strings.HasPrefix(lower, "k3"):
		return SignatureProviderKimi
	case strings.Contains(lower, "grok"):
		return SignatureProviderGrok
	case isMetaMuseModelName(lower):
		return SignatureProviderMeta
	case strings.Contains(lower, "swe-"):
		return SignatureProviderSWE
	default:
		return SignatureProviderUnknown
	}
}

func isMetaMuseModelName(lower string) bool {
	if lower == "muse" ||
		strings.HasPrefix(lower, "muse-") ||
		strings.HasPrefix(lower, "muse_") ||
		strings.HasSuffix(lower, "/muse") ||
		strings.Contains(lower, "/muse-") ||
		strings.Contains(lower, "/muse_") {
		return true
	}
	for _, prefix := range []string{"meta-", "meta_"} {
		if idx := strings.Index(lower, prefix+"muse"); idx >= 0 {
			rest := lower[idx+len(prefix+"muse"):]
			if rest == "" || rest[0] == '-' || rest[0] == '_' || rest[0] == '.' || rest[0] == ':' {
				return true
			}
		}
	}
	return false
}

const selfDescribingSignatureFirstChars = "ACEQRg"

func base64AlphabetSet(extra string) [256]bool {
	var set [256]bool
	for c := byte('A'); c <= 'Z'; c++ {
		set[c] = true
	}
	for c := byte('a'); c <= 'z'; c++ {
		set[c] = true
	}
	for c := byte('0'); c <= '9'; c++ {
		set[c] = true
	}
	for i := 0; i < len(extra); i++ {
		set[extra[i]] = true
	}
	return set
}

func maybeSelfDescribingSignatureEnvelope(rawSignature string) bool {
	if rawSignature == "" {
		return false
	}
	return strings.IndexByte(selfDescribingSignatureFirstChars, rawSignature[0]) >= 0
}

func DetectSignatureProvider(rawSignature string) SignatureProvider {
	return DetectSignatureProviderForBlock(rawSignature, SignatureBlockKindUnknown)
}

func DetectSignatureProviderForBlock(rawSignature string, blockKind SignatureBlockKind) SignatureProvider {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return SignatureProviderUnknown
	}

	if prefixedProvider, unprefixed, ok := SplitSignatureProviderPrefix(sig); ok {

		if strings.Contains(unprefixed, "#") {
			return SignatureProviderUnknown
		}
		switch prefixedProvider {
		case SignatureProviderGemini:
			if IsGeminiThoughtSignatureBypass(unprefixed) {
				return SignatureProviderGeminiBypass
			}
			if isRecognizedGeminiProviderSignature(unprefixed, blockKind) {
				return SignatureProviderGemini
			}
		case SignatureProviderClaude:
			if IsValidClaudeThinkingSignature(unprefixed, ClaudeSignatureValidationOptions{Strict: true}) || IsValidClaudeCAISSignature(unprefixed) {
				return SignatureProviderClaude
			}
		case SignatureProviderGPT:
			if IsValidGPTReasoningSignature(unprefixed) {
				return SignatureProviderGPT
			}
		case SignatureProviderSWE:
			if strings.HasPrefix(unprefixed, "sealed.v1.") {
				return SignatureProviderSWE
			}
		}
		return SignatureProviderUnknown
	}
	if strings.Contains(sig, "#") {
		return SignatureProviderUnknown
	}

	if IsGeminiThoughtSignatureBypass(sig) {
		return SignatureProviderGeminiBypass
	}
	if strings.HasPrefix(sig, "sealed.v1.") {
		return SignatureProviderSWE
	}

	if maybeSelfDescribingSignatureEnvelope(sig) {
		if IsValidGPTReasoningSignature(sig) {
			return SignatureProviderGPT
		}
		if IsValidMetaReasoningSignature(sig) {
			return SignatureProviderMeta
		}
		if IsValidClaudeCAISSignature(sig) {
			return SignatureProviderClaude
		}
		if IsValidClaudeThinkingSignature(sig, ClaudeSignatureValidationOptions{Strict: true}) {
			return SignatureProviderClaude
		}
		if isRecognizedGeminiProviderSignature(sig, blockKind) {
			return SignatureProviderGemini
		}
	}

	if IsValidKimiThinkingSignature(sig) {
		return SignatureProviderKimi
	}
	return SignatureProviderUnknown
}

func IsSignatureCompatibleWithProvider(targetProvider SignatureProvider, rawSignature string) bool {
	decision := DecideSignatureCompatibility(targetProvider, rawSignature, SignatureBlockKindUnknown)
	return decision.Compatible
}

func DecideSignatureCompatibility(targetProvider SignatureProvider, rawSignature string, blockKind SignatureBlockKind) SignatureCompatibilityDecision {
	return DecideSignatureCompatibilityForModel(targetProvider, "", rawSignature, blockKind)
}

func DecideSignatureCompatibilityForModel(targetProvider SignatureProvider, targetModel string, rawSignature string, blockKind SignatureBlockKind) SignatureCompatibilityDecision {
	targetProvider = normalizeSignatureTargetProvider(targetProvider)
	if blockKind == "" {
		blockKind = SignatureBlockKindUnknown
	}

	detected := DetectSignatureProviderForBlock(rawSignature, blockKind)
	decision := SignatureCompatibilityDecision{
		TargetProvider:   targetProvider,
		DetectedProvider: detected,
		BlockKind:        blockKind,
	}

	if targetProvider == SignatureProviderClaude && detected == SignatureProviderClaude &&
		strings.HasPrefix(SignaturePayloadWithoutProviderPrefix(rawSignature), "Q") {
		decision.Action = SignatureActionDropBlock
		decision.Reason = "Antigravity CAQS wrapper requires Antigravity replay"
		return decision
	}

	if signatureProviderMatchesTarget(targetProvider, detected) {

		if normalized := normalizeCompatibleSignatureForProvider(targetProvider, rawSignature, blockKind); normalized != "" {
			decision.Compatible = true
			decision.Action = SignatureActionPreserve
			decision.NormalizedSignature = normalized
			decision.Reason = claudeCompatibleSignatureReason(targetProvider, rawSignature, targetModel)
			return decision
		}
	}

	decision.Compatible = false
	switch targetProvider {
	case SignatureProviderGemini:
		if blockKind == SignatureBlockKindGeminiFunctionCall || blockKind == SignatureBlockKindGeminiModelPart || blockKind == SignatureBlockKindUnknown {
			decision.Action = SignatureActionReplaceWithGeminiBypass
			decision.ReplacementSignature = GeminiSkipThoughtSignatureValidator
			decision.Reason = "missing or incompatible signature"
			return decision
		}
		decision.Action = SignatureActionDropBlock
		decision.Reason = "signature is not compatible with Gemini and this block is not a bypass-safe Gemini model part"
	case SignatureProviderClaude:
		decision.Action = SignatureActionDropBlock
		decision.Reason = "Claude has no cross-provider bypass sentinel for thinking blocks"
	case SignatureProviderGPT:
		decision.Action = SignatureActionDropBlock
		decision.Reason = "GPT reasoning encrypted_content cannot be synthesized from another provider signature"
	case SignatureProviderSWE:
		decision.Action = SignatureActionDropBlock
		decision.Reason = "SWE requires sealed.v1 signature from its own backend"
	case SignatureProviderMeta:
		decision.Action = SignatureActionDropBlock
		decision.Reason = "Meta Muse requires its native encrypted_content envelope and has no cross-provider bypass sentinel"
	case SignatureProviderKimi:

		decision.Action = SignatureActionDropSignature
		decision.Reason = "Kimi does not validate replayed thinking signatures, so the block survives without one"
	case SignatureProviderGrok:

		decision.Action = SignatureActionDropBlock
		decision.Reason = "xAI verifies encrypted_content on replay and rejects foreign or mutated blobs"
	default:
		decision.Action = SignatureActionNoCompatibleReplacement
		decision.Reason = "unknown target provider"
	}
	return decision
}

func SplitSignatureProviderPrefix(rawSignature string) (SignatureProvider, string, bool) {
	prefix, rest, ok := strings.Cut(strings.TrimSpace(rawSignature), "#")
	if !ok {
		return SignatureProviderUnknown, rawSignature, false
	}
	provider := SignatureProviderFromCachePrefix(prefix)
	if provider == SignatureProviderUnknown {
		return SignatureProviderUnknown, rawSignature, false
	}
	return provider, strings.TrimSpace(rest), true
}

func SignatureProviderFromCachePrefix(prefix string) SignatureProvider {
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "claude", "anthropic", "cais", "claude-cais", "claude_cais", "ccmax", "claude-code-max", "claude_code_max":
		return SignatureProviderClaude
	case "gemini", "google":
		return SignatureProviderGemini
	case "openai", "gpt", "codex":
		return SignatureProviderGPT
	case "swe", "sealed":
		return SignatureProviderSWE
	default:
		return SignatureProviderUnknown
	}
}

func SignaturePayloadWithoutProviderPrefix(rawSignature string) string {
	if _, unprefixed, ok := SplitSignatureProviderPrefix(rawSignature); ok {
		return unprefixed
	}
	return strings.TrimSpace(rawSignature)
}

func CompatibleSignatureForProvider(targetProvider SignatureProvider, rawSignature string) (string, bool) {
	return CompatibleSignatureForProviderBlock(targetProvider, rawSignature, SignatureBlockKindUnknown)
}

func CompatibleSignatureForProviderBlock(targetProvider SignatureProvider, rawSignature string, blockKind SignatureBlockKind) (string, bool) {
	decision := DecideSignatureCompatibility(targetProvider, rawSignature, blockKind)
	if !decision.Compatible || decision.NormalizedSignature == "" {
		return "", false
	}
	return decision.NormalizedSignature, true
}

func CompatibleAntigravityClaudeThinkingSignature(rawSignature string) (string, bool) {
	if DetectSignatureProviderForBlock(rawSignature, SignatureBlockKindClaudeThinking) != SignatureProviderClaude {
		return "", false
	}
	normalized, err := NormalizeClaudeThinkingSignature(
		SignaturePayloadWithoutProviderPrefix(rawSignature),
		ClaudeSignatureValidationOptions{Strict: true},
	)
	if err != nil {
		return "", false
	}
	return normalized, true
}

func claudeCompatibleSignatureReason(targetProvider SignatureProvider, rawSignature, targetModel string) string {
	const genericReason = "signature provider matches target provider"
	if targetProvider != SignatureProviderClaude {
		return genericReason
	}
	info, err := InspectClaudeCAISSignature(SignaturePayloadWithoutProviderPrefix(rawSignature))
	if err != nil {
		return genericReason
	}
	var reason string
	if info.ModelText != "" {
		reason = "valid Claude CAIS signature with embedded model " + info.ModelText + " is compatible with any Claude target"
	} else if info.EnvelopeVersion >= 4 {
		reason = "valid Claude CAQS signature is compatible with any Claude target"
	} else {
		reason = "valid Claude CAIS signature is compatible with any Claude target"
	}
	if trimmedModel := strings.TrimSpace(targetModel); trimmedModel != "" {
		reason += ", including target model " + trimmedModel
	}
	return reason
}

func normalizeSignatureTargetProvider(provider SignatureProvider) SignatureProvider {
	switch provider {
	case SignatureProviderGeminiBypass:
		return SignatureProviderGemini
	default:
		return provider
	}
}

func signatureProviderMatchesTarget(target, detected SignatureProvider) bool {
	switch target {
	case SignatureProviderGemini:
		return detected == SignatureProviderGemini || detected == SignatureProviderGeminiBypass
	case SignatureProviderClaude:
		return detected == SignatureProviderClaude
	case SignatureProviderGPT:
		return detected == SignatureProviderGPT
	case SignatureProviderSWE:
		return detected == SignatureProviderSWE
	case SignatureProviderMeta:
		return detected == SignatureProviderMeta
	case SignatureProviderKimi:
		return detected == SignatureProviderKimi
	default:

		return false
	}
}

func normalizeCompatibleSignatureForProvider(targetProvider SignatureProvider, rawSignature string, blockKind SignatureBlockKind) string {
	payload := SignaturePayloadWithoutProviderPrefix(rawSignature)
	switch normalizeSignatureTargetProvider(targetProvider) {
	case SignatureProviderClaude:
		if IsValidClaudeCAISSignature(payload) {
			return payload
		}
		normalized, err := NormalizeClaudeProviderNativeThinkingSignature(payload)
		if err != nil {
			return ""
		}
		return normalized
	case SignatureProviderGemini:
		if IsGeminiThoughtSignatureBypass(payload) {
			return payload
		}
		if isRecognizedGeminiProviderSignature(payload, blockKind) {
			return payload
		}
	case SignatureProviderGPT:
		if IsValidGPTReasoningSignature(payload) {
			return payload
		}
	case SignatureProviderSWE:
		if strings.HasPrefix(payload, "sealed.v1.") {
			return payload
		}
	case SignatureProviderMeta:
		if IsValidMetaReasoningSignature(payload) {
			return payload
		}
	case SignatureProviderKimi:
		if IsValidKimiThinkingSignature(payload) {
			return payload
		}
	}
	return ""
}

func isRecognizedGeminiProviderSignature(rawSignature string, blockKind SignatureBlockKind) bool {
	if IsValidClaudeCAISSignature(rawSignature) {
		return false
	}
	if IsValidGeminiThoughtSignature(rawSignature, GeminiThoughtSignatureValidationOptions{RequireKnownEnvelope: true}) {
		return true
	}
	return false
}

func IsRecognizedReasoningSignature(rawSignature string) bool {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return false
	}
	if DetectSignatureProvider(sig) != SignatureProviderUnknown {
		return true
	}
	return IsValidGrokEncryptedContent(sig)
}
