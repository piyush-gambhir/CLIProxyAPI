package gemini

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

func ConvertGeminiRequestToClaude(modelName string, inputRawJSON []byte, stream bool) ([]byte, error) {
	return convertGeminiRequestToClaude(modelName, inputRawJSON, stream)

}

func convertGeminiRequestToClaude(modelName string, inputRawJSON []byte, stream bool) ([]byte, error) {
	rawJSON := inputRawJSON
	var drops translatorcommon.UserTurnDrops

	userID := translatorcommon.DeriveClaudeUserID(rawJSON)

	outDoc := ojson.NewDoc([]byte(`{"model":"","max_tokens":32000,"messages":[],"metadata":{}}`))
	_ = outDoc.Set("metadata.user_id", userID)

	root := util.ParseGJSONBytesNoCopy(rawJSON)
	messageAccumulator := translatorcommon.NewClaudeMessageAccumulator(int(root.Get("contents.#").Int()) + 1)

	getGeminiToolID := func(value ojson.Result) string {
		if toolID := strings.TrimSpace(value.Get("id").String()); toolID != "" {
			return toolID
		}
		return strings.TrimSpace(value.Get("call_id").String())
	}

	removePendingToolID := func(ids []string, toolID string) []string {
		if toolID == "" {
			return ids
		}
		for idx, pendingID := range ids {
			if pendingID == toolID {
				return append(ids[:idx], ids[idx+1:]...)
			}
		}
		return ids
	}

	var pendingToolIDs []string
	toolCallCounter := 0

	_ = outDoc.Set("model", modelName)
	if serviceTier := root.Get("service_tier"); serviceTier.Exists() && serviceTier.Type == ojson.String {
		_ = outDoc.Set("service_tier", serviceTier.String())
	}

	if genConfig := root.Get("generationConfig"); genConfig.Exists() {

		if maxTokens := genConfig.Get("maxOutputTokens"); maxTokens.Exists() {
			_ = outDoc.Set("max_tokens", maxTokens.Int())
		}

		if topP := genConfig.Get("topP"); topP.Exists() {
			_ = outDoc.Set("top_p", topP.Float())
		}

		if stopSeqs := genConfig.Get("stopSequences"); stopSeqs.Exists() && stopSeqs.IsArray() {
			var stopSequences []string
			stopSeqs.ForEach(func(_, value ojson.Result) bool {
				stopSequences = append(stopSequences, value.String())
				return true
			})
			if len(stopSequences) > 0 {
				_ = outDoc.Set("stop_sequences", stopSequences)
			}
		}

		if thinkingConfig := genConfig.Get("thinkingConfig"); thinkingConfig.Exists() && thinkingConfig.IsObject() {
			mi := registry.LookupModelInfo(modelName, "claude")
			supportsAdaptive := mi != nil && mi.Thinking != nil && len(mi.Thinking.Levels) > 0
			supportsMax := supportsAdaptive && thinking.HasLevel(mi.Thinking.Levels, string(thinking.LevelMax))
			supportsDynamic := supportsAdaptive && mi.Thinking.DynamicAllowed

			thinkingLevel := thinkingConfig.Get("thinkingLevel")
			if !thinkingLevel.Exists() {
				thinkingLevel = thinkingConfig.Get("thinking_level")
			}
			if thinkingLevel.Exists() {
				level := strings.ToLower(strings.TrimSpace(thinkingLevel.String()))
				if supportsAdaptive {
					switch {
					case level == "":
					case level == "none":
						_ = outDoc.Set("thinking.type", "disabled")
						_ = outDoc.Delete("thinking.budget_tokens")
						_ = outDoc.Delete("output_config.effort")
					case level == "auto" && supportsDynamic:
						_ = outDoc.Set("thinking.type", "adaptive")
						_ = outDoc.Delete("thinking.budget_tokens")
						_ = outDoc.Delete("output_config.effort")
					default:
						if !thinking.HasLevel(mi.Thinking.Levels, level) {
							if mapped, ok := thinking.MapToClaudeEffort(level, supportsMax); ok {
								level = mapped
							}
						}
						_ = outDoc.Set("thinking.type", "adaptive")
						_ = outDoc.Delete("thinking.budget_tokens")
						_ = outDoc.Set("output_config.effort", level)
					}
				} else {
					switch level {
					case "":
					case "none":
						_ = outDoc.Set("thinking.type", "disabled")
						_ = outDoc.Delete("thinking.budget_tokens")
					case "auto":
						_ = outDoc.Set("thinking.type", "enabled")
						_ = outDoc.Delete("thinking.budget_tokens")
					default:
						if budget, ok := thinking.ConvertLevelToBudget(level); ok {
							_ = outDoc.Set("thinking.type", "enabled")
							_ = outDoc.Set("thinking.budget_tokens", budget)
						}
					}
				}
			} else {
				thinkingBudget := thinkingConfig.Get("thinkingBudget")
				if !thinkingBudget.Exists() {
					thinkingBudget = thinkingConfig.Get("thinking_budget")
				}
				if thinkingBudget.Exists() {
					budget := int(thinkingBudget.Int())
					if supportsAdaptive {
						switch {
						case budget == 0:
							_ = outDoc.Set("thinking.type", "disabled")
							_ = outDoc.Delete("thinking.budget_tokens")
							_ = outDoc.Delete("output_config.effort")
						case budget == -1 && supportsDynamic:
							_ = outDoc.Set("thinking.type", "adaptive")
							_ = outDoc.Delete("thinking.budget_tokens")
							_ = outDoc.Delete("output_config.effort")
						default:
							level, ok := thinking.ConvertBudgetToLevel(budget)
							if ok {
								if !thinking.HasLevel(mi.Thinking.Levels, level) {
									if mapped, okM := thinking.MapToClaudeEffort(level, supportsMax); okM {
										level = mapped
									}
								}
								_ = outDoc.Set("thinking.type", "adaptive")
								_ = outDoc.Delete("thinking.budget_tokens")
								_ = outDoc.Set("output_config.effort", level)
							}
						}
					} else {
						switch budget {
						case 0:
							_ = outDoc.Set("thinking.type", "disabled")
							_ = outDoc.Delete("thinking.budget_tokens")
						case -1:
							_ = outDoc.Set("thinking.type", "enabled")
							_ = outDoc.Delete("thinking.budget_tokens")
						default:
							_ = outDoc.Set("thinking.type", "enabled")
							_ = outDoc.Set("thinking.budget_tokens", budget)
						}
					}
				}
			}
		}
	}

	sysInstr := root.Get("systemInstruction")
	if !sysInstr.Exists() {
		sysInstr = root.Get("system_instruction")
	}
	if sysInstr.Exists() {
		if parts := sysInstr.Get("parts"); parts.Exists() && parts.IsArray() {
			var systemText strings.Builder
			parts.ForEach(func(_, part ojson.Result) bool {
				if translatorcommon.IsGeminiThoughtPart(part) {
					return true
				}
				if text := part.Get("text"); text.Exists() {
					if systemText.Len() > 0 {
						systemText.WriteString("\n")
					}
					systemText.WriteString(text.String())
				}
				return true
			})
			if systemText.Len() > 0 {

				systemMessage := []byte(`{"role":"user","content":[{"type":"text","text":""}]}`)
				systemMessage, _ = ojson.SetBytes(systemMessage, "content.0.text", systemText.String())
				messageAccumulator.Append(systemMessage)
				messageAccumulator.Flush()
			}
		}
	}

	if contents := root.Get("contents"); contents.Exists() && contents.IsArray() {
		previousRole := ""
		contents.ForEach(func(_, content ojson.Result) bool {
			parts := content.Get("parts")
			role := translatorcommon.NormalizeGeminiTurnRole(content.Get("role").String(), parts, previousRole)

			hasFunctionResponse := translatorcommon.GeminiPartsHaveFunctionResponse(parts)
			if hasFunctionResponse && !translatorcommon.GeminiPartsHaveFunctionCall(parts) {
				role = "user"
			}

			contentItems := make([][]byte, 0, 4)

			sendable := 0
			flushContent := func() {
				if role != "assistant" {
					drops.EndTurn(sendable)
				}
				if len(contentItems) > 0 {
					msgDoc := ojson.NewDoc([]byte(`{"role":"","content":[]}`))
					_ = msgDoc.Set("role", role)
					_ = msgDoc.SetRawBytes("content", translatorcommon.JoinRawArray(contentItems))
					messageAccumulator.Append(msgDoc.Bytes())
				}
				contentItems = contentItems[:0]
				sendable = 0
			}

			switchRole := func(nextRole string) {
				if role != nextRole {
					flushContent()
					role = nextRole
				}
			}
			if parts.Exists() && parts.IsArray() {
				parts.ForEach(func(_, part ojson.Result) bool {
					if translatorcommon.IsGeminiThoughtPart(part) {
						return true
					}

					if text := part.Get("text"); text.Exists() {
						textContent := []byte(`{"type":"text","text":""}`)
						textContent, _ = ojson.SetBytes(textContent, "text", text.String())
						contentItems = append(contentItems, textContent)
						if strings.TrimSpace(text.String()) != "" {
							sendable++
						}
						return true
					}

					if fc := translatorcommon.GeminiPartFunctionCall(part); fc.Exists() && (role == "assistant" || hasFunctionResponse) {
						switchRole("assistant")
						toolUseDoc := ojson.NewDoc([]byte(`{"type":"tool_use","id":"","name":"","input":{}}`))

						toolID := getGeminiToolID(fc)
						if toolID == "" {
							toolCallCounter++
							toolID = fmt.Sprintf("toolu_gemini_%016d", toolCallCounter)
						}
						pendingToolIDs = append(pendingToolIDs, toolID)
						_ = toolUseDoc.Set("id", toolID)

						if name := fc.Get("name"); name.Exists() {
							_ = toolUseDoc.Set("name", util.SanitizeClaudeFunctionName(name.String()))
						}
						if args := fc.Get("args"); args.Exists() && args.IsObject() {
							_ = toolUseDoc.SetRaw("input", args.Raw)
						}
						contentItems = append(contentItems, toolUseDoc.Bytes())
						sendable++
						return true
					}

					if fr := translatorcommon.GeminiPartFunctionResponse(part); fr.Exists() {
						switchRole("user")
						toolResultDoc := ojson.NewDoc([]byte(`{"type":"tool_result","tool_use_id":"","content":""}`))

						var toolID string
						if customID := getGeminiToolID(fr); customID != "" {
							toolID = customID
							pendingToolIDs = removePendingToolID(pendingToolIDs, toolID)
						} else if len(pendingToolIDs) > 0 {
							toolID = pendingToolIDs[0]

							pendingToolIDs = pendingToolIDs[1:]
						} else {

							toolCallCounter++
							toolID = fmt.Sprintf("toolu_gemini_%016d", toolCallCounter)
						}
						_ = toolResultDoc.Set("tool_use_id", toolID)

						if result := fr.Get("response.result"); result.Exists() {
							_ = toolResultDoc.Set("content", result.String())
						} else if response := fr.Get("response"); response.Exists() {
							_ = toolResultDoc.Set("content", response.Raw)
						}
						contentItems = append(contentItems, toolResultDoc.Bytes())
						sendable++
						return true
					}

					if inlineData := geminiClaudeInlineData(part); inlineData.Exists() {
						if contentPart, ok := claudeContentPartFromGeminiInlineData(inlineData, role == "assistant"); ok {
							contentItems = append(contentItems, contentPart)
							sendable++
						} else if role != "assistant" {
							drops.Drop("inlineData")
						}
						return true
					}

					if fileData := geminiClaudeFileData(part); fileData.Exists() {
						if contentPart, ok := claudeContentPartFromGeminiFileData(fileData); ok {
							contentItems = append(contentItems, contentPart)
							sendable++
						} else if role != "assistant" {
							drops.Drop("fileData")
						}
						return true
					}

					return true
				})
			}
			flushContent()
			previousRole = role

			return true
		})
	}
	messages := messageAccumulator.Messages()
	orderGeminiClaudeToolResults(messages)
	translatorcommon.SetRawArrayItemsDoc(outDoc, "messages", messages)

	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var anthropicTools []interface{}

		tools.ForEach(func(_, tool ojson.Result) bool {
			if funcDecls := tool.Get("functionDeclarations"); funcDecls.Exists() && funcDecls.IsArray() {
				funcDecls.ForEach(func(_, funcDecl ojson.Result) bool {
					toolDoc := ojson.NewDoc([]byte(`{"name":"","description":"","input_schema":{"type":"object","properties":{}}}`))

					if name := funcDecl.Get("name"); name.Exists() {
						_ = toolDoc.Set("name", util.SanitizeClaudeFunctionName(name.String()))
					}
					if desc := funcDecl.Get("description"); desc.Exists() {
						_ = toolDoc.Set("description", desc.String())
					}
					if params := funcDecl.Get("parameters"); params.Exists() {
						cleaned := normalizeClaudeToolSchema(params)
						_ = toolDoc.SetRawBytes("input_schema", cleaned)
					} else if params = funcDecl.Get("parametersJsonSchema"); params.Exists() {
						cleaned := normalizeClaudeToolSchema(params)
						_ = toolDoc.SetRawBytes("input_schema", cleaned)
					} else {
						_ = toolDoc.SetRaw("input_schema", `{"type":"object","properties":{}}`)
					}

					anthropicTool := lowercaseClaudeToolSchemaTypes(toolDoc.Bytes())
					anthropicTools = append(anthropicTools, util.ParseGJSONBytesNoCopy(anthropicTool).Value())
					return true
				})
			}
			return true
		})

		if len(anthropicTools) > 0 {
			_ = outDoc.Set("tools", anthropicTools)
		}
	}

	if toolConfig := root.Get("tool_config"); toolConfig.Exists() {
		setClaudeToolChoiceFromGeminiToolConfigDoc(outDoc, toolConfig.Get("function_calling_config"))
	} else if toolConfig := root.Get("toolConfig"); toolConfig.Exists() {
		setClaudeToolChoiceFromGeminiToolConfigDoc(outDoc, toolConfig.Get("functionCallingConfig"))
	}

	_ = outDoc.Set("stream", stream)

	return thinking.ApplyTranslatedSummaryToClaude(outDoc.Bytes(), rawJSON, "gemini", modelName), drops.Err()
}

