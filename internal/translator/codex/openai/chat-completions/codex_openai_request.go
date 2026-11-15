





package chat_completions

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	applypatch "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/apply-patch"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)













func ConvertOpenAIRequestToCodex(modelName string, inputRawJSON []byte, stream bool) ([]byte, error) {
	return convertOpenAIRequestToCodex(modelName, inputRawJSON, stream)

}



func convertOpenAIRequestToCodex(modelName string, inputRawJSON []byte, stream bool) ([]byte, error) {
	rawJSON := inputRawJSON
	var drops translatorcommon.UserTurnDrops
	root := util.ParseGJSONBytesNoCopy(rawJSON)
	tools := root.Get("tools")
	toolResults := tools.Array()

	outDoc := ojson.NewDocString(`{"instructions":""}`)


	_ = outDoc.Set("stream", stream)

	for _, path := range []string{"max_completion_tokens", "max_tokens", "max_output_tokens"} {
		if v := root.Get(path); v.Type == ojson.Number {
			_ = outDoc.Set("max_output_tokens", v.Int())
			break
		}
	}


	if v := root.Get("reasoning_effort"); v.Exists() {
		_ = outDoc.Set("reasoning.effort", v.Value())
	} else {
		_ = outDoc.Set("reasoning.effort", "medium")
	}
	if serviceTier := normalizeCodexServiceTier(root.Get("service_tier")); serviceTier != "" {
		_ = outDoc.Set("service_tier", serviceTier)
	}
	_ = outDoc.Set("parallel_tool_calls", root.Get("parallel_tool_calls").Type != ojson.False)



	_ = outDoc.Set("include", []string{"reasoning.encrypted_content"})


	_ = outDoc.Set("model", modelName)


	originalToolNameMap := map[string]string{}
	customToolNames := map[string]struct{}{}
	functionToolNames := map[string]struct{}{}
	{
		if tools.IsArray() && len(toolResults) > 0 {
			for _, tool := range toolResults {
				switch tool.Get("type").String() {
				case "function":
					functionToolNames[tool.Get("function.name").String()] = struct{}{}
				case "custom":
					customToolNames[customToolDefinition(tool).Get("name").String()] = struct{}{}
				}
			}


			for name := range functionToolNames {
				delete(customToolNames, name)
			}
		}
		allNames := collectRequestToolNames(rawJSON)
		if len(allNames) > 0 {
			originalToolNameMap = buildShortNameMap(allNames)
		}
	}

	resolveToolCall := func(toolCall ojson.Result) (callType, name, input string, valid bool) {
		switch toolCall.Get("type").String() {
		case "custom":
			return "custom", toolCall.Get("custom.name").String(), toolCall.Get("custom.input").String(), true
		case "function":
			name = toolCall.Get("function.name").String()
			callType = "function"
			if _, custom := customToolNames[name]; custom {
				callType = "custom"
			}
			input = toolCall.Get("function.arguments").String()
			if callType == "custom" && strings.TrimSpace(name) == "apply_patch" {

				if unwrapped, errUnwrap := applypatch.UnwrapInput(input); errUnwrap == nil {
					input = unwrapped
				}
			}
			return callType, name, input, true
		default:
			return "", "", "", false
		}
	}


	messages := root.Get("messages")
	type pendingToolCall struct {
		callID       string
		sourceCallID string
		callType     string
		consumed     bool
	}
	var pendingToolCalls []pendingToolCall
	ambiguousToolCallIDs := map[string]struct{}{}


	inputItems := translatorcommon.NewRawArrayItems(messages.Get("#").Int())
	if messages.IsArray() {
		arr := messages.Array()
		for i := 0; i < len(arr); i++ {
			m := arr[i]
			role := m.Get("role").String()

			switch role {
			case "tool":

				toolCallID := m.Get("tool_call_id").String()
				if _, ambiguous := ambiguousToolCallIDs[toolCallID]; toolCallID != "" && ambiguous {
					continue
				}

				pendingIndex := -1
				for index := range pendingToolCalls {
					pendingCall := &pendingToolCalls[index]
					if pendingCall.consumed {
						continue
					}
					if toolCallID == "" || pendingCall.sourceCallID == toolCallID || pendingCall.callID == toolCallID {
						pendingIndex = index
						break
					}
				}

				if pendingIndex < 0 {
					continue
				}
				pendingCall := &pendingToolCalls[pendingIndex]
				pendingCall.consumed = true
				toolCallID = pendingCall.callID
				outputType := "function_call_output"
				if pendingCall.callType == "custom" {
					outputType = "custom_tool_call_output"
				}

				toolOutputDoc := ojson.NewDocString(`{}`)
				_ = toolOutputDoc.Set("type", outputType)
				_ = toolOutputDoc.Set("call_id", toolCallID)
				setToolCallOutputContentDoc(toolOutputDoc, m.Get("content"))
				inputItems = append(inputItems, toolOutputDoc.Bytes())

			default:

				pendingToolCalls = nil
				ambiguousToolCallIDs = map[string]struct{}{}


				msgDoc := ojson.NewDocString(`{}`)
				_ = msgDoc.Set("type", "message")
				if role == "system" {
					_ = msgDoc.Set("role", "developer")
				} else {
					_ = msgDoc.Set("role", role)
				}

				contentItems := make([][]byte, 0, 4)

				turnSendable := 0


				c := m.Get("content")
				if c.Exists() && c.Type == ojson.String && c.String() != "" {
					turnSendable++

					partType := "input_text"
					if role == "assistant" {
						partType = "output_text"
					}
					partDoc := ojson.NewDocString(`{}`)
					_ = partDoc.Set("type", partType)
					_ = partDoc.Set("text", c.String())
					contentItems = append(contentItems, partDoc.Bytes())
				} else if c.Exists() && c.IsArray() {
					items := c.Array()
					for j := 0; j < len(items); j++ {
						it := items[j]
						t := it.Get("type").String()
						switch t {
						case "text":
							partType := "input_text"
							if role == "assistant" {
								partType = "output_text"
							}
							partDoc := ojson.NewDocString(`{}`)
							_ = partDoc.Set("type", partType)
							_ = partDoc.Set("text", it.Get("text").String())
							contentItems = append(contentItems, partDoc.Bytes())
							if it.Get("text").String() != "" {
								turnSendable++
							}
						case "image_url":

							if role == "user" {
								partDoc := ojson.NewDocString(`{}`)
								_ = partDoc.Set("type", "input_image")
								if u := it.Get("image_url.url"); u.Exists() {
									_ = partDoc.Set("image_url", u.String())
								}
								contentItems = append(contentItems, partDoc.Bytes())
								turnSendable++
							}
						case "file":
							if role == "user" {
								if part, ok := codexInputFilePart(it); ok {
									contentItems = append(contentItems, part)
									turnSendable++
								} else {
									drops.Drop(t)
								}
							}
						case "input_audio":
							if role == "user" {
								audioData := it.Get("input_audio.data").String()
								audioFormat := it.Get("input_audio.format").String()
								if audioData != "" {
									partDoc := ojson.NewDocString(`{}`)
									_ = partDoc.Set("type", "input_audio")
									_ = partDoc.Set("data", audioData)
									if audioFormat != "" {
										_ = partDoc.Set("format", audioFormat)
									}
									contentItems = append(contentItems, partDoc.Bytes())
									turnSendable++
								} else {
									drops.Drop(t)
								}
							}
						}
					}
				}
				if role == "user" {
					drops.EndTurn(turnSendable)
				}




				if role != "assistant" || len(contentItems) > 0 {
					_ = msgDoc.SetRawBytes("content", translatorcommon.JoinRawArray(contentItems))
					inputItems = append(inputItems, msgDoc.Bytes())
				}


				if role == "assistant" {
					toolCalls := m.Get("tool_calls")
					if toolCalls.Exists() && toolCalls.IsArray() {
						toolCallsArr := toolCalls.Array()
						callIDCounts := map[string]int{}
						usedCallIDs := map[string]struct{}{}
						for _, tc := range toolCallsArr {
							_, _, _, valid := resolveToolCall(tc)
							callID := tc.Get("id").String()
							if valid && callID != "" {
								callIDCounts[callID]++
								usedCallIDs[callID] = struct{}{}
							}
						}
						for callID, count := range callIDCounts {
							if count > 1 {
								ambiguousToolCallIDs[callID] = struct{}{}
							}
						}

						for j := 0; j < len(toolCallsArr); j++ {
							tc := toolCallsArr[j]
							toolCallType, toolCallName, toolCallInput, valid := resolveToolCall(tc)
							if !valid {
								continue
							}
							sourceCallID := tc.Get("id").String()
							if _, ambiguous := ambiguousToolCallIDs[sourceCallID]; sourceCallID != "" && ambiguous {
								continue
							}
							callID := sourceCallID
							if callID == "" {
								baseCallID := "call_missing_" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
								callID = baseCallID
								for suffix := 1; ; suffix++ {
									if _, used := usedCallIDs[callID]; !used {
										break
									}
									callID = baseCallID + "_" + strconv.Itoa(suffix)
								}
								usedCallIDs[callID] = struct{}{}
							}

							if shortID := shortenCodexCallIDIfNeeded(callID); shortID != callID {
								for attempt := 1; ; attempt++ {
									if _, used := usedCallIDs[shortID]; !used {
										break
									}
									shortID = shortenCodexCallIDIfNeeded(callID + "_" + strconv.Itoa(attempt))
								}
								usedCallIDs[shortID] = struct{}{}
								callID = shortID
							}
							pendingToolCalls = append(pendingToolCalls, pendingToolCall{
								callID:       callID,
								sourceCallID: sourceCallID,
								callType:     toolCallType,
							})

							switch toolCallType {
							case "function":

								funcCallDoc := ojson.NewDocString(`{}`)
								_ = funcCallDoc.Set("type", "function_call")
								_ = funcCallDoc.Set("call_id", callID)
								if short, ok := originalToolNameMap[toolCallName]; ok {
									toolCallName = short
								} else {
									toolCallName = shortenNameIfNeeded(toolCallName)
								}
								_ = funcCallDoc.Set("name", toolCallName)
								_ = funcCallDoc.Set("arguments", toolCallInput)
								inputItems = append(inputItems, funcCallDoc.Bytes())
							case "custom":
								customCallDoc := ojson.NewDocString(`{}`)
								_ = customCallDoc.Set("type", "custom_tool_call")
								_ = customCallDoc.Set("call_id", callID)
								if short, ok := originalToolNameMap[toolCallName]; ok {
									toolCallName = short
								} else {
									toolCallName = shortenNameIfNeeded(toolCallName)
								}
								_ = customCallDoc.Set("name", toolCallName)
								_ = customCallDoc.Set("input", toolCallInput)
								inputItems = append(inputItems, customCallDoc.Bytes())
							}
						}
					}
				}
			}
		}
	}
	translatorcommon.SetRawArrayItemsDoc(outDoc, "input", inputItems)


	rf := root.Get("response_format")
	text := root.Get("text")
	if rf.Exists() {

		_ = outDoc.SetRawBytes("text", []byte(`{}`))

		rft := rf.Get("type").String()
		switch rft {
		case "text":
			_ = outDoc.Set("text.format.type", "text")
		case "json_schema":
			js := rf.Get("json_schema")
			if js.Exists() {
				_ = outDoc.Set("text.format.type", "json_schema")
				if v := js.Get("name"); v.Exists() {
					_ = outDoc.Set("text.format.name", v.Value())
				}
				if v := js.Get("strict"); v.Exists() {
					_ = outDoc.Set("text.format.strict", v.Value())
				}
				if v := js.Get("schema"); v.Exists() {
					_ = outDoc.SetRawBytes("text.format.schema", []byte(v.Raw))
				}
			}
		}


		if text.Exists() {
			if v := text.Get("verbosity"); v.Exists() {
				_ = outDoc.Set("text.verbosity", v.Value())
			}
		}
	} else if text.Exists() {

		if v := text.Get("verbosity"); v.Exists() {
			_ = outDoc.SetRawBytes("text", []byte(`{}`))
			_ = outDoc.Set("text.verbosity", v.Value())
		}
	}


	if tools.IsArray() && len(toolResults) > 0 {
		toolItems := make([][]byte, 0, len(toolResults))
		arr := toolResults
		for i := 0; i < len(arr); i++ {
			t := arr[i]
			toolType := t.Get("type").String()
			if toolType == "custom" {
				definition := customToolDefinition(t)
				itemDoc := ojson.NewDocString(definition.Raw)
				_ = itemDoc.Set("type", "custom")
				if grammar := definition.Get("format.grammar"); definition.Get("format.type").String() == "grammar" && grammar.IsObject() {
					_ = itemDoc.SetRaw("format", grammar.Raw)
					_ = itemDoc.Set("format.type", "grammar")
				}
				name := definition.Get("name").String()
				if short, ok := originalToolNameMap[name]; ok {
					name = short
				} else {
					name = shortenNameIfNeeded(name)
				}
				_ = itemDoc.Set("name", name)
				toolItems = append(toolItems, itemDoc.Bytes())
				continue
			}



			if toolType != "" && toolType != "function" && t.IsObject() {
				toolItems = append(toolItems, []byte(t.Raw))
				continue
			}

			if toolType == "function" {
				itemDoc := ojson.NewDocString(`{}`)
				_ = itemDoc.Set("type", "function")
				fn := t.Get("function")
				if fn.Exists() {
					if v := fn.Get("name"); v.Exists() {
						name := v.String()
						if short, ok := originalToolNameMap[name]; ok {
							name = short
						} else {
							name = shortenNameIfNeeded(name)
						}
						_ = itemDoc.Set("name", name)
					}
					if v := fn.Get("description"); v.Exists() {
						_ = itemDoc.Set("description", v.Value())
					}
					if v := fn.Get("parameters"); v.Exists() {
						_ = itemDoc.SetRawBytes("parameters", []byte(v.Raw))
					}
					if v := fn.Get("strict"); v.Exists() {
						_ = itemDoc.Set("strict", v.Value())
					} else {


						_ = itemDoc.Set("strict", false)
					}
				}
				toolItems = append(toolItems, itemDoc.Bytes())
			}
		}
		_ = outDoc.SetRawBytes("tools", translatorcommon.JoinRawArray(toolItems))
	}




	if tc := root.Get("tool_choice"); tc.Exists() {
		switch {
		case tc.Type == ojson.String:
			_ = outDoc.Set("tool_choice", tc.String())
		case tc.IsObject():
			tcType := tc.Get("type").String()
			if tcType == "function" || tcType == "custom" {
				name := customToolDefinition(tc).Get("name").String()
				if tcType == "function" {
					name = tc.Get("function.name").String()
					if _, custom := customToolNames[name]; custom {
						tcType = "custom"
					}
				}
				if name != "" {
					if short, ok := originalToolNameMap[name]; ok {
						name = short
					} else {
						name = shortenNameIfNeeded(name)
					}
				}
				choiceDoc := ojson.NewDocString(`{}`)
				_ = choiceDoc.Set("type", tcType)
				if name != "" {
					_ = choiceDoc.Set("name", name)
				}
				_ = outDoc.SetRawBytes("tool_choice", choiceDoc.Bytes())
			} else if tcType != "" {

				_ = outDoc.SetRawBytes("tool_choice", []byte(tc.Raw))
			}
		}
	}

	_ = outDoc.Set("store", false)
	return outDoc.Bytes(), drops.Err()
}

