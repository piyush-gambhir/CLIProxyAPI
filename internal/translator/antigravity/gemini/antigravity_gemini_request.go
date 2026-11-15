




package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/signature"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/translator/gemini/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	log "github.com/sirupsen/logrus"
)

















func ConvertGeminiRequestToAntigravity(modelName string, inputRawJSON []byte, _ bool) ([]byte, error) {
	if len(inputRawJSON) == 0 {
		return []byte{}, nil
	}
	functionNameMap := util.SanitizedFunctionNameMap(inputRawJSON)
	reqDoc := ojson.NewDoc(inputRawJSON)
	if reqDoc.Get("model").Exists() {
		_ = reqDoc.Delete("model")
	}

	if _, errFixCLIToolResponse := fixCLIToolResponseDocAt(reqDoc, ""); errFixCLIToolResponse != nil {
		return []byte{}, nil
	}

	if systemInstructionResult := reqDoc.Get("system_instruction"); systemInstructionResult.Exists() {
		_ = reqDoc.SetRaw("systemInstruction", systemInstructionResult.Raw)
		_ = reqDoc.Delete("system_instruction")
	}
	signature.SanitizeGeminiSystemInstructionDoc(reqDoc, "contents")

	normalizeGeminiGenerationConfigResponseSchemaDocAt(reqDoc, "")




	contents := reqDoc.Get("contents")
	if contents.IsArray() && geminiContentRolesNeedNormalization(contents) {
		contentItems := translatorcommon.NewRawArrayItems(contents.Get("#").Int())
		previousRole := ""
		contents.ForEach(func(_, value ojson.Result) bool {
			role := value.Get("role").String()
			content := []byte(value.Raw)
			if role != "user" && role != "model" {
				if translatorcommon.ContentHasGeminiFunctionResponse([]byte(value.Raw)) {
					role = "user"
				} else if previousRole == "" || previousRole == "model" {
					role = "user"
				} else {
					role = "model"
				}
				content, _ = ojson.SetBytes(content, "role", role)
			}
			previousRole = role
			contentItems = append(contentItems, content)
			return true
		})
		_ = reqDoc.SetRawBytes("contents", translatorcommon.JoinRawArray(contentItems))
	}

	toolsResult := reqDoc.Get("tools")
	if toolsResult.IsArray() {
		seenFunctionNames := make(map[string]struct{})
		toolsChanged := false
		var toolItems [][]byte
		toolsResult.ForEach(func(toolIndex, tool ojson.Result) bool {
			toolJSON := []byte(tool.Raw)
			toolChanged := false
			for _, key := range []string{"functionDeclarations", "function_declarations"} {
				declarations := tool.Get(key)
				if !declarations.IsArray() {
					continue
				}

				declarationsChanged := false
				var declarationItems [][]byte
				declarations.ForEach(func(_, declaration ojson.Result) bool {
					nameResult := declaration.Get("name")
					originalName := nameResult.String()
					mappedName := util.MapSanitizedFunctionName(functionNameMap, originalName)
					if mappedName != "" {
						if _, exists := seenFunctionNames[mappedName]; exists {
							declarationsChanged = true
							return true
						}
						seenFunctionNames[mappedName] = struct{}{}
					}

					var declarationDoc *ojson.Doc
					ensureDeclDoc := func() *ojson.Doc {
						if declarationDoc == nil {
							declarationDoc = ojson.NewDocString(declaration.Raw)
						}
						return declarationDoc
					}
					if nameResult.Type != ojson.String || mappedName != originalName {
						_ = ensureDeclDoc().Set("name", mappedName)
						declarationsChanged = true
					}
					if parameters := declaration.Get("parameters"); parameters.Exists() {
						normalizedParams := util.NormalizeJSONSchemaNumericConstraints(parameters.Raw)
						_ = ensureDeclDoc().SetRaw("parametersJsonSchema", normalizedParams)
						_ = ensureDeclDoc().Delete("parameters")
						declarationsChanged = true
					} else if parametersJSONSchema := declaration.Get("parametersJsonSchema"); parametersJSONSchema.Exists() {
						if normalizedParams := util.NormalizeJSONSchemaNumericConstraints(parametersJSONSchema.Raw); normalizedParams != parametersJSONSchema.Raw {
							_ = ensureDeclDoc().SetRaw("parametersJsonSchema", normalizedParams)
							declarationsChanged = true
						}
					}
					if declarationDoc != nil {
						declarationItems = append(declarationItems, declarationDoc.Bytes())
					} else {
						declarationItems = append(declarationItems, []byte(declaration.Raw))
					}
					return true
				})
				if declarationsChanged {
					var errSet error
					toolJSON, errSet = ojson.SetRawBytes(toolJSON, key, translatorcommon.JoinRawArray(declarationItems))
					if errSet != nil {
						log.Warnf("failed to normalize function declarations in tool %d: %v", toolIndex.Int(), errSet)
					} else {
						toolChanged = true
					}
				}
			}
			toolsChanged = toolsChanged || toolChanged
			toolItems = append(toolItems, toolJSON)
			return true
		})
		if toolsChanged {
			_ = reqDoc.SetRawBytes("tools", translatorcommon.JoinRawArray(toolItems))
		}
		removeEmptyGeminiFunctionToolsDocAt(reqDoc, "")
	}
	rewriteGeminiFunctionNamesDocAt(reqDoc, "", functionNameMap)

	if strings.Contains(strings.ToLower(modelName), "claude") {
		SanitizeAntigravityClaudeGeminiRequestSignaturesDocAt(modelName, reqDoc, "")
	} else {
		signature.SanitizeGeminiRequestThoughtSignaturesDoc(reqDoc, "contents")
	}

	common.AttachDefaultSafetySettingsDoc(reqDoc, "safetySettings")

	modelJSON, _ := json.Marshal(modelName)
	out := make([]byte, 0, util.ByteCapacity(34, len(modelJSON), reqDoc.Len()))
	out = append(out, `{"project":"","request":`...)
	out = reqDoc.AppendTo(out)
	out = append(out, `,"model":`...)
	out = append(out, modelJSON...)
	out = append(out, '}')
	return out, nil
}



