package helps

import (
	"bytes"

	"github.com/MoeForge/moego/ojson"
)




func IsClaudeTokenEvent(payload []byte) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return false
	}


	if bytes.HasPrefix(payload, []byte("event:")) {
		if idx := bytes.IndexByte(payload, '\n'); idx != -1 {
			payload = bytes.TrimSpace(payload[idx+1:])
		}
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

	eventType := ojson.GetBytesNoCopy(payload, "type").String()
	switch eventType {

	case "content_block_delta":
		delta := ojson.GetBytesNoCopy(payload, "delta")
		if len(delta.Get("text").String()) > 0 {
			return true
		}
		if len(delta.Get("thinking").String()) > 0 {
			return true
		}
		if len(delta.Get("partial_json").String()) > 0 {
			return true
		}
		if len(delta.Get("signature").String()) > 0 {
			return true
		}
		return false


	case "content_block_start":
		cb := ojson.GetBytesNoCopy(payload, "content_block")
		if len(cb.Get("text").String()) > 0 || len(cb.Get("thinking").String()) > 0 {
			return true
		}
		return false


	case "message_delta":
		return len(ojson.GetBytesNoCopy(payload, "delta.stop_reason").String()) > 0
	case "message_stop", "error":
		return true


	case "message_start", "ping", "content_block_stop":
		return false

	default:

		if ojson.GetBytesNoCopy(payload, "content").IsArray() {
			for _, block := range ojson.GetBytesNoCopy(payload, "content").Array() {
				if len(block.Get("text").String()) > 0 || len(block.Get("thinking").String()) > 0 {
					return true
				}
				if block.Get("type").String() == "tool_use" && len(block.Get("name").String()) > 0 {
					return true
				}
			}
		}
		return false
	}
}




func ObserveClaudeTokenEvent(reporter *UsageReporter, payload []byte) {
	if reporter != nil && len(payload) > 0 {
		reporter.ObserveResponseModel(payload)
	}
	if reporter == nil || len(payload) == 0 {
		return
	}
	if reporter.IsTTFTSet() {
		return
	}
	reporter.ObserveTokenEvent(IsClaudeTokenEvent(payload))
}
