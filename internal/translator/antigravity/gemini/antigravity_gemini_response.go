package gemini

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

type convertAntigravityGeminiParams struct {
	SawResponse     bool
	SawFinishReason bool
	ModelVersion    string
	ResponseID      string
	UsageMetadata   []byte
}

func (p *convertAntigravityGeminiParams) observe(rawJSON []byte) {
	if !p.SawResponse {
		p.SawResponse = hasAntigravityResponsePayload(rawJSON)
	}
	if !p.SawFinishReason {
		for _, path := range []string{"response.candidates", "candidates"} {
			for _, candidate := range ojson.GetBytesNoCopy(rawJSON, path).Array() {
				if candidate.Get("finishReason").String() != "" {
					p.SawFinishReason = true
					break
				}
			}
			if p.SawFinishReason {
				break
			}
		}
	}
	if p.ModelVersion == "" {
		p.ModelVersion = firstStringValue(rawJSON, "response.modelVersion", "modelVersion")
	}
	if p.ResponseID == "" {
		p.ResponseID = firstStringValue(rawJSON, "response.responseId", "responseId")
	}

	for _, path := range []string{"response.usageMetadata", "response.cpaUsageMetadata", "usageMetadata", "cpaUsageMetadata"} {
		if usage := ojson.GetBytesNoCopy(rawJSON, path); usage.Exists() {
			p.UsageMetadata = append(p.UsageMetadata[:0], usage.Raw...)
			break
		}
	}
}

