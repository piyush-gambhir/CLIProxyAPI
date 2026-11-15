package thinking

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

func isResponsesFormat(format string) bool {
	return format == "codex" || format == "openai-response"
}

func extractConfigurationUpdateConfig(body []byte) ThinkingConfig {
	if len(body) == 0 || !ojson.ValidBytes(body) {
		return ThinkingConfig{}
	}
	return extractConfigurationUpdateConfigFromInput(ojson.GetBytesNoCopy(body, "input"))
}

func extractConfigurationUpdateConfigDoc(doc *ojson.Doc, docValid bool) ThinkingConfig {
	if doc == nil || !docValid {
		return ThinkingConfig{}
	}
	return extractConfigurationUpdateConfigFromInput(doc.Get("input"))
}

func extractConfigurationUpdateConfigFromInput(input ojson.Result) ThinkingConfig {
	if !input.IsArray() {
		return ThinkingConfig{}
	}

	var effort string
	input.ForEach(func(_, item ojson.Result) bool {
		if item.Get("type").String() == "configuration_update" {
			value := item.Get("reasoning.effort")
			if value.Type == ojson.String {
				if normalized := strings.ToLower(strings.TrimSpace(value.String())); normalized != "" {
					effort = normalized
				}
			}
		}
		return true
	})
	switch effort {
	case "":
		return ThinkingConfig{}
	case "none":
		return ThinkingConfig{Mode: ModeNone, Budget: 0}
	case "auto":
		return ThinkingConfig{Mode: ModeAuto, Budget: -1}
	default:
		return ThinkingConfig{Mode: ModeLevel, Level: ThinkingLevel(effort)}
	}
}

func CollapseConfigurationUpdates(body []byte, keepTopLevelEffort ...bool) []byte {
	if len(body) == 0 || !ojson.ValidBytes(body) {
		return body
	}
	doc := ojson.NewDoc(body)
	if !collapseConfigurationUpdatesDoc(doc, true, keepTopLevelEffort...) {
		return body
	}
	return doc.Bytes()
}

func CollapseConfigurationUpdatesDoc(doc *ojson.Doc, keepTopLevelEffort ...bool) {
	_ = collapseConfigurationUpdatesDoc(doc, true, keepTopLevelEffort...)
}

func collapseConfigurationUpdatesDoc(doc *ojson.Doc, docValid bool, keepTopLevelEffort ...bool) bool {
	if doc == nil || !docValid {
		return false
	}
	effort := ""
	if !(len(keepTopLevelEffort) > 0 && keepTopLevelEffort[0]) {
		effort = reasoningEffortFromConfig(extractConfigurationUpdateConfigDoc(doc, docValid))
	}
	modified := stripConfigurationUpdatesDoc(doc, docValid)
	if effort != "" {
		if doc.Set("reasoning.effort", effort) == nil {
			modified = true
		}
	}
	return modified
}

func StripConfigurationUpdates(body []byte) []byte {
	return stripConfigurationUpdates(body)
}

func StripConfigurationUpdatesDoc(doc *ojson.Doc) {
	_ = stripConfigurationUpdatesDoc(doc, true)
}

func stripConfigurationUpdates(body []byte) []byte {
	if len(body) == 0 || !ojson.ValidBytes(body) {
		return body
	}
	doc := ojson.NewDoc(body)
	if !stripConfigurationUpdatesDoc(doc, true) {
		return body
	}
	return doc.Bytes()
}

func stripConfigurationUpdatesDoc(doc *ojson.Doc, docValid bool) bool {
	if doc == nil || !docValid {
		return false
	}
	input := doc.Get("input")
	if !input.IsArray() {
		return false
	}

	var kept []string
	removed := false
	input.ForEach(func(_, item ojson.Result) bool {
		if item.Get("type").String() == "configuration_update" {
			removed = true
		} else {
			kept = append(kept, item.Raw)
		}
		return true
	})
	if !removed {
		return false
	}
	return doc.SetRaw("input", "["+strings.Join(kept, ",")+"]") == nil
}

func stripResponsesEffort(body []byte) []byte {
	if len(body) == 0 || !ojson.ValidBytes(body) {
		return body
	}
	doc := ojson.NewDoc(body)
	if !stripResponsesEffortDoc(doc, true) {
		return body
	}
	return doc.Bytes()
}

func stripResponsesEffortDoc(doc *ojson.Doc, docValid bool) bool {
	if doc == nil || !docValid || !doc.Get("reasoning.effort").Exists() {
		return false
	}
	if errDelete := doc.Delete("reasoning.effort"); errDelete != nil {
		return false
	}
	if reasoning := doc.Get("reasoning"); reasoning.IsObject() && len(reasoning.Map()) == 0 {
		_ = doc.Delete("reasoning")
	}
	return true
}