func normalizeGeminiGenerationConfigResponseSchema(rawJSON []byte) []byte {
	doc := ojson.NewDoc(rawJSON)
	if normalizeGeminiGenerationConfigResponseSchemaDoc(doc) {
		return doc.Bytes()
	}
	return rawJSON
}

func normalizeGeminiGenerationConfigResponseSchemaDoc(doc *ojson.Doc) bool {
	return normalizeGeminiGenerationConfigResponseSchemaDocAt(doc, "request.")
}

func normalizeGeminiGenerationConfigResponseSchemaDocAt(doc *ojson.Doc, prefix string) bool {
	if doc == nil {
		return false
	}
	changed := false
	for _, container := range []string{prefix + "generationConfig", prefix + "generation_config"} {
		if !doc.Get(container).Exists() {
			continue
		}
		for _, schemaKey := range []string{"responseJsonSchema", "response_json_schema"} {
			oldPath := container + "." + schemaKey
			if schema := doc.Get(oldPath); schema.Exists() {
				targetPath := container + ".responseSchema"
				if !doc.Get(targetPath).Exists() {
					_ = doc.SetRaw(targetPath, schema.Raw)
				}
				_ = doc.Delete(oldPath)
				changed = true
			}
		}
	}
	return changed
}



func geminiContentRolesNeedNormalization(contents ojson.Result) bool {
	needsNormalization := false
	contents.ForEach(func(_, value ojson.Result) bool {
		role := value.Get("role").String()
		if role != "user" && role != "model" {
			needsNormalization = true
			return false
		}
		return true
	})
	return needsNormalization
}

func removeEmptyGeminiFunctionTools(rawJSON []byte) []byte {
	doc := ojson.NewDoc(rawJSON)
	if removeEmptyGeminiFunctionToolsDoc(doc) {
		return doc.Bytes()
	}
	return rawJSON
}

func removeEmptyGeminiFunctionToolsDoc(doc *ojson.Doc) bool {
	return removeEmptyGeminiFunctionToolsDocAt(doc, "request.")
}

func removeEmptyGeminiFunctionToolsDocAt(doc *ojson.Doc, prefix string) bool {
	if doc == nil {
		return false
	}
	toolsPath := prefix + "tools"
	tools := doc.Get(toolsPath)
	if tools.IsArray() && len(tools.Array()) == 0 {
		_ = doc.Delete(toolsPath)
		return true
	}
	changed := false
	var cleanedTools [][]byte
	for _, tool := range tools.Array() {
		toolJSON := []byte(tool.Raw)
		if tool.IsObject() {
			var toolDoc *ojson.Doc
			for _, key := range []string{"functionDeclarations", "function_declarations"} {
				if declarations := tool.Get(key); declarations.IsArray() && len(declarations.Array()) == 0 {
					if toolDoc == nil {
						toolDoc = ojson.NewDoc(toolJSON)
					}
					_ = toolDoc.Delete(key)
					changed = true
				}
			}
			if toolDoc != nil {
				toolJSON = toolDoc.Bytes()
			}
			if len(util.ParseGJSONBytesNoCopy(toolJSON).Map()) == 0 {
				changed = true
				continue
			}
		}
		cleanedTools = append(cleanedTools, toolJSON)
	}
	if !changed {
		return false
	}
	if len(cleanedTools) == 0 {
		_ = doc.Delete(toolsPath)
		return true
	}
	_ = doc.SetRawBytes(toolsPath, translatorcommon.JoinRawArray(cleanedTools))
	return true
}

