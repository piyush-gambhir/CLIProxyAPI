package helps

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const (
	responsesToolSearchType       = "tool_search"
	responsesToolSearchCallType   = "tool_search_call"
	responsesToolSearchOutputType = "tool_search_output"
)

func ResponsesHasToolSearch(payload []byte) bool {
	if hasClientToolSearchDeclaration(ojson.GetBytesNoCopy(payload, "tools")) {
		return true
	}
	found := false
	ojson.GetBytesNoCopy(payload, "input").ForEach(func(_, item ojson.Result) bool {
		switch strings.TrimSpace(item.Get("type").String()) {
		case responsesToolSearchCallType, responsesToolSearchOutputType:
			found = true
		case "additional_tools":
			found = hasClientToolSearchDeclaration(item.Get("tools"))
		}
		return !found
	})
	return found
}

func StripResponsesToolSearch(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	if !StripResponsesToolSearchDoc(doc) {
		return payload
	}
	return doc.Bytes()
}

func StripResponsesToolSearchDoc(doc *ojson.Doc) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	modified := false
	if tools := doc.Get("tools"); tools.IsArray() {
		if kept, changed := withoutClientToolSearch(tools); changed {
			if errSet := doc.SetRawBytes("tools", kept); errSet == nil {
				modified = true
			}
		}
	}
	input := doc.Get("input")
	if !input.IsArray() {
		return modified
	}
	dropped := make(map[int]bool)
	for idx, item := range input.Array() {
		switch strings.TrimSpace(item.Get("type").String()) {
		case responsesToolSearchCallType, responsesToolSearchOutputType:
			dropped[idx] = true
			modified = true
		case "additional_tools":
			if kept, keptChanged := withoutClientToolSearch(item.Get("tools")); keptChanged {
				if errSet := doc.SetRawBytes(fmt.Sprintf("input.%d.tools", idx), kept); errSet == nil {
					modified = true
				}
			}
		}
	}
	if len(dropped) > 0 {
		updatedItems := doc.Get("input").Array()
		items := make([]string, 0, len(updatedItems)-len(dropped))
		for idx, item := range updatedItems {
			if !dropped[idx] {
				items = append(items, item.Raw)
			}
		}
		_ = doc.SetRaw("input", "["+strings.Join(items, ",")+"]")
	}
	return modified
}

func isClientToolSearchDeclaration(tool ojson.Result) bool {
	return strings.TrimSpace(tool.Get("type").String()) == responsesToolSearchType &&
		strings.TrimSpace(tool.Get("execution").String()) == "client"
}

func hasClientToolSearchDeclaration(tools ojson.Result) bool {
	found := false
	tools.ForEach(func(_, tool ojson.Result) bool {
		found = isClientToolSearchDeclaration(tool)
		return !found
	})
	return found
}

func withoutClientToolSearch(tools ojson.Result) ([]byte, bool) {
	if !tools.IsArray() {
		return nil, false
	}
	changed := false
	kept := make([]string, 0, len(tools.Array()))
	tools.ForEach(func(_, tool ojson.Result) bool {
		if isClientToolSearchDeclaration(tool) {
			changed = true
			return true
		}
		kept = append(kept, tool.Raw)
		return true
	})
	return []byte("[" + strings.Join(kept, ",") + "]"), changed
}