func setToolCallOutputContentDoc(doc *ojson.Doc, content ojson.Result) {
	switch {
	case content.Type == ojson.String:
		structuredContent := ojson.Parse(content.String())
		if hasToolOutputImagePart(structuredContent) {
			setToolCallOutputContentDoc(doc, structuredContent)
			return
		}
		_ = doc.Set("output", content.String())
	case content.IsArray():
		outputItems := make([][]byte, 0, 4)
		for _, item := range content.Array() {
			outputItems = append(outputItems, toolOutputContentPart(item))
		}
		_ = doc.SetRawBytes("output", translatorcommon.JoinRawArray(outputItems))
	default:
		fallbackOutput := content.Raw
		if fallbackOutput == "" {
			fallbackOutput = content.String()
		}
		_ = doc.Set("output", fallbackOutput)
	}
}

func toolOutputContentPart(item ojson.Result) []byte {
	itemType := item.Get("type").String()
	switch itemType {
	case "text", "input_text", "output_text":
		partDoc := ojson.NewDocString(`{}`)
		_ = partDoc.Set("type", "input_text")
		_ = partDoc.Set("text", item.Get("text").String())
		return partDoc.Bytes()
	case "image_url", "input_image":
		imageURL := item.Get("image_url.url").String()
		fileID := item.Get("image_url.file_id").String()
		if itemType == "input_image" {
			imageURL = item.Get("image_url").String()
			fileID = item.Get("file_id").String()
		}
		if imageURL == "" && fileID == "" {
			return toolOutputFallbackPart(item)
		}
		partDoc := ojson.NewDocString(`{}`)
		_ = partDoc.Set("type", "input_image")
		if imageURL != "" {
			_ = partDoc.Set("image_url", imageURL)
		}
		if fileID != "" {
			_ = partDoc.Set("file_id", fileID)
		}
		detail := item.Get("image_url.detail").String()
		if itemType == "input_image" {
			detail = item.Get("detail").String()
		}
		if detail != "" {
			_ = partDoc.Set("detail", detail)
		}
		return partDoc.Bytes()
	case "file":
		if part, ok := codexInputFilePart(item); ok {
			return part
		}
		return toolOutputFallbackPart(item)
	default:
		return toolOutputFallbackPart(item)
	}
}



