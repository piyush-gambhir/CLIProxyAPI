package signature

import (
	"fmt"
	"github.com/MoeForge/moego/base64"
	"strings"
	"unicode/utf8"
)

const MaxGPTReasoningSignatureLen = 32 * 1024 * 1024

type GPTReasoningSignatureInfo struct {
	DecodedLen    int
	CiphertextLen int

	RouterSuffix string
}

func IsValidGPTReasoningSignature(rawSignature string) bool {
	_, err := InspectGPTReasoningSignature(rawSignature)
	return err == nil
}

func InspectGPTReasoningSignature(rawSignature string) (*GPTReasoningSignatureInfo, error) {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return nil, fmt.Errorf("empty GPT reasoning signature")
	}
	if len(sig) > MaxGPTReasoningSignatureLen {
		return nil, fmt.Errorf("GPT reasoning signature exceeds maximum length (%d bytes)", MaxGPTReasoningSignatureLen)
	}

	if !strings.HasPrefix(sig, "gAAAA") {
		return nil, fmt.Errorf("invalid GPT reasoning signature: expected gAAAA prefix")
	}
	native := sig
	routerSuffix := ""
	if head, tail, ok := strings.Cut(sig, "."); ok {
		if !isValidMetaRouterSuffix(tail) {
			return nil, fmt.Errorf("invalid GPT reasoning signature: router suffix must be 1-%d alphanumeric characters", maxMetaRouterSuffixLen)
		}
		native = head
		routerSuffix = tail
	}
	if index, r, ok := firstInvalidGPTReasoningSignatureChar(native); ok {
		return nil, fmt.Errorf("invalid GPT reasoning signature: contains non-base64url character U+%04X at byte %d", r, index)
	}

	decoded, err := decodeGPTReasoningSignature(native)
	if err != nil {
		return nil, err
	}
	if len(decoded) < 73 {
		return nil, fmt.Errorf("invalid GPT reasoning signature: decoded payload too short")
	}
	if decoded[0] != 0x80 {
		return nil, fmt.Errorf("invalid GPT reasoning signature: expected version 0x80, got 0x%02x", decoded[0])
	}

	ciphertextLen := len(decoded) - 1 - 8 - 16 - 32
	if ciphertextLen <= 0 || ciphertextLen%16 != 0 {
		return nil, fmt.Errorf("invalid GPT reasoning signature: ciphertext length %d is not a positive AES block multiple", ciphertextLen)
	}

	return &GPTReasoningSignatureInfo{
		DecodedLen:    len(decoded),
		CiphertextLen: ciphertextLen,
		RouterSuffix:  routerSuffix,
	}, nil
}

func decodeGPTReasoningSignature(sig string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(sig); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(sig); err == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf("invalid GPT reasoning signature: base64url decode failed")
}

var gptReasoningSignatureCharSet = base64AlphabetSet("-_=")

func firstInvalidGPTReasoningSignatureChar(sig string) (int, rune, bool) {
	for index := 0; index < len(sig); index++ {
		if !gptReasoningSignatureCharSet[sig[index]] {
			r, _ := utf8.DecodeRuneInString(sig[index:])
			return index, r, true
		}
	}
	return 0, 0, false
}