func removeEmptyGeminiSystemInstructionParts(rawJSON []byte) []byte {
	return signature.SanitizeGeminiSystemInstruction(rawJSON, "request.contents")
}

func removeEmptyGeminiSystemInstructionPartsDoc(doc *ojson.Doc) {
	signature.SanitizeGeminiSystemInstructionDoc(doc, "request.contents")
}


var geminiFunctionNameFields = []string{"functionCall", "functionResponse", "function_call", "function_response"}



func geminiFunctionNamesNeedRewrite(contents ojson.Result, functionNameMap map[string]string) bool {
	needsRewrite := false
	contents.ForEach(func(_, content ojson.Result) bool {
		content.Get("parts").ForEach(func(_, part ojson.Result) bool {
			for _, field := range geminiFunctionNameFields {
				nameResult := part.Get(field + ".name")
				name := nameResult.String()
				if name == "" {
					continue
				}
				if nameResult.Type == ojson.String && util.MapSanitizedFunctionName(functionNameMap, name) == name {
					continue
				}
				needsRewrite = true
				return false
			}
			return true
		})
		return !needsRewrite
	})
	return needsRewrite
}

func rewriteGeminiFunctionNames(rawJSON []byte, functionNameMap map[string]string) []byte {
	doc := ojson.NewDoc(rawJSON)
	if rewriteGeminiFunctionNamesDoc(doc, functionNameMap) {
		return doc.Bytes()
	}
	return rawJSON
}

func rewriteGeminiFunctionNamesDoc(doc *ojson.Doc, functionNameMap map[string]string) bool {
	return rewriteGeminiFunctionNamesDocAt(doc, "request.", functionNameMap)
}

func rewriteGeminiFunctionNamesDocAt(doc *ojson.Doc, prefix string, functionNameMap map[string]string) bool {
	if doc == nil {
		return false
	}
	changed := false
	contentsPath := prefix + "contents"
	contents := doc.Get(contentsPath)
	canBatchContents := contents.IsArray()
	if canBatchContents {
		contents.ForEach(func(_, content ojson.Result) bool {
			parts := content.Get("parts")
			if parts.Exists() && !parts.IsArray() {
				canBatchContents = false
				return false
			}
			return true
		})
	}


	if canBatchContents && geminiFunctionNamesNeedRewrite(contents, functionNameMap) {
		contentItems := translatorcommon.NewRawArrayItems(contents.Get("#").Int())
		contents.ForEach(func(_, content ojson.Result) bool {
			contentJSON := []byte(content.Raw)
			partsChanged := false
			partItems := make([][]byte, 0, 4)
			content.Get("parts").ForEach(func(_, part ojson.Result) bool {
				var partDoc *ojson.Doc
				for _, field := range geminiFunctionNameFields {
					nameResult := part.Get(field + ".name")
					name := nameResult.String()
					if name == "" {
						continue
					}
					mappedName := util.MapSanitizedFunctionName(functionNameMap, name)
					if nameResult.Type == ojson.String && mappedName == name {
						continue
					}
					if partDoc == nil {
						partDoc = ojson.NewDocString(part.Raw)
					}
					_ = partDoc.Set(field+".name", mappedName)
					partsChanged = true
				}
				if partDoc != nil {
					partItems = append(partItems, partDoc.Bytes())
				} else {
					partItems = append(partItems, []byte(part.Raw))
				}
				return true
			})
			if partsChanged {
				contentJSON, _ = ojson.SetRawBytes(contentJSON, "parts", translatorcommon.JoinRawArray(partItems))
			}
			contentItems = append(contentItems, contentJSON)
			return true
		})
		_ = doc.SetRawBytes(contentsPath, translatorcommon.JoinRawArray(contentItems))
		changed = true
	} else if !canBatchContents {
		for contentIndex, content := range contents.Array() {
			for partIndex, part := range content.Get("parts").Array() {
				for _, field := range geminiFunctionNameFields {
					nameResult := part.Get(field + ".name")
					name := nameResult.String()
					if name == "" {
						continue
					}
					mappedName := util.MapSanitizedFunctionName(functionNameMap, name)
					if nameResult.Type == ojson.String && mappedName == name {
						continue
					}
					path := fmt.Sprintf("%s.%d.parts.%d.%s.name", contentsPath, contentIndex, partIndex, field)
					_ = doc.Set(path, mappedName)
					changed = true
				}
			}
		}
	}

	for _, allowedPath := range []string{
		prefix + "toolConfig.functionCallingConfig.allowedFunctionNames",
		prefix + "tool_config.function_calling_config.allowed_function_names",
	} {
		allowedNames := doc.Get(allowedPath)
		if allowedNames.IsArray() {
			namesChanged := false
			nameItems := make([][]byte, 0, 4)
			allowedNames.ForEach(func(_, name ojson.Result) bool {
				mappedName := util.MapSanitizedFunctionName(functionNameMap, name.String())
				namesChanged = namesChanged || name.Type != ojson.String || mappedName != name.String()
				mappedNameJSON, _ := json.Marshal(mappedName)
				nameItems = append(nameItems, mappedNameJSON)
				return true
			})
			if namesChanged {
				_ = doc.SetRawBytes(allowedPath, translatorcommon.JoinRawArray(nameItems))
				changed = true
			}
		} else {
			for index, name := range allowedNames.Array() {
				mappedName := util.MapSanitizedFunctionName(functionNameMap, name.String())
				if name.Type == ojson.String && mappedName == name.String() {
					continue
				}
				path := fmt.Sprintf("%s.%d", allowedPath, index)
				_ = doc.Set(path, mappedName)
				changed = true
			}
		}
	}
	return changed
}

