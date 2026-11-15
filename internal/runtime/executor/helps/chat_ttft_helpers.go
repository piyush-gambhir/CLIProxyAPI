package helps

import (
	"bytes"

	"github.com/MoeForge/moego/ojson"
)

func IsChatTokenEvent(payload []byte) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}

	if bytes.Equal(payload, []byte("data: [DONE]")) || bytes.Equal(payload, []byte("[DONE]")) {
		return true
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
		if bytes.Equal(payload, []byte("[DONE]")) {
			return true
		}
	}

	if ojson.GetBytesNoCopy(payload, "error.message").Exists() || ojson.GetBytesNoCopy(payload, "error").Exists() {
		return true
	}

	choices := ojson.GetBytesNoCopy(payload, "choices").Array()
	if len(choices) == 0 {
		return false
	}

	for _, choice := range choices {

		delta := choice.Get("delta")
		if delta.Exists() {
			if len(delta.Get("content").String()) > 0 {
				return true
			}
			if len(delta.Get("reasoning_content").String()) > 0 {
				return true
			}
			if len(delta.Get("reasoning").String()) > 0 {
				return true
			}
			if len(delta.Get("refusal").String()) > 0 {
				return true
			}
			for _, tc := range delta.Get("tool_calls").Array() {
				if len(tc.Get("function.arguments").String()) > 0 || len(tc.Get("function.name").String()) > 0 {
					return true
				}
				if len(tc.Get("custom.input").String()) > 0 {
					return true
				}
			}
		}

		message := choice.Get("message")
		if message.Exists() {
			if len(message.Get("content").String()) > 0 {
				return true
			}
			if len(message.Get("reasoning_content").String()) > 0 {
				return true
			}
			if len(message.Get("refusal").String()) > 0 {
				return true
			}
			for _, tc := range message.Get("tool_calls").Array() {
				if len(tc.Get("function.arguments").String()) > 0 || len(tc.Get("function.name").String()) > 0 {
					return true
				}
			}
		}

		if len(choice.Get("finish_reason").String()) > 0 {
			return true
		}
	}

	return false
}

func ObserveChatTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter != nil && len(payload) > 0 {
		reporter.ObserveResponseModel(payload)
	}
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsChatTokenEvent(payload))
}
