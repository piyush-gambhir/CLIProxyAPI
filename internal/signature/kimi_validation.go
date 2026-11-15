package signature

import (
	"fmt"
	"github.com/MoeForge/moego/base64"
	"strings"
)















































const (


	KimiThinkingSignatureNonStreamingLen = 12946


	KimiThinkingSignatureStreamingLen = 4340
)



type KimiThinkingSignatureMode string

const (
	KimiThinkingSignatureModeNonStreaming KimiThinkingSignatureMode = "non_streaming"
	KimiThinkingSignatureModeStreaming    KimiThinkingSignatureMode = "streaming"
)





var kimiThinkingSignatureLens = map[int]KimiThinkingSignatureMode{
	KimiThinkingSignatureNonStreamingLen: KimiThinkingSignatureModeNonStreaming,
	KimiThinkingSignatureStreamingLen:    KimiThinkingSignatureModeStreaming,
}





const MinKimiThinkingSignatureEntropyRatio = 0.85


type KimiThinkingSignatureInfo struct {
	RawLen     int
	DecodedLen int
	Mode       KimiThinkingSignatureMode
}









func InspectKimiThinkingSignature(raw string) (*KimiThinkingSignatureInfo, error) {
	sig := strings.TrimSpace(raw)
	if sig == "" {
		return nil, fmt.Errorf("empty Kimi thinking signature")
	}
	if sig != raw {
		return nil, fmt.Errorf("Kimi thinking signature has leading or trailing whitespace")
	}
	mode, ok := kimiThinkingSignatureLens[len(sig)]
	if !ok {
		return nil, fmt.Errorf("invalid Kimi thinking signature: unexpected length %d", len(sig))
	}
	if strings.Contains(sig, "=") {
		return nil, fmt.Errorf("invalid Kimi thinking signature: expected unpadded standard base64")
	}
	if index, r, ok := firstInvalidGrokEncryptedContentChar(sig); ok {
		return nil, fmt.Errorf("invalid Kimi thinking signature: contains non-base64 character U+%04X at byte %d", r, index)
	}
	if _, _, ok := SplitSignatureProviderPrefix(sig); ok {
		return nil, fmt.Errorf("invalid Kimi thinking signature: carries another provider's cache prefix")
	}




	if maybeSelfDescribingSignatureEnvelope(sig) {
		if strings.HasPrefix(sig, "gAAAA") {
			return nil, fmt.Errorf("Kimi thinking signature looks like GPT/Codex reasoning signature")
		}
		if IsValidClaudeCAISSignature(sig) {
			return nil, fmt.Errorf("Kimi thinking signature looks like Claude CAIS thinking signature")
		}
		if IsValidClaudeThinkingSignature(sig, ClaudeSignatureValidationOptions{Strict: true}) {
			return nil, fmt.Errorf("Kimi thinking signature looks like Claude thinking signature")
		}
		if IsValidGeminiThoughtSignature(sig, GeminiThoughtSignatureValidationOptions{RequireKnownEnvelope: true}) {
			return nil, fmt.Errorf("Kimi thinking signature looks like Gemini thoughtSignature")
		}
	}
	decoded, err := base64.RawStdEncoding.DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("invalid Kimi thinking signature: base64 decode failed: %w", err)
	}
	if entropyRatio := byteEntropyRatio(decoded); entropyRatio < MinKimiThinkingSignatureEntropyRatio {
		return nil, fmt.Errorf("invalid Kimi thinking signature: decoded payload entropy ratio %.3f below %.3f", entropyRatio, MinKimiThinkingSignatureEntropyRatio)
	}
	return &KimiThinkingSignatureInfo{
		RawLen:     len(sig),
		DecodedLen: len(decoded),
		Mode:       mode,
	}, nil
}



func IsValidKimiThinkingSignature(raw string) bool {
	_, err := InspectKimiThinkingSignature(raw)
	return err == nil
}
