package util

import (
	"github.com/MoeForge/moego/ojson"
)

type ClaudeToolResultImage struct {
	MimeType string
	Data     string
}

type ClaudeToolResult struct {
	Result string

	ResultIsRaw bool

	IsError bool

	Images []ClaudeToolResultImage
}

func ConvertClaudeToolResult(block ojson.Result) ClaudeToolResult {
	result := ConvertClaudeToolResultContent(block.Get("content"))
	result.IsError = block.Get("is_error").Type == ojson.True
	return result
}

func ConvertClaudeToolResultContent(content ojson.Result) ClaudeToolResult {
	switch {
	case content.Type == ojson.String:
		return ClaudeToolResult{Result: content.String()}
	case content.IsArray():
		var images []ClaudeToolResultImage
		nonImageCount := 0
		lastNonImageRaw := ""
		filteredDoc := ojson.NewDoc([]byte(`[]`))
		content.ForEach(func(_, block ojson.Result) bool {
			if isClaudeBase64Image(block) {
				if img, ok := claudeImageFromBlock(block); ok {
					images = append(images, img)
				}
				return true
			}
			nonImageCount++
			lastNonImageRaw = block.Raw
			_ = filteredDoc.SetRaw("-1", block.Raw)
			return true
		})
		switch {
		case nonImageCount == 1:
			return ClaudeToolResult{Result: lastNonImageRaw, ResultIsRaw: true, Images: images}
		case nonImageCount > 1:
			return ClaudeToolResult{Result: filteredDoc.String(), ResultIsRaw: true, Images: images}
		default:
			return ClaudeToolResult{Images: images}
		}
	case content.IsObject():
		if isClaudeBase64Image(content) {
			if img, ok := claudeImageFromBlock(content); ok {
				return ClaudeToolResult{Images: []ClaudeToolResultImage{img}}
			}
			return ClaudeToolResult{}
		}
		return ClaudeToolResult{Result: content.Raw, ResultIsRaw: true}
	case content.Raw != "":
		return ClaudeToolResult{Result: content.Raw, ResultIsRaw: true}
	default:
		return ClaudeToolResult{}
	}
}

func isClaudeBase64Image(block ojson.Result) bool {
	return block.Get("type").String() == "image" && block.Get("source.type").String() == "base64"
}

func claudeImageFromBlock(block ojson.Result) (ClaudeToolResultImage, bool) {
	data := block.Get("source.data").String()
	if data == "" {
		return ClaudeToolResultImage{}, false
	}
	return ClaudeToolResultImage{
		MimeType: block.Get("source.media_type").String(),
		Data:     data,
	}, true
}
