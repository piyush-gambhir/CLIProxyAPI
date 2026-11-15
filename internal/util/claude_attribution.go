package util

import (
	"strings"
	"unicode"

	"github.com/MoeForge/moego/ojson"
)

const claudeCodeAttributionSystemPrefix = "x-anthropic-billing-header:"



func IsClaudeCodeAttributionSystemText(text string) bool {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	return strings.HasPrefix(text, claudeCodeAttributionSystemPrefix)
}





func StripClaudeCodeAttributionSystem(payload []byte) []byte {
	system := ojson.GetBytesNoCopy(payload, "system")
	if !system.Exists() {
		return payload
	}
	if system.Type == ojson.String {
		if !IsClaudeCodeAttributionSystemText(system.String()) {
			return payload
		}
		updated, errDelete := ojson.DeleteBytes(payload, "system")
		if errDelete != nil {
			return payload
		}
		return updated
	}
	if !system.IsArray() {
		return payload
	}
	kept := make([]string, 0, len(system.Array()))
	removed := false
	system.ForEach(func(_, block ojson.Result) bool {
		if block.Get("type").String() == "text" && IsClaudeCodeAttributionSystemText(block.Get("text").String()) {
			removed = true
			return true
		}
		if block.Raw != "" {
			kept = append(kept, block.Raw)
		}
		return true
	})
	if !removed {
		return payload
	}
	if len(kept) == 0 {
		updated, errDelete := ojson.DeleteBytes(payload, "system")
		if errDelete != nil {
			return payload
		}
		return updated
	}
	updated, errSet := ojson.SetRawBytes(payload, "system", []byte("["+strings.Join(kept, ",")+"]"))
	if errSet != nil {
		return payload
	}
	return updated
}