func SanitizeAntigravityClaudeGeminiRequestSignatures(modelName string, rawJSON []byte) []byte {
	rawJSON = signature.SanitizeGeminiSystemInstruction(rawJSON, "request.contents")
	doc := ojson.NewDoc(rawJSON)
	if SanitizeAntigravityClaudeGeminiRequestSignaturesDoc(modelName, doc) {
		return doc.Bytes()
	}
	return rawJSON
}

func SanitizeAntigravityClaudeGeminiRequestSignaturesDoc(modelName string, doc *ojson.Doc) bool {
	return SanitizeAntigravityClaudeGeminiRequestSignaturesDocAt(modelName, doc, "request.")
}

func SanitizeAntigravityClaudeGeminiRequestSignaturesDocAt(modelName string, doc *ojson.Doc, prefix string) bool {
	if doc == nil {
		return false
	}
	contentsPath := prefix + "contents"
	signature.SanitizeGeminiSystemInstructionDoc(doc, contentsPath)
	contents := doc.Get(contentsPath)
	if !contents.IsArray() {
		return false
	}

	contentsArray := contents.Array()
	changed := false
	rewrittenContents := make([][]byte, 0, len(contentsArray))

	for contentIndex, content := range contentsArray {
		parts := content.Get("parts")
		if !parts.IsArray() {
			rewrittenContents = append(rewrittenContents, []byte(content.Raw))
			continue
		}

		isModelTurn := content.Get("role").String() == "model"
		partsArray := parts.Array()
		contentChanged := false
		rewrittenParts := make([][]byte, 0, len(partsArray))

		for partIndex, partResult := range partsArray {
			var part map[string]any
			decoder := json.NewDecoder(strings.NewReader(partResult.Raw))
			decoder.UseNumber()
			if err := decoder.Decode(&part); err != nil {
				rewrittenParts = append(rewrittenParts, []byte(partResult.Raw))
				continue
			}

			rawSignature, hasStringSignature := antigravityClaudeGeminiPartThoughtSignature(part)
			hasSignatureKey := hasStringSignature || antigravityClaudeGeminiPartHasThoughtSignatureKey(part) || antigravityClaudeGeminiPartHasThoughtSignatureKeyInRaw(partResult.Raw)

			if hasFunctionResponsePart(part) {
				if hasSignatureKey {
					changed = true
					contentChanged = true
					deleteAntigravityClaudeGeminiPartThoughtSignatureFields(part)
					logAntigravityClaudeGeminiSignatureSanitize(modelName, "drop_signature", "functionResponse parts cannot replay Claude thinking signatures", contentIndex, partIndex, rawSignature)
					partBytes, _ := json.Marshal(part)
					rewrittenParts = append(rewrittenParts, partBytes)
				} else {
					rewrittenParts = append(rewrittenParts, []byte(partResult.Raw))
				}
				continue
			}

			if !isModelTurn {
				if hasSignatureKey {
					changed = true
					contentChanged = true
					deleteAntigravityClaudeGeminiPartThoughtSignatureFields(part)
					logAntigravityClaudeGeminiSignatureSanitize(modelName, "drop_signature", "non-model parts cannot replay Claude thinking signatures", contentIndex, partIndex, rawSignature)
					partBytes, _ := json.Marshal(part)
					rewrittenParts = append(rewrittenParts, partBytes)
				} else {
					rewrittenParts = append(rewrittenParts, []byte(partResult.Raw))
				}
				continue
			}

			if part["thought"] == true {
				normalized, compatible := signature.CompatibleAntigravityClaudeThinkingSignature(rawSignature)
				if !compatible {
					changed = true
					contentChanged = true
					logAntigravityClaudeGeminiSignatureSanitize(modelName, "drop_thinking_block", "missing_or_incompatible_signature", contentIndex, partIndex, rawSignature)
					continue
				}
				text, _ := part["text"].(string)
				if strings.TrimSpace(text) == "" {
					changed = true
					contentChanged = true
					logAntigravityClaudeGeminiSignatureSanitize(modelName, "drop_thinking_block", "empty_thinking_text", contentIndex, partIndex, rawSignature)
					continue
				}
				if normalized != rawSignature {
					changed = true
					contentChanged = true
					logAntigravityClaudeGeminiSignatureSanitize(modelName, "normalize_signature", "compatible_claude_signature", contentIndex, partIndex, rawSignature)
				}
				deleteAntigravityClaudeGeminiPartThoughtSignatureFields(part)
				part["thoughtSignature"] = normalized
				partBytes, _ := json.Marshal(part)
				rewrittenParts = append(rewrittenParts, partBytes)
				continue
			}

			if hasSignatureKey {
				changed = true
				contentChanged = true
				deleteAntigravityClaudeGeminiPartThoughtSignatureFields(part)
				logAntigravityClaudeGeminiSignatureSanitize(modelName, "drop_signature", "non-thinking parts should not carry Claude thinking signatures", contentIndex, partIndex, rawSignature)
				partBytes, _ := json.Marshal(part)
				rewrittenParts = append(rewrittenParts, partBytes)
			} else {
				rewrittenParts = append(rewrittenParts, []byte(partResult.Raw))
			}
		}

		if len(rewrittenParts) == 0 {
			changed = true
			continue
		}
		if contentChanged || len(rewrittenParts) != len(partsArray) {
			contentBytes := []byte(content.Raw)
			contentBytes, _ = ojson.SetRawBytes(contentBytes, "parts", translatorcommon.JoinRawArray(rewrittenParts))
			rewrittenContents = append(rewrittenContents, contentBytes)
		} else {
			rewrittenContents = append(rewrittenContents, []byte(content.Raw))
		}
	}

	if !changed {
		return false
	}
	return doc.SetRawBytes(contentsPath, translatorcommon.JoinRawArray(rewrittenContents)) == nil
}