func codexInputFilePart(item ojson.Result) ([]byte, bool) {
	fileID := item.Get("file.file_id").String()
	fileData := item.Get("file.file_data").String()
	fileURL := item.Get("file.file_url").String()
	if fileID == "" && fileData == "" && fileURL == "" {
		return nil, false
	}
	partDoc := ojson.NewDocString(`{}`)
	_ = partDoc.Set("type", "input_file")
	if fileID != "" {
		_ = partDoc.Set("file_id", fileID)
	}
	if fileData != "" {
		_ = partDoc.Set("file_data", fileData)
	}
	if fileURL != "" {
		_ = partDoc.Set("file_url", fileURL)
	}
	if filename := item.Get("file.filename").String(); filename != "" {
		_ = partDoc.Set("filename", filename)
	}
	return partDoc.Bytes(), true
}

func hasToolOutputImagePart(content ojson.Result) bool {
	if !content.IsArray() {
		return false
	}
	for _, item := range content.Array() {
		switch item.Get("type").String() {
		case "image_url":
			if item.Get("image_url.url").String() != "" || item.Get("image_url.file_id").String() != "" {
				return true
			}
		case "input_image":
			if item.Get("image_url").String() != "" || item.Get("file_id").String() != "" {
				return true
			}
		}
	}
	return false
}