func orderGeminiClaudeToolResults(messages [][]byte) {
	for i, message := range messages {
		root := util.ParseGJSONBytesNoCopy(message)
		if root.Get("role").String() != "user" {
			continue
		}
		var results, other [][]byte
		hasOther, needsReorder := false, false
		root.Get("content").ForEach(func(_, part ojson.Result) bool {
			if part.Get("type").String() == "tool_result" {
				results = append(results, []byte(part.Raw))
				needsReorder = needsReorder || hasOther
			} else {
				other = append(other, []byte(part.Raw))
				hasOther = true
			}
			return true
		})
		if needsReorder {
			messageDoc := ojson.NewDoc(message)
			_ = messageDoc.SetRawBytes("content", translatorcommon.JoinRawArray(append(results, other...)))
			messages[i] = messageDoc.Bytes()
		}
	}
}

func normalizeClaudeToolSchema(parameters ojson.Result) []byte {
	cleanedDoc := ojson.NewDocString(util.NormalizeJSONSchemaNumericConstraints(parameters.Raw))
	if parameters.Get("additionalProperties").Type != ojson.False {
		_ = cleanedDoc.Set("additionalProperties", false)
	}
	const schema = "http://json-schema.org/draft-07/schema#"
	currentSchema := parameters.Get("$schema")
	if currentSchema.Type != ojson.String || currentSchema.String() != schema {
		_ = cleanedDoc.Set("$schema", schema)
	}
	return cleanedDoc.Bytes()
}