func (p *convertAntigravityGeminiParams) syntheticTerminalChunk() []byte {
	chunkDoc := ojson.NewDoc([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}]}`))
	if len(p.UsageMetadata) > 0 {
		_ = chunkDoc.SetRawBytes("usageMetadata", p.UsageMetadata)
	}
	if p.ModelVersion != "" {
		_ = chunkDoc.Set("modelVersion", p.ModelVersion)
	}
	if p.ResponseID != "" {
		_ = chunkDoc.Set("responseId", p.ResponseID)
	}
	return chunkDoc.Bytes()
}

func hasAntigravityResponsePayload(rawJSON []byte) bool {
	for _, path := range []string{"response.candidates", "candidates"} {
		if candidates := ojson.GetBytesNoCopy(rawJSON, path); candidates.IsArray() && len(candidates.Array()) > 0 {
			return true
		}
	}

	for _, path := range []string{
		"response.usageMetadata", "response.cpaUsageMetadata",
		"usageMetadata", "cpaUsageMetadata",
	} {
		if usage := ojson.GetBytesNoCopy(rawJSON, path); usage.IsObject() && len(usage.Map()) > 0 {
			return true
		}
	}
	return false
}

func firstStringValue(rawJSON []byte, paths ...string) string {
	for _, path := range paths {
		if value := ojson.GetBytesNoCopy(rawJSON, path).String(); value != "" {
			return strings.Clone(value)
		}
	}
	return ""
}

func ConvertAntigravityResponseToGemini(ctx context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}

	var state *convertAntigravityGeminiParams
	if param != nil {
		if *param == nil {
			*param = &convertAntigravityGeminiParams{}
		}
		if s, ok := (*param).(*convertAntigravityGeminiParams); ok {
			state = s
		}
	}

	if bytes.Equal(rawJSON, []byte("[DONE]")) {

		if state == nil || !state.SawResponse || state.SawFinishReason {
			return [][]byte{}
		}
		state.SawFinishReason = true
		finalChunk := state.syntheticTerminalChunk()
		if alt, ok := ctx.Value("alt").(string); ok && alt != "" {
			finalChunk, _ = ojson.SetRawBytes([]byte("[]"), "-1", finalChunk)
		}
		return [][]byte{finalChunk}
	}

	if state != nil {
		state.observe(rawJSON)
	}

	if alt, ok := ctx.Value("alt").(string); ok {
		var chunk []byte
		if alt == "" {
			responseResult := util.GetGJSONBytesNoCopy(rawJSON, "response")
			if responseResult.Exists() {
				if responseResult.Index > 0 && responseResult.Index+len(responseResult.Raw) <= len(rawJSON) {
					chunk = rawJSON[responseResult.Index : responseResult.Index+len(responseResult.Raw)]
				} else {
					chunk = []byte(responseResult.Raw)
				}
				chunk = restoreUsageMetadata(chunk)
				chunk = restoreGeminiFunctionNames(chunk, originalRequestRawJSON)
			}
		} else {
			chunkDoc := ojson.NewDoc([]byte("[]"))
			responseResult := util.ParseGJSONBytesNoCopy(rawJSON)
			if responseResult.IsArray() {
				responseResultItems := responseResult.Array()
				for i := 0; i < len(responseResultItems); i++ {
					responseResultItem := responseResultItems[i]
					if responseResultItem.Get("response").Exists() {
						_ = chunkDoc.SetRaw("-1", responseResultItem.Get("response").Raw)
					}
				}
			}
			chunk = chunkDoc.Bytes()
		}
		return [][]byte{chunk}
	}
	return [][]byte{}
}

var cpaUsageMetadataMarker = []byte(`"cpaUsageMetadata"`)

func ConvertAntigravityResponseToGeminiNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) []byte {
	var chunk []byte
	responseResult := util.GetGJSONBytesNoCopy(rawJSON, "response")
	if responseResult.Exists() {
		if responseResult.Index > 0 && responseResult.Index+len(responseResult.Raw) <= len(rawJSON) {
			chunk = rawJSON[responseResult.Index : responseResult.Index+len(responseResult.Raw)]
		} else {
			chunk = []byte(responseResult.Raw)
		}
		chunk = restoreUsageMetadata(chunk)
		chunk = restoreGeminiFunctionNames(chunk, originalRequestRawJSON)
	} else {
		chunk = restoreGeminiFunctionNames(rawJSON, originalRequestRawJSON)
	}
	candidates := util.GetGJSONBytesNoCopy(chunk, "candidates")
	if candidates.IsArray() {

		var chunkDoc *ojson.Doc
		idx := 0
		candidates.ForEach(func(_, candidate ojson.Result) bool {
			if candidate.Get("finishReason").String() == "" {
				if chunkDoc == nil {
					chunkDoc = ojson.NewDoc(chunk)
				}
				_ = chunkDoc.Set(fmt.Sprintf("candidates.%d.finishReason", idx), "STOP")
			}
			idx++
			return true
		})
		if chunkDoc != nil {
			chunk = chunkDoc.Bytes()
		}
	}
	return chunk
}

func restoreGeminiFunctionNames(chunk, originalRequestRawJSON []byte) []byte {
	nameMap := util.DisambiguatedToolNameMap(originalRequestRawJSON)
	if len(nameMap) == 0 {
		return chunk
	}
	var chunkDoc *ojson.Doc
	candidates := ojson.GetBytesNoCopy(chunk, "candidates")
	for candidateIndex, candidate := range candidates.Array() {
		for partIndex, part := range candidate.Get("content.parts").Array() {
			for _, field := range []string{"functionCall", "functionResponse", "function_call", "function_response"} {
				nameResult := part.Get(field + ".name")
				name := nameResult.String()
				if name == "" {
					continue
				}
				restoredName := util.RestoreSanitizedToolName(nameMap, name)
				if nameResult.Type == ojson.String && restoredName == name {
					continue
				}
				if chunkDoc == nil {
					chunkDoc = ojson.NewDoc(chunk)
				}
				path := fmt.Sprintf("candidates.%d.content.parts.%d.%s.name", candidateIndex, partIndex, field)
				_ = chunkDoc.Set(path, restoredName)
			}
		}
	}
	if chunkDoc != nil {
		return chunkDoc.Bytes()
	}
	return chunk
}

func GeminiTokenCount(ctx context.Context, count int64) []byte {
	return translatorcommon.GeminiTokenCountJSON(count)
}

func restoreUsageMetadata(chunk []byte) []byte {
	if !bytes.Contains(chunk, cpaUsageMetadataMarker) {
		return chunk
	}
	if cpaUsage := util.GetGJSONBytesNoCopy(chunk, "cpaUsageMetadata"); cpaUsage.Exists() {
		if cpaUsage.Index > 0 && cpaUsage.Index <= len(chunk) {
			searchStart := cpaUsage.Index - 36
			if searchStart < 0 {
				searchStart = 0
			}
			if rel := bytes.LastIndex(chunk[searchStart:cpaUsage.Index], cpaUsageMetadataMarker); rel >= 0 {
				keyPos := searchStart + rel + 1
				out := make([]byte, 0, len(chunk)-3)
				out = append(out, chunk[:keyPos]...)
				out = append(out, "usageMetadata"...)
				out = append(out, chunk[keyPos+len("cpaUsageMetadata"):]...)
				return out
			}
		}
		chunkDoc := ojson.NewDoc(chunk)
		_ = chunkDoc.SetRaw("usageMetadata", cpaUsage.Raw)
		_ = chunkDoc.Delete("cpaUsageMetadata")
		chunk = chunkDoc.Bytes()
	}
	return chunk
}
