package common

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

func IsGeminiThoughtPart(part ojson.Result) bool {
	return part.Get("thought").Bool()
}

func MergeAdjacentGeminiContents(contents [][]byte) [][]byte {
	if len(contents) <= 1 {
		return contents
	}
	merged := make([][]byte, 0, len(contents))
	for _, content := range contents {
		if len(content) == 0 {
			continue
		}
		role := ojson.GetBytesNoCopy(content, "role").String()
		partsResult := ojson.GetBytesNoCopy(content, "parts")
		if !partsResult.IsArray() || len(partsResult.Array()) == 0 {
			continue
		}
		if len(merged) > 0 {
			lastIndex := len(merged) - 1
			lastJSON := merged[lastIndex]
			lastRole := ojson.GetBytesNoCopy(lastJSON, "role").String()
			if lastRole == "user" && role == "user" {
				lastParts := ojson.GetBytesNoCopy(lastJSON, "parts").Array()
				combinedParts := make([][]byte, 0, len(lastParts)+len(partsResult.Array()))
				for _, p := range lastParts {
					combinedParts = append(combinedParts, []byte(p.Raw))
				}
				for _, p := range partsResult.Array() {
					combinedParts = append(combinedParts, []byte(p.Raw))
				}
				combinedParts = ReorderGeminiUserParts(combinedParts)
				updated, err := ojson.SetRawBytes(lastJSON, "parts", JoinRawArray(combinedParts))
				if err == nil {
					merged[lastIndex] = updated
					continue
				}
			}
		}
		merged = append(merged, content)
	}
	return merged
}

func ContentHasGeminiFunctionCall(content []byte) bool {
	return GeminiPartsHaveFunctionCall(ojson.GetBytesNoCopy(content, "parts"))
}

func GeminiPartsHaveFunctionCall(parts ojson.Result) bool {
	hasFC := false
	parts.ForEach(func(_, part ojson.Result) bool {
		if part.Get("functionCall").Exists() || part.Get("function_call").Exists() {
			hasFC = true
			return false
		}
		return true
	})
	return hasFC
}

func ContentHasGeminiFunctionResponse(content []byte) bool {
	return GeminiPartsHaveFunctionResponse(ojson.GetBytesNoCopy(content, "parts"))
}

func GeminiPartsHaveFunctionResponse(parts ojson.Result) bool {
	hasFR := false
	parts.ForEach(func(_, part ojson.Result) bool {
		if part.Get("functionResponse").Exists() || part.Get("function_response").Exists() {
			hasFR = true
			return false
		}
		return true
	})
	return hasFR
}

func GeminiPartFunctionCall(part ojson.Result) ojson.Result {
	if fc := part.Get("functionCall"); fc.Exists() {
		return fc
	}
	return part.Get("function_call")
}

func GeminiPartFunctionResponse(part ojson.Result) ojson.Result {
	if fr := part.Get("functionResponse"); fr.Exists() {
		return fr
	}
	return part.Get("function_response")
}

func NormalizeGeminiTurnRole(rawRole string, parts ojson.Result, previousRole string) string {
	switch strings.ToLower(strings.TrimSpace(rawRole)) {
	case "model", "assistant":
		return "assistant"
	case "user", "function", "tool":
		return "user"
	default:
		if GeminiPartsHaveFunctionCall(parts) {
			return "assistant"
		}
		if GeminiPartsHaveFunctionResponse(parts) {
			return "user"
		}
		if previousRole == "user" {
			return "assistant"
		}
		return "user"
	}
}

func ReorderGeminiUserParts(parts [][]byte) [][]byte {
	hasFR := false
	hasTrailingText := false
	for _, p := range parts {
		isFR := ojson.GetBytesNoCopy(p, "functionResponse").Exists() || ojson.GetBytesNoCopy(p, "function_response").Exists()
		if isFR {
			hasFR = true
		} else if hasFR && ojson.GetBytesNoCopy(p, "text").Exists() {
			hasTrailingText = true
			break
		}
	}
	if !hasFR || !hasTrailingText {
		return parts
	}

	promptParts := make([][]byte, 0, len(parts))
	toolParts := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if ojson.GetBytesNoCopy(p, "text").Exists() {
			promptParts = append(promptParts, p)
		} else {
			toolParts = append(toolParts, p)
		}
	}
	return append(promptParts, toolParts...)
}

func MergeAdjacentGeminiUserContents(contents [][]byte) [][]byte {
	if len(contents) <= 1 {
		return contents
	}
	merged := make([][]byte, 0, len(contents))
	for _, content := range contents {
		if len(content) == 0 {
			continue
		}
		role := ojson.GetBytesNoCopy(content, "role").String()
		partsResult := ojson.GetBytesNoCopy(content, "parts")
		if !partsResult.IsArray() || partsResult.Raw == "[]" || !partsResult.Get("0").Exists() {
			continue
		}
		if len(merged) > 0 {
			lastIndex := len(merged) - 1
			lastJSON := merged[lastIndex]
			lastRole := ojson.GetBytesNoCopy(lastJSON, "role").String()
			if lastRole == "user" && role == "user" && !ContentHasGeminiFunctionResponse(lastJSON) && !ContentHasGeminiFunctionResponse(content) {
				lastParts := ojson.GetBytesNoCopy(lastJSON, "parts").Array()
				currentParts := partsResult.Array()
				combinedParts := make([][]byte, 0, len(lastParts)+len(currentParts))
				for _, p := range lastParts {
					combinedParts = append(combinedParts, []byte(p.Raw))
				}
				for _, p := range currentParts {
					combinedParts = append(combinedParts, []byte(p.Raw))
				}
				updated, err := ojson.SetRawBytes(lastJSON, "parts", JoinRawArray(combinedParts))
				if err == nil {
					merged[lastIndex] = updated
					continue
				}
			}
		}
		merged = append(merged, content)
	}
	return merged
}

