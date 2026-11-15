package htmlsanitize

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"mime"
	"strings"
)


func String(value string) string {
	return html.EscapeString(value)
}


func Strings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, String(value))
	}
	return out
}


func JSONBody(body []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return body, false
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if errDecode := decoder.Decode(&value); errDecode != nil {
		return body, false
	}
	var extra any
	if errExtra := decoder.Decode(&extra); errExtra != io.EOF {
		return body, false
	}

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if errEncode := encoder.Encode(JSONValue(value)); errEncode != nil {
		return body, false
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), true
}


func JSONBodyIfLikely(body []byte, contentType string) ([]byte, bool) {
	if IsJSONContentType(contentType) || LooksLikeJSON(body) {
		return JSONBody(body)
	}
	return body, false
}


func JSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return String(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = JSONValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = JSONValue(item)
		}
		return out
	default:
		return value
	}
}


func IsJSONContentType(contentType string) bool {
	mediaType, _, errParse := mime.ParseMediaType(strings.TrimSpace(contentType))
	if errParse != nil {
		mediaType = strings.TrimSpace(contentType)
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}


func LooksLikeJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}
