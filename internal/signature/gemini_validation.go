



































































package signature

import (
	"fmt"
	"github.com/MoeForge/moego/base64"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	MaxGeminiThoughtSignatureLen = 32 * 1024 * 1024

	GeminiSkipThoughtSignatureValidator = "skip_thought_signature_validator"
	GeminiContextEngineeringBypass      = "context_engineering_is_the_way_to_go"
)





type GeminiThoughtSignatureValidationOptions struct {


	AllowBypassSentinel bool



	RequireKnownEnvelope bool





	RequireObservedMarker bool
}

type GeminiThoughtSignatureEnvelope string

const (
	GeminiThoughtSignatureEnvelopeUnknown GeminiThoughtSignatureEnvelope = "unknown"




	GeminiThoughtSignatureEnvelopeProtobufField2 GeminiThoughtSignatureEnvelope = "protobuf_field_2"








	GeminiThoughtSignatureEnvelopeBareTink  GeminiThoughtSignatureEnvelope = "bare_tink"
	GeminiThoughtSignatureEnvelopeASCIIUUID GeminiThoughtSignatureEnvelope = "ascii_uuid"
)



type GeminiThoughtSignatureInfo struct {
	IsBypassSentinel  bool
	BypassSentinel    string
	DecodedLen        int
	FirstByte         byte
	HasObservedMarker bool
	KnownEnvelope     bool
	Envelope          GeminiThoughtSignatureEnvelope
	RecordCount       int
	OpaquePayloadLen  int
}

type geminiFunctionCallRef struct {
	id   string
	name string
	path string
}

type geminiFunctionResponseRef struct {
	part ojson.Result
	path string
}

func geminiThoughtSignatureValidationOptions(opts []GeminiThoughtSignatureValidationOptions) GeminiThoughtSignatureValidationOptions {
	if len(opts) == 0 {
		return GeminiThoughtSignatureValidationOptions{}
	}
	return opts[0]
}




func IsGeminiThoughtSignatureBypass(rawSignature string) bool {
	switch strings.TrimSpace(rawSignature) {
	case GeminiSkipThoughtSignatureValidator, GeminiContextEngineeringBypass:
		return true
	default:
		return false
	}
}



func IsValidGeminiThoughtSignature(rawSignature string, opts ...GeminiThoughtSignatureValidationOptions) bool {
	_, err := InspectGeminiThoughtSignature(rawSignature, opts...)
	return err == nil
}




func InspectGeminiThoughtSignature(rawSignature string, opts ...GeminiThoughtSignatureValidationOptions) (*GeminiThoughtSignatureInfo, error) {
	opt := geminiThoughtSignatureValidationOptions(opts)
	sig := strings.TrimSpace(rawSignature)
	if sig == "" {
		return nil, fmt.Errorf("empty Gemini thought signature")
	}

	if IsValidClaudeCAISSignature(sig) {
		return nil, fmt.Errorf("invalid Gemini thought signature: detected Claude CAIS signature")
	}

	if IsGeminiThoughtSignatureBypass(sig) {
		if !opt.AllowBypassSentinel {
			return nil, fmt.Errorf("Gemini thought signature bypass sentinel is not allowed")
		}
		return &GeminiThoughtSignatureInfo{
			IsBypassSentinel: true,
			BypassSentinel:   sig,
		}, nil
	}

	decoded, err := decodeGeminiThoughtSignature(sig)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("invalid Gemini thought signature: empty decoded payload")
	}

	info := &GeminiThoughtSignatureInfo{
		DecodedLen:        len(decoded),
		FirstByte:         decoded[0],
		HasObservedMarker: decoded[0] == 0x12,
	}
	info.Envelope, info.KnownEnvelope = classifyGeminiThoughtSignatureEnvelope(decoded)
	info.RecordCount, info.OpaquePayloadLen = inspectGeminiEnvelope(decoded, info.Envelope)
	if opt.RequireKnownEnvelope && !info.KnownEnvelope {
		return nil, fmt.Errorf("invalid Gemini thought signature: unknown envelope %q", info.Envelope)
	}
	if opt.RequireObservedMarker && !info.HasObservedMarker {
		return nil, fmt.Errorf("invalid Gemini thought signature: expected observed marker 0x12, got 0x%02x", info.FirstByte)
	}

	return info, nil
}