func antigravityClaudeGeminiPartHasThoughtSignatureKeyInRaw(raw string) bool {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var stack []bool
	expectKey := false

	for {
		t, err := dec.Token()
		if err != nil {
			break
		}

		switch v := t.(type) {
		case json.Delim:
			switch v {
			case '{':
				stack = append(stack, true)
				expectKey = true
			case '}':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				if len(stack) > 0 && stack[len(stack)-1] {
					expectKey = true
				} else {
					expectKey = false
				}
			case '[':
				stack = append(stack, false)
				expectKey = false
			case ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				if len(stack) > 0 && stack[len(stack)-1] {
					expectKey = true
				} else {
					expectKey = false
				}
			}
		case string:
			if expectKey && len(stack) > 0 && stack[len(stack)-1] {
				if v == "thoughtSignature" || v == "thought_signature" {
					return true
				}
				expectKey = false
			} else {
				if len(stack) > 0 && stack[len(stack)-1] {
					expectKey = true
				}
			}
		default:
			if len(stack) > 0 && stack[len(stack)-1] {
				expectKey = true
			}
		}
	}
	return false
}

func antigravityClaudeGeminiPartHasThoughtSignatureKey(part map[string]any) bool {
	for _, path := range [][]string{
		{"thoughtSignature"},
		{"thought_signature"},
		{"functionCall", "thoughtSignature"},
		{"functionCall", "thought_signature"},
		{"functionResponse", "thoughtSignature"},
		{"functionResponse", "thought_signature"},
		{"extra_content", "google", "thought_signature"},
	} {
		if hasKeyAtPath(part, path...) {
			return true
		}
	}
	return false
}

func hasKeyAtPath(value map[string]any, path ...string) bool {
	var current any = value
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return false
		}
		if _, exists := m[key]; !exists {
			return false
		}
		current = m[key]
	}
	return true
}

