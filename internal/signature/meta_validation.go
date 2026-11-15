package signature

import (
	"bytes"
	"fmt"
	"github.com/MoeForge/moego/base64"
	"strings"
	"unicode/utf8"
)




const MaxMetaReasoningSignatureLen = 32 * 1024 * 1024

const (
	metaReasoningSignatureOverheadLen = 45
	metaReasoningSignatureMinPayload  = 1024



	maxMetaRouterSuffixLen = 128
)










var metaReasoningSignatureHeader = []byte{0x43, 0xe3, 0xda, 0x0e, 0x01}



type MetaReasoningSignatureInfo struct {
	DecodedLen int


	PayloadLen     int
	BucketExponent int



	RouterSuffix string
}

func IsValidMetaReasoningSignature(rawSignature string) bool {
	_, err := InspectMetaReasoningSignature(rawSignature)
	return err == nil
}










func InspectMetaReasoningSignature(rawSignature string) (*MetaReasoningSignatureInfo, error) {
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return nil, fmt.Errorf("empty Meta reasoning signature")
	}
	if len(sig) > MaxMetaReasoningSignatureLen {
		return nil, fmt.Errorf("Meta reasoning signature exceeds maximum length (%d bytes)", MaxMetaReasoningSignatureLen)
	}

	if !strings.HasPrefix(sig, "Q-PaDg") {
		return nil, fmt.Errorf("invalid Meta reasoning signature: expected Q-PaDg prefix")
	}
	native := sig
	routerSuffix := ""
	if head, tail, ok := strings.Cut(sig, "."); ok {



		if !isValidMetaRouterSuffix(tail) {
			return nil, fmt.Errorf("invalid Meta reasoning signature: router suffix must be 1-%d alphanumeric characters", maxMetaRouterSuffixLen)
		}
		native = head
		routerSuffix = tail
	}
	info, err := inspectNativeMetaReasoningSignature(native)
	if err != nil {
		return nil, err
	}
	info.RouterSuffix = routerSuffix
	return info, nil
}



func inspectNativeMetaReasoningSignature(sig string) (*MetaReasoningSignatureInfo, error) {



	if !strings.HasPrefix(sig, "Q-PaDg") {
		return nil, fmt.Errorf("invalid Meta reasoning signature: expected Q-PaDg prefix")
	}


	if len(sig)%4 == 1 {
		return nil, fmt.Errorf("invalid Meta reasoning signature: base64url length %d cannot decode", len(sig))
	}
	decodedLen := len(sig) / 4 * 3
	switch len(sig) % 4 {
	case 2:
		decodedLen++
	case 3:
		decodedLen += 2
	}
	payloadLen := decodedLen - metaReasoningSignatureOverheadLen
	if payloadLen < metaReasoningSignatureMinPayload || payloadLen&(payloadLen-1) != 0 {
		return nil, fmt.Errorf("invalid Meta reasoning signature: payload length %d is not a power-of-two bucket", payloadLen)
	}
	if index, r, ok := firstInvalidMetaReasoningSignatureChar(sig); ok {
		return nil, fmt.Errorf("invalid Meta reasoning signature: contains non-base64url character U+%04X at byte %d", r, index)
	}


	decoded, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("invalid Meta reasoning signature: base64url decode failed")
	}
	if !bytes.Equal(decoded[:len(metaReasoningSignatureHeader)], metaReasoningSignatureHeader) {
		return nil, fmt.Errorf("invalid Meta reasoning signature: unexpected envelope header %x", decoded[:len(metaReasoningSignatureHeader)])
	}
	exponent := 0
	for p := payloadLen; p > metaReasoningSignatureMinPayload; p >>= 1 {
		exponent++
	}
	return &MetaReasoningSignatureInfo{
		DecodedLen:     len(decoded),
		PayloadLen:     payloadLen,
		BucketExponent: exponent,
	}, nil
}




var metaReasoningSignatureCharSet = base64AlphabetSet("-_")

func firstInvalidMetaReasoningSignatureChar(sig string) (int, rune, bool) {
	for index := 0; index < len(sig); index++ {
		if !metaReasoningSignatureCharSet[sig[index]] {
			r, _ := utf8.DecodeRuneInString(sig[index:])
			return index, r, true
		}
	}
	return 0, 0, false
}




var metaRouterSuffixCharSet = base64AlphabetSet("")

func isValidMetaRouterSuffix(suffix string) bool {
	if len(suffix) == 0 || len(suffix) > maxMetaRouterSuffixLen {
		return false
	}
	for index := 0; index < len(suffix); index++ {
		if !metaRouterSuffixCharSet[suffix[index]] {
			return false
		}
	}
	return true
}