func ValidateGeminiThoughtSignatures(inputRawJSON []byte, opts ...GeminiThoughtSignatureValidationOptions) error {
	contents, contentsPath := geminiContents(inputRawJSON)
	if !contents.IsArray() {
		return nil
	}

	contentResults := contents.Array()
	for i := 0; i < len(contentResults); i++ {
		content := contentResults[i]
		parts := content.Get("parts")
		if !parts.IsArray() {
			continue
		}

		isModelTurn := strings.EqualFold(strings.TrimSpace(content.Get("role").String()), "model")
		firstFunctionCallSeen := false
		partResults := parts.Array()
		for j := 0; j < len(partResults); j++ {
			part := partResults[j]
			hasFunctionCall := part.Get("functionCall").Exists()
			isFirstFunctionCall := isModelTurn && hasFunctionCall && !firstFunctionCallSeen
			if isModelTurn && (hasFunctionCall || !part.Get("thought").Bool()) {
				firstFunctionCallSeen = hasFunctionCall
			}
			rawSignature, hasSignature := geminiPartThoughtSignature(part)
			if !hasFunctionCall && !hasSignature {
				continue
			}

			partPath := fmt.Sprintf("%s[%d].parts[%d]", contentsPath, i, j)
			rawSignature = strings.TrimSpace(rawSignature)
			if part.Get("functionResponse").Exists() && hasSignature {
				return fmt.Errorf("%s: functionResponse must not carry thoughtSignature", partPath)
			}
			if rawSignature == "" {
				if isFirstFunctionCall {
					return fmt.Errorf("%s: missing thoughtSignature on first functionCall", partPath)
				}
				if hasSignature {
					return fmt.Errorf("%s: empty thoughtSignature", partPath)
				}
				continue
			}
			if IsGeminiThoughtSignatureBypass(rawSignature) && !isFirstFunctionCall {
				return fmt.Errorf("%s: Gemini bypass sentinel is allowed only on the first model functionCall", partPath)
			}
			if !hasNormalizedGeminiPartThoughtSignature(part, rawSignature) {
				return fmt.Errorf("%s: thoughtSignature must use one canonical top-level field", partPath)
			}
			if _, err := InspectGeminiThoughtSignature(rawSignature, opts...); err != nil {
				return fmt.Errorf("%s: %w", partPath, err)
			}
		}
	}

	return nil
}