func antigravityClaudeGeminiPartThoughtSignature(part map[string]any) (string, bool) {
	for _, path := range [][]string{
		{"thoughtSignature"},
		{"thought_signature"},
		{"functionCall", "thoughtSignature"},
		{"functionCall", "thought_signature"},
		{"functionResponse", "thoughtSignature"},
		{"functionResponse", "thought_signature"},
		{"extra_content", "google", "thought_signature"},
	} {
		if value, ok := stringAtPath(part, path...); ok {
			return value, true
		}
	}
	return "", false
}

func deleteAntigravityClaudeGeminiPartThoughtSignatureFields(part map[string]any) {
	for _, path := range [][]string{
		{"thoughtSignature"},
		{"thought_signature"},
		{"functionCall", "thoughtSignature"},
		{"functionCall", "thought_signature"},
		{"functionResponse", "thoughtSignature"},
		{"functionResponse", "thought_signature"},
		{"extra_content", "google", "thought_signature"},
	} {
		deleteAtPath(part, path...)
	}
}

func hasFunctionResponsePart(part map[string]any) bool {
	if _, ok := part["functionResponse"]; ok {
		return true
	}
	_, ok := part["function_response"]
	return ok
}

func stringAtPath(value map[string]any, path ...string) (string, bool) {
	var current any = value
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = m[key]
		if !ok {
			return "", false
		}
	}
	s, ok := current.(string)
	return s, ok
}

func deleteAtPath(value map[string]any, path ...string) {
	if len(path) == 0 {
		return
	}
	current := value
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]any)
		if !ok {
			return
		}
		current = next
	}
	delete(current, path[len(path)-1])
}

func logAntigravityClaudeGeminiSignatureSanitize(modelName, action, reason string, contentIndex, partIndex int, rawSignature string) {
	fields := log.Fields{
		"component":         "signature_sanitizer",
		"translator":        "antigravity_gemini",
		"target_provider":   string(signature.SignatureProviderClaude),
		"action":            action,
		"reason":            reason,
		"model":             modelName,
		"content_index":     contentIndex,
		"part_index":        partIndex,
		"has_signature":     strings.TrimSpace(rawSignature) != "",
		"signature_length":  len(strings.TrimSpace(rawSignature)),
		"detected_provider": string(signature.DetectSignatureProviderForBlock(rawSignature, signature.SignatureBlockKindClaudeThinking)),
	}
	log.WithFields(fields).Debug("antigravity gemini translator: sanitized Claude target thoughtSignature before upstream")
}


type FunctionCallGroup struct {
	ResponsesNeeded int
	CallIDs         []string
	CallNames       []string
}

func selectGroupResponses(collected []ojson.Result, group *FunctionCallGroup) ([]ojson.Result, []ojson.Result) {
	needed := group.ResponsesNeeded
	if len(collected) <= needed {
		return collected, nil
	}

	hasCallID := false
	for _, id := range group.CallIDs {
		if id != "" {
			hasCallID = true
			break
		}
	}
	if !hasCallID {
		return collected[:needed], collected[needed:]
	}


	byID := make(map[string][]int, len(collected))
	byName := make(map[string][]int, len(collected))
	var emptyNameIdxs []int
	for idx, resp := range collected {
		funcResp := resp.Get("functionResponse")
		if id := funcResp.Get("id").String(); id != "" {
			byID[id] = append(byID[id], idx)
		}
		if name := funcResp.Get("name").String(); name != "" {
			byName[name] = append(byName[name], idx)
		} else {
			emptyNameIdxs = append(emptyNameIdxs, idx)
		}
	}

	used := make([]bool, len(collected))
	selected := make([]ojson.Result, needed)
	matchedSlot := make([]bool, needed)
	popUnused := func(queue []int) (int, []int, bool) {
		for len(queue) > 0 {
			idx := queue[0]
			queue = queue[1:]
			if !used[idx] {
				return idx, queue, true
			}
		}
		return 0, queue, false
	}

	for slot := 0; slot < needed; slot++ {
		if slot >= len(group.CallIDs) || group.CallIDs[slot] == "" {
			continue
		}
		callID := group.CallIDs[slot]
		if idx, rest, ok := popUnused(byID[callID]); ok {
			byID[callID] = rest
			used[idx] = true
			selected[slot] = collected[idx]
			matchedSlot[slot] = true
		} else {
			delete(byID, callID)
		}
	}

	for slot := 0; slot < needed; slot++ {
		if matchedSlot[slot] || slot >= len(group.CallNames) || group.CallNames[slot] == "" {
			continue
		}
		callName := group.CallNames[slot]
		if idx, rest, ok := popUnused(byName[callName]); ok {
			byName[callName] = rest
			used[idx] = true
			selected[slot] = collected[idx]
			matchedSlot[slot] = true
			continue
		}
		delete(byName, callName)
		if idx, rest, ok := popUnused(emptyNameIdxs); ok {
			emptyNameIdxs = rest
			used[idx] = true
			selected[slot] = collected[idx]
			matchedSlot[slot] = true
		} else {
			emptyNameIdxs = nil
		}
	}

	freeCursor := 0
	for slot := 0; slot < needed; slot++ {
		if matchedSlot[slot] {
			continue
		}
		for freeCursor < len(collected) && used[freeCursor] {
			freeCursor++
		}
		if freeCursor < len(collected) {
			used[freeCursor] = true
			selected[slot] = collected[freeCursor]
			matchedSlot[slot] = true
			freeCursor++
		}
	}

	remaining := make([]ojson.Result, 0, len(collected)-needed)
	for idx, resp := range collected {
		if !used[idx] {
			remaining = append(remaining, resp)
		}
	}
	return selected, remaining
}

