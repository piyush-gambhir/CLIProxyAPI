package signature

import (
	"fmt"
	"github.com/MoeForge/moego/base64"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	MaxGrokEncryptedContentLen = 8 * 1024 * 1024

	MinGrokEncryptedContentDecodedLen = 32

	MinGrokEncryptedContentEntropyRatio = 0.85
)

type GrokEncryptedContentInfo struct {
	RawLen     int
	DecodedLen int

	RouterSuffix string
}

func InspectGrokEncryptedContent(raw string) (*GrokEncryptedContentInfo, error) {
	sig := strings.TrimSpace(raw)
	if sig == "" {
		return nil, fmt.Errorf("empty Grok encrypted_content")
	}
	if len(sig) > MaxGrokEncryptedContentLen {
		return nil, fmt.Errorf("Grok encrypted_content exceeds maximum length (%d bytes)", MaxGrokEncryptedContentLen)
	}
	if sig != raw {
		return nil, fmt.Errorf("Grok encrypted_content has leading or trailing whitespace")
	}
	if strings.Contains(sig, "=") {
		return nil, fmt.Errorf("invalid Grok encrypted_content: expected unpadded standard base64")
	}
	if _, _, ok := SplitSignatureProviderPrefix(sig); ok {
		return nil, fmt.Errorf("invalid Grok encrypted_content: carries another provider's cache prefix")
	}
	native := sig
	routerSuffix := ""
	if head, tail, ok := strings.Cut(sig, "."); ok {
		if !isValidMetaRouterSuffix(tail) {
			return nil, fmt.Errorf("invalid Grok encrypted_content: router suffix must be 1-%d alphanumeric characters", maxMetaRouterSuffixLen)
		}
		native = head
		routerSuffix = tail
	}
	if index, r, ok := firstInvalidGrokEncryptedContentChar(native); ok {
		return nil, fmt.Errorf("invalid Grok encrypted_content: contains non-base64 character U+%04X at byte %d", r, index)
	}

	if maybeSelfDescribingSignatureEnvelope(native) {
		if strings.HasPrefix(native, "gAAAA") {
			return nil, fmt.Errorf("Grok encrypted_content looks like GPT/Codex reasoning signature")
		}
		if IsValidMetaReasoningSignature(native) {
			return nil, fmt.Errorf("Grok encrypted_content looks like Meta reasoning signature")
		}
		if IsValidClaudeThinkingSignature(native, ClaudeSignatureValidationOptions{Strict: true}) {
			return nil, fmt.Errorf("Grok encrypted_content looks like Claude thinking signature")
		}
		if IsValidClaudeCAISSignature(native) {
			return nil, fmt.Errorf("Grok encrypted_content looks like Claude CAIS thinking signature")
		}
		if _, err := InspectGeminiThoughtSignature(native, GeminiThoughtSignatureValidationOptions{RequireKnownEnvelope: true}); err == nil {
			return nil, fmt.Errorf("Grok encrypted_content looks like Gemini thoughtSignature")
		}
	}

	if IsValidKimiThinkingSignature(native) {
		return nil, fmt.Errorf("Grok encrypted_content has a Kimi thinking signature length")
	}

	decoded, err := decodeGrokEncryptedContent(native)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(string(decoded), "cpa-codex-gemini-carrier-v1:") {
		return nil, fmt.Errorf("invalid Grok encrypted_content: carries Gemini Codex thoughtSignature carrier")
	}
	if len(decoded) < MinGrokEncryptedContentDecodedLen {
		return nil, fmt.Errorf("invalid Grok encrypted_content: decoded payload too short (%d bytes)", len(decoded))
	}
	if entropyRatio := byteEntropyRatio(decoded); entropyRatio < MinGrokEncryptedContentEntropyRatio {
		return nil, fmt.Errorf("invalid Grok encrypted_content: decoded payload entropy ratio %.3f below %.3f", entropyRatio, MinGrokEncryptedContentEntropyRatio)
	}
	return &GrokEncryptedContentInfo{
		RawLen:       len(sig),
		DecodedLen:   len(decoded),
		RouterSuffix: routerSuffix,
	}, nil
}

func IsValidGrokEncryptedContent(raw string) bool {
	_, err := InspectGrokEncryptedContent(raw)
	return err == nil
}

func decodeGrokEncryptedContent(sig string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("invalid Grok encrypted_content: base64 decode failed: %w", err)
	}
	return decoded, nil
}

var grokEncryptedContentCharSet = base64AlphabetSet("+/")

func firstInvalidGrokEncryptedContentChar(sig string) (int, rune, bool) {
	for index := 0; index < len(sig); index++ {
		if !grokEncryptedContentCharSet[sig[index]] {
			r, _ := utf8.DecodeRuneInString(sig[index:])
			return index, r, true
		}
	}
	return 0, 0, false
}

func byteEntropyRatio(buf []byte) float64 {
	if len(buf) == 0 {
		return 0
	}
	var counts [256]int
	for _, b := range buf {
		counts[b]++
	}
	n := float64(len(buf))
	entropy := 0.0
	for _, count := range counts {
		if count == 0 {
			continue
		}
		p := float64(count) / n
		entropy -= p * math.Log2(p)
	}
	maxSymbols := len(buf)
	if maxSymbols > 256 {
		maxSymbols = 256
	}
	if maxSymbols <= 1 {
		return 0
	}
	return entropy / math.Log2(float64(maxSymbols))
}