func ValidateGeminiFunctionCallPairing(inputRawJSON []byte) error {
	contents, contentsPath := geminiContents(inputRawJSON)
	if !contents.IsArray() {
		return nil
	}

	var pending []geminiFunctionCallRef
	var validationErr error
	contents.ForEach(func(contentIndex, content ojson.Result) bool {
		i := int(contentIndex.Int())
		parts := content.Get("parts")
		if !parts.IsArray() || parts.Raw == "[]" || !parts.Get("0").Exists() {
			if len(pending) > 0 {
				validationErr = fmt.Errorf(
					"%s[%d]: content appears before %d pending functionResponse part(s)",
					contentsPath,
					i,
					len(pending),
				)
			}
			return validationErr == nil
		}

		var calls []geminiFunctionCallRef
		var responses []geminiFunctionResponseRef
		parts.ForEach(func(partIndex, part ojson.Result) bool {
			j := int(partIndex.Int())
			partPath := fmt.Sprintf("%s[%d].parts[%d]", contentsPath, i, j)
			if call := part.Get("functionCall"); call.Exists() {
				if call.Get("name").String() == "" {
					validationErr = fmt.Errorf("%s: missing functionCall.name", partPath)
					return false
				}
				calls = append(calls, geminiFunctionCallRef{
					id:   call.Get("id").String(),
					name: call.Get("name").String(),
					path: partPath,
				})
			}
			if response := part.Get("functionResponse"); response.Exists() {
				responses = append(responses, geminiFunctionResponseRef{
					part: part,
					path: partPath,
				})
			}
			return true
		})
		if validationErr != nil {
			return false
		}

		switch {
		case len(calls) > 0 && len(responses) > 0:
			validationErr = fmt.Errorf(
				"%s[%d]: functionCall and functionResponse parts must not be interleaved in the same content",
				contentsPath,
				i,
			)
		case len(calls) > 0 && len(pending) > 0:
			validationErr = fmt.Errorf(
				"%s[%d]: functionCall appears before %d pending functionResponse part(s)",
				contentsPath,
				i,
				len(pending),
			)
		case len(calls) > 0:
			pending = calls
			return true
		case len(responses) == 0 && len(pending) > 0:



			role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
			if role == "model" {
				validationErr = fmt.Errorf(
					"%s[%d]: model content appears before %d pending functionResponse part(s)",
					contentsPath,
					i,
					len(pending),
				)
			}
			return validationErr == nil
		case len(responses) == 0:
			return true
		case len(pending) == 0:
			validationErr = fmt.Errorf("%s[%d]: functionResponse without preceding functionCall", contentsPath, i)
		case len(responses) != len(pending):
			validationErr = fmt.Errorf(
				"%s[%d]: functionResponse count %d does not match pending functionCall count %d",
				contentsPath,
				i,
				len(responses),
				len(pending),
			)
		}
		if validationErr != nil {
			return false
		}

		for responseIndex, responseRef := range responses {
			partPath := responseRef.path
			response := responseRef.part.Get("functionResponse")
			call := pending[responseIndex]
			responseID := response.Get("id").String()
			responseName := response.Get("name").String()

			switch {
			case call.id != "" && responseID == "":
				validationErr = fmt.Errorf("%s: missing functionResponse.id for %s", partPath, call.path)
			case call.id != "" && responseID != call.id:
				validationErr = fmt.Errorf(
					"%s: functionResponse.id %q does not match functionCall.id %q at %s",
					partPath,
					responseID,
					call.id,
					call.path,
				)
			case responseName == "":
				validationErr = fmt.Errorf("%s: missing functionResponse.name", partPath)
			case call.name != "" && responseName != call.name:
				validationErr = fmt.Errorf(
					"%s: functionResponse.name %q does not match functionCall.name %q at %s",
					partPath,
					responseName,
					call.name,
					call.path,
				)
			}
			if validationErr != nil {
				return false
			}
		}

		pending = nil
		return true
	})
	return validationErr
}

func decodeGeminiThoughtSignature(sig string) ([]byte, error) {
	if len(sig) > MaxGeminiThoughtSignatureLen {
		return nil, fmt.Errorf("Gemini thought signature exceeds maximum length (%d bytes)", MaxGeminiThoughtSignatureLen)
	}

	decoded, err := base64.StdEncoding.DecodeString(sig)
	if err == nil {
		return decoded, nil
	}
	if decoded, rawErr := base64.RawStdEncoding.DecodeString(sig); rawErr == nil {
		return decoded, nil
	}

	return nil, fmt.Errorf("invalid Gemini thought signature: base64 decode failed: %w", err)
}

func classifyGeminiThoughtSignatureEnvelope(decoded []byte) (GeminiThoughtSignatureEnvelope, bool) {
	if len(decoded) == 0 {
		return GeminiThoughtSignatureEnvelopeUnknown, false
	}
	if isASCIIUUIDBytes(decoded) {
		return GeminiThoughtSignatureEnvelopeASCIIUUID, false
	}
	if isGeminiField2Envelope(decoded) {
		return GeminiThoughtSignatureEnvelopeProtobufField2, true
	}
	if isGeminiBareTinkEnvelope(decoded) {
		return GeminiThoughtSignatureEnvelopeBareTink, true
	}
	return GeminiThoughtSignatureEnvelopeUnknown, false
}

func isGeminiField2Envelope(decoded []byte) bool {
	info, ok := inspectGeminiField2Envelope(decoded)
	return ok && info.RecordCount == 1 && info.OpaquePayloadLen > 0
}

func inspectGeminiEnvelope(decoded []byte, envelope GeminiThoughtSignatureEnvelope) (recordCount int, opaquePayloadLen int) {
	switch envelope {
	case GeminiThoughtSignatureEnvelopeProtobufField2:
		if info, ok := inspectGeminiField2Envelope(decoded); ok {
			return info.RecordCount, info.OpaquePayloadLen
		}
	case GeminiThoughtSignatureEnvelopeBareTink:


		if isGeminiBareTinkEnvelope(decoded) {
			return 1, len(decoded)
		}
	}
	return 0, 0
}




const geminiBareTinkMinLen = 1 + 4 + 16