func lowercaseClaudeToolSchemaTypes(tool []byte) []byte {
	var pathsToLower []string
	util.Walk(util.ParseGJSONBytesNoCopy(tool), "", "type", &pathsToLower)
	var toolDoc *ojson.Doc
	for _, path := range pathsToLower {
		typeValue := ojson.GetBytesNoCopy(tool, path)
		normalizedType := strings.ToLower(typeValue.String())
		if typeValue.Type == ojson.String && normalizedType == typeValue.String() {
			continue
		}
		if toolDoc == nil {
			toolDoc = ojson.NewDoc(tool)
		}
		_ = toolDoc.Set(path, normalizedType)
	}
	if toolDoc != nil {
		return toolDoc.Bytes()
	}
	return tool
}

func setClaudeToolChoiceFromGeminiToolConfigDoc(outDoc *ojson.Doc, funcCalling ojson.Result) {
	if !funcCalling.Exists() {
		return
	}
	mode := funcCalling.Get("mode")
	if !mode.Exists() {
		return
	}
	switch mode.String() {
	case "AUTO":
		_ = outDoc.SetRaw("tool_choice", `{"type":"auto"}`)
	case "NONE":
		_ = outDoc.SetRaw("tool_choice", `{"type":"none"}`)
	case "ANY":
		allowedNames := funcCalling.Get("allowedFunctionNames")
		if !allowedNames.Exists() {
			allowedNames = funcCalling.Get("allowed_function_names")
		}
		allowedNameItems := allowedNames.Array()
		if allowedNames.IsArray() && len(allowedNameItems) == 1 {
			choice := []byte(`{"type":"tool","name":""}`)
			choice, _ = ojson.SetBytes(choice, "name", util.SanitizeClaudeFunctionName(allowedNameItems[0].String()))
			_ = outDoc.SetRawBytes("tool_choice", choice)
		} else {
			_ = outDoc.SetRaw("tool_choice", `{"type":"any"}`)
		}
	}
}