func normalizeAntigravityInlineDataPart(part ojson.Result) ([]byte, bool) {
	inline := part.Get("inlineData")
	if !inline.Exists() {
		inline = part.Get("inline_data")
	}
	if !inline.Exists() {
		return nil, false
	}
	data := inline.Get("data").String()
	if data == "" {
		return nil, false
	}
	mimeType := inline.Get("mimeType").String()
	if mimeType == "" {
		mimeType = inline.Get("mime_type").String()
	}
	if mimeType == "" {

		mimeType = "image/png"
	}
	outDoc := ojson.NewDoc([]byte(`{"inlineData":{"mimeType":"","data":""}}`))
	_ = outDoc.Set("inlineData.mimeType", mimeType)
	_ = outDoc.Set("inlineData.data", data)
	return outDoc.Bytes(), true
}

func attachInlineDataToFunctionResponse(response ojson.Result, images [][]byte) ojson.Result {
	if len(images) == 0 {
		return response
	}
	targetDoc := ojson.NewDocString(response.Raw)
	for _, img := range images {
		_ = targetDoc.SetRawBytes("functionResponse.parts.-1", img)
	}
	return util.ParseGJSONBytesNoCopy(targetDoc.Bytes())
}




func collectFunctionResponsesWithSiblingInlineData(parts ojson.Result) []ojson.Result {
	responses := make([]ojson.Result, 0)
	leadingImages := make([][]byte, 0)
	current := -1
	parts.ForEach(func(_, part ojson.Result) bool {
		if part.Get("functionResponse").Exists() {
			responses = append(responses, part)
			current = len(responses) - 1
			if len(leadingImages) > 0 {
				responses[current] = attachInlineDataToFunctionResponse(responses[current], leadingImages)
				leadingImages = nil
			}
			return true
		}
		imagePart, ok := normalizeAntigravityInlineDataPart(part)
		if !ok {
			return true
		}
		if current >= 0 {
			responses[current] = attachInlineDataToFunctionResponse(responses[current], [][]byte{imagePart})
			return true
		}
		leadingImages = append(leadingImages, imagePart)
		return true
	})
	return responses
}




func parseFunctionResponseRaw(response ojson.Result, fallbackName string) string {
	if response.IsObject() && ojson.Valid(response.Raw) {
		raw := response.Raw
		name := response.Get("functionResponse.name").String()
		if strings.TrimSpace(name) == "" && fallbackName != "" {
			updated, _ := ojson.SetBytes([]byte(raw), "functionResponse.name", fallbackName)
			raw = string(updated)
		}
		return raw
	}

	log.Debugf("parse function response failed, using fallback")
	funcResp := response.Get("functionResponse")
	if funcResp.Exists() {
		frDoc := ojson.NewDoc([]byte(`{"functionResponse":{"name":"","response":{"result":""}}}`))
		name := funcResp.Get("name").String()
		if strings.TrimSpace(name) == "" {
			name = fallbackName
		}
		_ = frDoc.Set("functionResponse.name", name)
		_ = frDoc.Set("functionResponse.response.result", funcResp.Get("response").String())
		if id := funcResp.Get("id").String(); id != "" {
			_ = frDoc.Set("functionResponse.id", id)
		}
		return frDoc.String()
	}

	useName := fallbackName
	if useName == "" {
		useName = "unknown"
	}
	frDoc := ojson.NewDoc([]byte(`{"functionResponse":{"name":"","response":{"result":""}}}`))
	_ = frDoc.Set("functionResponse.name", useName)
	_ = frDoc.Set("functionResponse.response.result", response.String())
	return frDoc.String()
}













