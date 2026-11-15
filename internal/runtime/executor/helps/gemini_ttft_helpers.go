package helps

import (
	"bytes"

	"github.com/MoeForge/moego/ojson"
)




func IsGeminiTokenEvent(payload []byte) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}


	if bytes.HasPrefix(payload, []byte("data:")) {
		prefixLen := len("data:")
		if bytes.HasPrefix(payload, []byte("data: ")) {
			prefixLen = len("data: ")
		}
		payload = bytes.TrimSpace(payload[prefixLen:])
		if len(payload) == 0 {
			return false
		}
	}


	if ojson.GetBytesNoCopy(payload, "error.message").Exists() || ojson.GetBytesNoCopy(payload, "error").Exists() || ojson.GetBytesNoCopy(payload, "response.error").Exists() {
		return true
	}

	candidatesNode := ojson.GetBytesNoCopy(payload, "candidates")
	if !candidatesNode.Exists() {
		candidatesNode = ojson.GetBytesNoCopy(payload, "response.candidates")
	}
	candidates := candidatesNode.Array()
	if len(candidates) == 0 {
		return false
	}

	for _, candidate := range candidates {

		parts := candidate.Get("content.parts").Array()
		for _, part := range parts {
			if len(part.Get("text").String()) > 0 {
				return true
			}
			if len(part.Get("thoughtText").String()) > 0 {
				return true
			}
			if thought := part.Get("thought"); thought.Type == ojson.String && len(thought.String()) > 0 {
				return true
			}
			if len(part.Get("functionCall.name").String()) > 0 {
				return true
			}
			if len(part.Get("inlineData.data").String()) > 0 {
				return true
			}
		}


		if len(candidate.Get("finishReason").String()) > 0 {
			return true
		}
	}

	return false
}




func ObserveGeminiTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter != nil && len(payload) > 0 {
		reporter.ObserveResponseModel(payload)
	}
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsGeminiTokenEvent(payload))
}
