package helps

import (
	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
)

var emptyGeminiUserTurnJSON = []byte(`{"role":"user","parts":[{"text":""}]}`)

func EnsureGeminiLeadingUserContent(payload []byte, path string) []byte {
	if len(payload) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	if !EnsureGeminiLeadingUserContentDoc(doc, path) {
		return payload
	}
	return doc.Bytes()
}

func EnsureGeminiLeadingUserContentDoc(doc *ojson.Doc, path string) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	firstRole := doc.Get(path + ".0.role")
	if firstRole.String() != "model" {
		return false
	}
	contents := doc.Get(path)
	if !contents.IsArray() {
		return false
	}
	contentArray := contents.Array()
	if len(contentArray) == 0 {
		return false
	}

	contentItems := make([][]byte, 0, len(contentArray)+1)
	contentItems = append(contentItems, emptyGeminiUserTurnJSON)
	for _, content := range contentArray {
		contentItems = append(contentItems, []byte(content.Raw))
	}

	return doc.SetRawBytes(path, translatorcommon.JoinRawArray(contentItems)) == nil
}

func contentHasFunctionResponse(content ojson.Result) bool {
	parts := content.Get("parts")
	if !parts.IsArray() {
		return false
	}
	for _, part := range parts.Array() {
		if part.Get("functionResponse").Exists() {
			return true
		}
	}
	return false
}

func contentHasFunctionResponseMedia(content ojson.Result) bool {
	parts := content.Get("parts")
	if !parts.IsArray() {
		return false
	}
	for _, part := range parts.Array() {
		for _, nested := range part.Get("functionResponse.parts").Array() {
			if nested.Get("inlineData").Exists() || nested.Get("inline_data").Exists() ||
				nested.Get("fileData").Exists() || nested.Get("file_data").Exists() {
				return true
			}
		}
	}
	return false
}

func EnsureGeminiTrailingUserContent(payload []byte, path string) []byte {
	if len(payload) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	if !EnsureGeminiTrailingUserContentDoc(doc, path) {
		return payload
	}
	return doc.Bytes()
}

func EnsureGeminiTrailingUserContentDoc(doc *ojson.Doc, path string) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	contents := doc.Get(path)
	if !contents.IsArray() {
		return false
	}
	contentArray := contents.Array()
	if len(contentArray) == 0 {
		return false
	}
	lastContent := contentArray[len(contentArray)-1]
	lastRole := lastContent.Get("role").String()
	if lastRole != "model" && lastRole != "assistant" {
		return false
	}
	if contentHasFunctionResponse(lastContent) && !contentHasFunctionResponseMedia(lastContent) {
		return false
	}

	contentItems := make([][]byte, 0, len(contentArray)+1)
	for _, content := range contentArray {
		contentItems = append(contentItems, []byte(content.Raw))
	}
	contentItems = append(contentItems, emptyGeminiUserTurnJSON)

	return doc.SetRawBytes(path, translatorcommon.JoinRawArray(contentItems)) == nil
}

func DropGeminiTrailingThoughtOnlyContent(payload []byte, path string) []byte {
	if len(payload) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	if !DropGeminiTrailingThoughtOnlyContentDoc(doc, path) {
		return payload
	}
	return doc.Bytes()
}

func DropGeminiTrailingThoughtOnlyContentDoc(doc *ojson.Doc, path string) bool {
	if doc == nil || doc.Len() == 0 {
		return false
	}
	contents := doc.Get(path)
	if !contents.IsArray() {
		return false
	}
	contentArray := contents.Array()
	keep := len(contentArray)
	for keep > 1 && contentIsThoughtOnlyModelTurn(contentArray[keep-1]) {
		keep--
	}
	if keep == len(contentArray) {
		return false
	}

	contentItems := make([][]byte, 0, keep)
	for _, content := range contentArray[:keep] {
		contentItems = append(contentItems, []byte(content.Raw))
	}
	return doc.SetRawBytes(path, translatorcommon.JoinRawArray(contentItems)) == nil
}

func contentIsThoughtOnlyModelTurn(content ojson.Result) bool {
	role := content.Get("role").String()
	if role != "model" && role != "assistant" {
		return false
	}
	parts := content.Get("parts").Array()
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !part.Get("thought").Bool() || part.Get("functionCall").Exists() || part.Get("functionResponse").Exists() {
			return false
		}
	}
	return true
}

func EnsureGeminiBoundaryUserContent(payload []byte, path string) []byte {
	if len(payload) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	if !EnsureGeminiBoundaryUserContentDoc(doc, path) {
		return payload
	}
	return doc.Bytes()
}

func EnsureGeminiBoundaryUserContentDoc(doc *ojson.Doc, path string) bool {
	leading := EnsureGeminiLeadingUserContentDoc(doc, path)
	trailing := EnsureGeminiTrailingUserContentDoc(doc, path)
	return leading || trailing
}