func fixCLIToolResponse(input []byte) ([]byte, error) {
	doc := ojson.NewDoc(input)
	changed, err := fixCLIToolResponseDoc(doc)
	if err != nil || !changed {
		return input, err
	}
	return doc.Bytes(), nil
}

func fixCLIToolResponseDoc(doc *ojson.Doc) (bool, error) {
	return fixCLIToolResponseDocAt(doc, "request.")
}

func fixCLIToolResponseDocAt(doc *ojson.Doc, prefix string) (bool, error) {
	if doc == nil {
		return false, fmt.Errorf("contents not found in input")
	}


	contentsPath := prefix + "contents"
	contents := doc.Get(contentsPath)
	if !contents.Exists() {
		return false, fmt.Errorf("contents not found in input")
	}

	needsGrouping := false
	allContentsAreObjects := true
	contents.ForEach(func(_, content ojson.Result) bool {
		if !content.IsObject() {
			allContentsAreObjects = false
			return true
		}
		content.Get("parts").ForEach(func(_, part ojson.Result) bool {
			if part.Get("functionResponse").Exists() {
				needsGrouping = true
				return false
			}
			return true
		})
		return !needsGrouping
	})
	if contents.IsArray() && allContentsAreObjects && !needsGrouping {
		return false, nil
	}


	contentItems := translatorcommon.NewRawArrayItems(contents.Get("#").Int())
	var pendingGroups []*FunctionCallGroup
	var collectedResponses []ojson.Result
	appendFunctionResponses := func(responses []ojson.Result, callNames []string) {
		partItems := make([][]byte, 0, len(responses))
		for responseIndex, response := range responses {
			partRaw := parseFunctionResponseRaw(response, callNames[responseIndex])
			if partRaw != "" {
				partItems = append(partItems, []byte(partRaw))
			}
		}
		if len(partItems) > 0 {
			functionResponseContent := []byte(`{"parts":[],"role":"function"}`)
			functionResponseContent, _ = ojson.SetRawBytes(functionResponseContent, "parts", translatorcommon.JoinRawArray(partItems))
			contentItems = append(contentItems, functionResponseContent)
		}
	}



	contents.ForEach(func(key, value ojson.Result) bool {
		role := value.Get("role").String()
		parts := value.Get("parts")


		responsePartsInThisContent := collectFunctionResponsesWithSiblingInlineData(parts)


		if len(responsePartsInThisContent) > 0 {
			collectedResponses = append(collectedResponses, responsePartsInThisContent...)


			for len(pendingGroups) > 0 && len(collectedResponses) >= pendingGroups[0].ResponsesNeeded {
				group := pendingGroups[0]
				pendingGroups = pendingGroups[1:]


				var groupResponses []ojson.Result
				groupResponses, collectedResponses = selectGroupResponses(collectedResponses, group)

				appendFunctionResponses(groupResponses, group.CallNames)
			}
			if len(pendingGroups) == 0 {


				collectedResponses = nil
			}

			return true
		}


		if role == "model" {
			var callIDs []string
			var callNames []string
			parts.ForEach(func(_, part ojson.Result) bool {
				if part.Get("functionCall").Exists() {
					callIDs = append(callIDs, part.Get("functionCall.id").String())
					callNames = append(callNames, part.Get("functionCall.name").String())
				}
				return true
			})

			if len(callNames) > 0 {

				if !value.IsObject() {
					log.Warnf("failed to parse model content")
					return true
				}
				contentItems = append(contentItems, []byte(value.Raw))


				group := &FunctionCallGroup{
					ResponsesNeeded: len(callNames),
					CallIDs:         callIDs,
					CallNames:       callNames,
				}
				pendingGroups = append(pendingGroups, group)
			} else {

				if !value.IsObject() {
					log.Warnf("failed to parse content")
					return true
				}
				contentItems = append(contentItems, []byte(value.Raw))
			}
		} else {

			if !value.IsObject() {
				log.Warnf("failed to parse content")
				return true
			}
			contentItems = append(contentItems, []byte(value.Raw))
		}

		return true
	})


	for _, group := range pendingGroups {
		if len(collectedResponses) >= group.ResponsesNeeded {
			groupResponses := collectedResponses[:group.ResponsesNeeded]
			collectedResponses = collectedResponses[group.ResponsesNeeded:]

			appendFunctionResponses(groupResponses, group.CallNames)
		}
	}


	_ = doc.SetRawBytes(contentsPath, translatorcommon.JoinRawArray(contentItems))
	return true, nil
}