func geminiClaudeInlineData(part ojson.Result) ojson.Result {
	inlineData := part.Get("inlineData")
	if inlineData.Exists() {
		return inlineData
	}
	return part.Get("inline_data")
}

func geminiClaudeFileData(part ojson.Result) ojson.Result {
	fileData := part.Get("fileData")
	if fileData.Exists() {
		return fileData
	}
	return part.Get("file_data")
}

func claudeContentPartFromGeminiInlineData(inlineData ojson.Result, keepPlaceholder bool) ([]byte, bool) {
	mimeType := inlineData.Get("mimeType").String()
	if mimeType == "" {
		mimeType = inlineData.Get("mime_type").String()
	}
	data := inlineData.Get("data").String()
	if mimeType == "" || data == "" {
		return nil, false
	}
	lowerMimeType := strings.ToLower(mimeType)
	switch {
	case strings.HasPrefix(lowerMimeType, "image/"):
		imageDoc := ojson.NewDoc([]byte(`{"type":"image","source":{"type":"base64","media_type":"","data":""}}`))
		_ = imageDoc.Set("source.media_type", mimeType)
		_ = imageDoc.Set("source.data", data)
		return imageDoc.Bytes(), true
	case strings.HasPrefix(lowerMimeType, "application/"), strings.HasPrefix(lowerMimeType, "text/"):
		docDoc := ojson.NewDoc([]byte(`{"type":"document","source":{"type":"base64","media_type":"","data":""}}`))
		_ = docDoc.Set("source.media_type", mimeType)
		_ = docDoc.Set("source.data", data)
		return docDoc.Bytes(), true
	default:
		if !keepPlaceholder {
			return nil, false
		}
		return claudeTextContentPart(fmt.Sprintf("Media content: inline data (Type: %s)", mimeType)), true
	}
}