func toolOutputFallbackPart(item ojson.Result) []byte {
	text := item.Raw
	if text == "" {
		text = item.String()
	}
	partDoc := ojson.NewDocString(`{}`)
	_ = partDoc.Set("type", "input_text")
	_ = partDoc.Set("text", text)
	return partDoc.Bytes()
}



func sanitizeToolName(name string) string {
	if name == "" {
		return ""
	}
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}




func shortenNameIfNeeded(name string) string {
	const limit = 64
	sanitized := sanitizeToolName(name)
	if len(sanitized) <= limit {
		return sanitized
	}
	if strings.HasPrefix(sanitized, "mcp__") {

		idx := strings.LastIndex(sanitized, "__")
		if idx > 0 {
			candidate := "mcp__" + sanitized[idx+2:]
			if len(candidate) > limit {
				return candidate[:limit]
			}
			return candidate
		}
	}
	return sanitized[:limit]
}


func customToolDefinition(tool ojson.Result) ojson.Result {
	if custom := tool.Get("custom"); custom.IsObject() {
		return custom
	}
	return tool
}



func collectRequestToolNames(rawJSON []byte) []string {
	var names []string
	seen := map[string]struct{}{}
	addName := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; !ok {
			n := strings.Clone(name)
			names = append(names, n)
			seen[n] = struct{}{}
		}
	}


	tools := ojson.GetBytesNoCopy(rawJSON, "tools")
	if tools.IsArray() {
		for _, tool := range tools.Array() {
			switch tool.Get("type").String() {
			case "function":
				addName(tool.Get("function.name").String())
			case "custom":
				addName(customToolDefinition(tool).Get("name").String())
			}
		}
	}


	tc := ojson.GetBytesNoCopy(rawJSON, "tool_choice")
	if tc.IsObject() {
		switch tc.Get("type").String() {
		case "function":
			fnName := tc.Get("function.name").String()
			if fnName == "" {
				fnName = tc.Get("name").String()
			}
			addName(fnName)
		case "custom":
			addName(customToolDefinition(tc).Get("name").String())
		}
	}


	messages := ojson.GetBytesNoCopy(rawJSON, "messages")
	if messages.IsArray() {
		for _, msg := range messages.Array() {
			if msg.Get("role").String() == "assistant" {
				toolCalls := msg.Get("tool_calls")
				if toolCalls.IsArray() {
					for _, tc := range toolCalls.Array() {
						fnName := tc.Get("function.name").String()
						if fnName != "" {
							addName(fnName)
						} else {
							addName(tc.Get("custom.name").String())
						}
					}
				}
			}
		}
	}

	return names
}




