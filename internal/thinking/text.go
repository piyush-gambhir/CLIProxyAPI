package thinking

import (
	"github.com/MoeForge/moego/ojson"
)







func GetThinkingText(part ojson.Result) string {

	if text := part.Get("text"); text.Exists() && text.Type == ojson.String {
		return text.String()
	}


	thinkingField := part.Get("thinking")
	if !thinkingField.Exists() {
		return ""
	}


	if thinkingField.Type == ojson.String {
		return thinkingField.String()
	}


	if thinkingField.IsObject() {
		if inner := thinkingField.Get("text"); inner.Exists() && inner.Type == ojson.String {
			return inner.String()
		}
		if inner := thinkingField.Get("thinking"); inner.Exists() && inner.Type == ojson.String {
			return inner.String()
		}
	}

	return ""
}