func claudeContentPartFromGeminiFileData(fileData ojson.Result) ([]byte, bool) {
	fileURI := fileData.Get("fileUri").String()
	if fileURI == "" {
		fileURI = fileData.Get("file_uri").String()
	}
	if fileURI == "" {
		return nil, false
	}
	mimeType := fileData.Get("mimeType").String()
	if mimeType == "" {
		mimeType = fileData.Get("mime_type").String()
	}
	lowerMimeType := strings.ToLower(mimeType)
	switch {
	case strings.HasPrefix(lowerMimeType, "image/"):
		imageContent := []byte(`{"type":"image","source":{"type":"url","url":""}}`)
		imageContent, _ = ojson.SetBytes(imageContent, "source.url", fileURI)
		return imageContent, true
	case strings.HasPrefix(lowerMimeType, "application/"), strings.HasPrefix(lowerMimeType, "text/"):
		docDoc := ojson.NewDoc([]byte(`{"type":"document","source":{"type":"url","url":""}}`))
		_ = docDoc.Set("source.url", fileURI)
		if mimeType != "" {
			_ = docDoc.Set("source.media_type", mimeType)
		}
		return docDoc.Bytes(), true
	default:
		fileInfo := "File: " + fileURI
		if mimeType != "" {
			fileInfo += " (Type: " + mimeType + ")"
		}
		return claudeTextContentPart(fileInfo), true
	}
}

func claudeTextContentPart(text string) []byte {
	textContent := []byte(`{"type":"text","text":""}`)
	textContent, _ = ojson.SetBytes(textContent, "text", text)
	return textContent
}