func buildShortNameMap(names []string) map[string]string {
	const limit = 64
	used := map[string]struct{}{}
	m := map[string]string{}

	baseCandidate := func(n string) string {
		return shortenNameIfNeeded(n)
	}

	makeUnique := func(cand string) string {
		if _, ok := used[cand]; !ok {
			return cand
		}
		base := cand
		for i := 1; ; i++ {
			suffix := "_" + strconv.Itoa(i)
			allowed := limit - len(suffix)
			if allowed < 0 {
				allowed = 0
			}
			tmp := base
			if len(tmp) > allowed {
				tmp = tmp[:allowed]
			}
			tmp = tmp + suffix
			if _, ok := used[tmp]; !ok {
				return tmp
			}
		}
	}

	for _, n := range names {
		cand := baseCandidate(n)
		uniq := makeUnique(cand)
		used[uniq] = struct{}{}
		m[n] = uniq
	}
	return m
}

func normalizeCodexServiceTier(result ojson.Result) string {
	if !result.Exists() || result.Type != ojson.String {
		return ""
	}

	switch strings.ToLower(strings.TrimSpace(result.String())) {
	case "fast", "priority":
		return "priority"
	case "ultrafast":
		return "ultrafast"
	default:
		return ""
	}
}



func shortenCodexCallIDIfNeeded(id string) string {
	const limit = 64
	if len(id) <= limit {
		return id
	}

	sum := sha256.Sum256([]byte(id))
	suffix := "_" + hex.EncodeToString(sum[:8])
	return id[:limit-len(suffix)] + suffix
}