func SplitGeminiFunctionResponseTurns(contents [][]byte) [][]byte {
	if len(contents) == 0 {
		return contents
	}
	split := make([][]byte, 0, len(contents))
	for _, content := range contents {
		if ojson.GetBytesNoCopy(content, "role").String() != "user" || !ContentHasGeminiFunctionResponse(content) {
			split = append(split, content)
			continue
		}
		parts := ojson.GetBytesNoCopy(content, "parts").Array()
		responseParts := make([][]byte, 0, len(parts))
		otherParts := make([][]byte, 0, len(parts))
		for _, part := range parts {
			if part.Get("functionResponse").Exists() || part.Get("function_response").Exists() {
				responseParts = append(responseParts, []byte(part.Raw))
			} else {
				otherParts = append(otherParts, []byte(part.Raw))
			}
		}
		var responseTurn, otherTurn []byte
		var responseErr, otherErr error
		if len(responseParts) > 0 {
			responseTurn, responseErr = ojson.SetRawBytes(content, "parts", JoinRawArray(responseParts))
		}
		if len(otherParts) > 0 {
			otherTurn, otherErr = ojson.SetRawBytes(content, "parts", JoinRawArray(otherParts))
		}
		if responseErr != nil || otherErr != nil {

			split = append(split, content)
			continue
		}
		if len(responseTurn) > 0 {
			split = append(split, responseTurn)
		}
		if len(otherTurn) > 0 {
			split = append(split, otherTurn)
		}
	}

	out := make([][]byte, 0, len(split))
	n := len(split)
	for i := 0; i < n; {
		if ojson.GetBytesNoCopy(split[i], "role").String() != "user" {
			out = append(out, split[i])
			i++
			continue
		}
		precedingModelHasFC := len(out) > 0 &&
			ojson.GetBytesNoCopy(out[len(out)-1], "role").String() == "model" &&
			ContentHasGeminiFunctionCall(out[len(out)-1])

		j := i
		hasFR := false
		for j < n && ojson.GetBytesNoCopy(split[j], "role").String() == "user" {
			if ContentHasGeminiFunctionResponse(split[j]) {
				hasFR = true
			}
			j++
		}
		userRun := split[i:j]
		if precedingModelHasFC && hasFR && len(userRun) > 1 {
			var combinedFRParts [][]byte
			otherTurns := make([][]byte, 0, len(userRun))
			for _, turn := range userRun {
				if ContentHasGeminiFunctionResponse(turn) {
					parts := ojson.GetBytesNoCopy(turn, "parts").Array()
					for _, p := range parts {
						combinedFRParts = append(combinedFRParts, []byte(p.Raw))
					}
				} else {
					otherTurns = append(otherTurns, turn)
				}
			}
			if len(combinedFRParts) > 0 {
				frTurn := []byte(`{"role":"user","parts":[]}`)
				frTurn, _ = ojson.SetRawBytes(frTurn, "parts", JoinRawArray(combinedFRParts))
				out = append(out, frTurn)
			}
			out = append(out, otherTurns...)
		} else {
			out = append(out, userRun...)
		}
		i = j
	}
	return out
}

func ContainsJSONRef(value ojson.Result) bool {
	if !value.IsObject() && !value.IsArray() {
		return false
	}
	found := false
	value.ForEach(func(key, child ojson.Result) bool {
		if value.IsObject() && key.String() == "$ref" && child.Type == ojson.String {
			found = true
			return false
		}
		if ContainsJSONRef(child) {
			found = true
			return false
		}
		return true
	})
	return found
}

func SetGeminiFunctionResponseResult(part []byte, path string, result ojson.Result) []byte {
	if !result.Exists() {
		part, _ = ojson.SetBytes(part, path, "")
		return part
	}
	if ContainsJSONRef(result) {
		targetPath := path
		if strings.HasSuffix(path, "response") {
			targetPath = path + ".result"
		}
		part, _ = ojson.SetBytes(part, targetPath, result.Raw)
		return part
	}
	part, _ = ojson.SetRawBytes(part, path, []byte(result.Raw))
	return part
}

func SetGeminiFunctionResponseRaw(part []byte, path string, rawJSON string) []byte {
	trimmed := strings.TrimSpace(rawJSON)
	if trimmed == "" {
		part, _ = ojson.SetBytes(part, path, "")
		return part
	}
	return SetGeminiFunctionResponseResult(part, path, ojson.Parse(trimmed))
}

func SetGeminiFunctionResponseResultDoc(doc *ojson.Doc, path string, result ojson.Result) {
	if doc == nil {
		return
	}
	if !result.Exists() {
		_ = doc.Set(path, "")
		return
	}
	if ContainsJSONRef(result) {
		targetPath := path
		if strings.HasSuffix(path, "response") {
			targetPath = path + ".result"
		}
		_ = doc.Set(targetPath, result.Raw)
		return
	}
	_ = doc.SetRaw(path, result.Raw)
}

func SetGeminiFunctionResponseRawDoc(doc *ojson.Doc, path string, rawJSON string) {
	if doc == nil {
		return
	}
	trimmed := strings.TrimSpace(rawJSON)
	if trimmed == "" {
		_ = doc.Set(path, "")
		return
	}
	SetGeminiFunctionResponseResultDoc(doc, path, ojson.Parse(trimmed))
}