var geminiVertexBareTinkKeyIDs = [][4]byte{
	{0x8f, 0x3d, 0x6b, 0x5f},
}




func isGeminiBareTinkEnvelope(decoded []byte) bool {
	if len(decoded) < geminiBareTinkMinLen || decoded[0] != 0x01 {
		return false
	}
	keyID := [4]byte(decoded[1:5])
	for _, known := range geminiVertexBareTinkKeyIDs {
		if keyID == known {
			return true
		}
	}
	return false
}




func IsGeminiVertexBareTinkSignature(rawSignature string) bool {
	info, err := InspectGeminiThoughtSignature(
		SignaturePayloadWithoutProviderPrefix(rawSignature),
		GeminiThoughtSignatureValidationOptions{RequireKnownEnvelope: true},
	)
	return err == nil && info.Envelope == GeminiThoughtSignatureEnvelopeBareTink
}

type geminiEnvelopeInfo struct {
	RecordCount      int
	OpaquePayloadLen int
}

func inspectGeminiField2Envelope(decoded []byte) (geminiEnvelopeInfo, bool) {
	value, ok := consumeGeminiField2Field1Value(decoded)
	if !ok || (!isLikelyGeminiOpaquePayload(value) && !isASCIIUUIDBytes(value) && !isLikelyGeminiToolInvocationPayload(value)) {
		return geminiEnvelopeInfo{}, false
	}
	return geminiEnvelopeInfo{
		RecordCount:      1,
		OpaquePayloadLen: len(value),
	}, true
}

func consumeGeminiField2Field1Value(decoded []byte) ([]byte, bool) {
	num, typ, n := protowire.ConsumeTag(decoded)
	if n < 0 || num != 2 || typ != protowire.BytesType {
		return nil, false
	}
	offset := n
	container, n := protowire.ConsumeBytes(decoded[offset:])
	if n < 0 {
		return nil, false
	}
	offset += n
	if offset != len(decoded) {
		return nil, false
	}

	num, typ, n = protowire.ConsumeTag(container)
	if n < 0 || num != 1 || typ != protowire.BytesType {
		return nil, false
	}
	containerOffset := n
	value, n := protowire.ConsumeBytes(container[containerOffset:])
	if n < 0 {
		return nil, false
	}
	containerOffset += n
	if containerOffset != len(container) {
		return nil, false
	}
	return value, true
}

func isLikelyGeminiOpaquePayload(value []byte) bool {













	return len(value) > 0 && value[0] == 0x01
}

func isLikelyGeminiToolInvocationPayload(value []byte) bool {






	if len(value) == 0 {
		return false
	}
	offset := 0
	hasTinkField := false
	for offset < len(value) {
		_, typ, n := protowire.ConsumeTag(value[offset:])
		if n < 0 {
			return false
		}
		offset += n
		switch typ {
		case protowire.VarintType:
			_, n = protowire.ConsumeVarint(value[offset:])
			if n < 0 {
				return false
			}
			offset += n
		case protowire.BytesType:
			bytesVal, n := protowire.ConsumeBytes(value[offset:])
			if n < 0 {
				return false
			}
			offset += n
			if isLikelyGeminiOpaquePayload(bytesVal) {
				hasTinkField = true
			}
		case protowire.Fixed32Type:
			_, n = protowire.ConsumeFixed32(value[offset:])
			if n < 0 {
				return false
			}
			offset += n
		case protowire.Fixed64Type:
			_, n = protowire.ConsumeFixed64(value[offset:])
			if n < 0 {
				return false
			}
			offset += n
		default:
			return false
		}
	}
	return hasTinkField && offset == len(value)
}

func isASCIIUUIDBytes(decoded []byte) bool {
	if len(decoded) != 36 {
		return false
	}
	for i, b := range decoded {
		switch i {
		case 8, 13, 18, 23:
			if b != '-' {
				return false
			}
		default:
			if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')) {
				return false
			}
		}
	}
	return true
}

func geminiContents(inputRawJSON []byte) (ojson.Result, string) {
	if contents := util.GetGJSONBytesNoCopy(inputRawJSON, "contents"); contents.Exists() {
		return contents, "contents"
	}
	return util.GetGJSONBytesNoCopy(inputRawJSON, "request.contents"), "request.contents"
}
