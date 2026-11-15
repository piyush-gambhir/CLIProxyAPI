package signature

import (
	"fmt"
	"github.com/MoeForge/moego/base64"
	"strings"
)





func InspectAntigravityClaudeCAQSSignature(rawSignature string) (*ClaudeCAISSignatureInfo, error) {
	sig := stripClaudeSignaturePrefix(rawSignature)
	if len(sig) == 0 || len(sig) > MaxClaudeThinkingSignatureLen {
		return nil, fmt.Errorf("invalid Antigravity CAQS signature length")
	}
	if sig[0] != 'Q' || strings.ContainsAny(sig, "\r\n") {
		return nil, fmt.Errorf("invalid Antigravity CAQS wrapper")
	}
	inner, errDecode := base64.StdEncoding.Strict().DecodeString(sig)
	if errDecode != nil {
		return nil, fmt.Errorf("invalid Antigravity CAQS encoding: %w", errDecode)
	}
	innerText := string(inner)
	if len(innerText) == 0 || innerText[0] != 'C' || strings.ContainsAny(innerText, " \t\r\n#") {
		return nil, fmt.Errorf("invalid Antigravity CAQS inner encoding")
	}
	decoded, errInner := base64.StdEncoding.Strict().DecodeString(innerText)
	if errInner != nil {
		return nil, fmt.Errorf("invalid Antigravity CAQS inner encoding: %w", errInner)
	}
	info, errInspect := inspectClaudeCAISPayload(decoded, true)
	if errInspect != nil {
		return nil, fmt.Errorf("invalid Antigravity CAQS payload: %w", errInspect)
	}



	if info.EnvelopeVersion != 4 ||
		info.Infrastructure == nil || *info.Infrastructure != 2 ||
		info.BlockKind != "thinking" || !info.SignatureInContainer {
		return nil, fmt.Errorf("unsupported Antigravity CAQS envelope or channel schema")
	}
	return info, nil
}
