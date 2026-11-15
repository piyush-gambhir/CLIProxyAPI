package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/MoeForge/moego/ojson"
	applypatch "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/apply-patch"
)


type ResponsesToolIdentity struct {
	Name       string
	Namespace  string
	Custom     bool
	ApplyPatch bool
	ToolSearch bool
}

const (

	ResponsesToolSearchType = "tool_search"

	ResponsesToolSearchExecutionClient = "client"


	ResponsesToolSearchFunctionName = "tool_search"



	ResponsesClientToolSearchKey = "\x00client_tool_search"

	ResponsesToolSearchCallType = "tool_search_call"

	ResponsesToolSearchOutputType = "tool_search_output"

	ResponsesDiscoveredToolPriority = 2

	responsesToolSearchDefaultParameters = `{"type":"object","properties":{"query":{"type":"string","description":"Search query for deferred tools."},"limit":{"type":"number","description":"Maximum number of tools to return."}},"required":["query"],"additionalProperties":false}`
)


type ResponsesToolDescriptor struct {
	Name           string
	LocalName      string
	Namespace      string
	ToolType       string
	Tool           ojson.Result
	SourcePriority int
	Direct         bool
	Order          int
	ClientSearch   bool
}


func QualifyResponsesNamespaceToolName(namespaceName, childName string) string {
	childName = strings.TrimSpace(childName)
	namespaceName = strings.TrimSpace(namespaceName)
	if childName == "" || namespaceName == "" || strings.HasPrefix(childName, "mcp__") {
		return childName
	}
	if childName == namespaceName || strings.HasPrefix(childName, namespaceName+"__") {
		return childName
	}
	if strings.HasSuffix(namespaceName, "__") {
		return namespaceName + childName
	}
	return namespaceName + "__" + childName
}

func responsesToolSources(root ojson.Result, includeClientToolSearch bool) []struct {
	tools    ojson.Result
	priority int
} {
	return responsesToolSourcesFromGetter(root.Get, includeClientToolSearch)
}

func responsesToolSourcesFromGetter(get func(string) ojson.Result, includeClientToolSearch bool) []struct {
	tools    ojson.Result
	priority int
} {
	if get == nil {
		return nil
	}
	var sources []struct {
		tools    ojson.Result
		priority int
	}
	appendSource := func(tools ojson.Result, priority int) {
		if tools.Exists() && tools.IsArray() {
			sources = append(sources, struct {
				tools    ojson.Result
				priority int
			}{tools: tools, priority: priority})
		}
	}
	appendSource(get("tools"), 0)
	if input := get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item ojson.Result) bool {
			if item.Get("type").String() == "additional_tools" {
				appendSource(item.Get("tools"), 1)
			}
			return true
		})
		if includeClientToolSearch {
			input.ForEach(func(_, item ojson.Result) bool {
				if item.Get("type").String() == ResponsesToolSearchOutputType && IsResponsesClientToolSearchItem(item) {
					appendSource(item.Get("tools"), ResponsesDiscoveredToolPriority)
				}
				return true
			})
		}
	}
	return sources
}



func IsResponsesClientToolSearchDeclaration(tool ojson.Result) bool {
	return strings.TrimSpace(tool.Get("type").String()) == ResponsesToolSearchType &&
		strings.TrimSpace(tool.Get("execution").String()) == ResponsesToolSearchExecutionClient
}




func IsResponsesClientToolSearchItem(item ojson.Result) bool {
	execution := strings.TrimSpace(item.Get("execution").String())
	return execution == "" || execution == ResponsesToolSearchExecutionClient
}

func responsesToolName(tool ojson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("function.name").String())
}


func ResponsesToolDescription(tool ojson.Result) string {
	if description := tool.Get("description").String(); description != "" {
		return description
	}
	return tool.Get("function.description").String()
}

func responsesToolDescription(tool ojson.Result) string {
	return ResponsesToolDescription(tool)
}


func ResponsesToolParameters(tool ojson.Result) ojson.Result {
	for _, path := range []string{
		"parameters",
		"parametersJsonSchema",
		"input_schema",
		"function.parameters",
		"function.parametersJsonSchema",
	} {
		if parameters := tool.Get(path); parameters.Exists() {
			return parameters
		}
	}
	return ojson.Result{}
}

func responsesToolParameters(tool ojson.Result) ojson.Result {
	return ResponsesToolParameters(tool)
}


func CollectResponsesToolDescriptors(root ojson.Result) []ResponsesToolDescriptor {
	return collectResponsesToolDescriptors(root, false)
}


func CollectResponsesToolDescriptorsDoc(doc *ojson.Doc) []ResponsesToolDescriptor {
	if doc == nil || doc.Len() == 0 {
		return nil
	}
	return collectResponsesToolDescriptorsFromGetter(doc.Get, false)
}

func collectResponsesToolDescriptors(root ojson.Result, includeClientToolSearch bool) []ResponsesToolDescriptor {
	return collectResponsesToolDescriptorsFromGetter(root.Get, includeClientToolSearch)
}

func collectResponsesToolDescriptorsFromGetter(get func(string) ojson.Result, includeClientToolSearch bool) []ResponsesToolDescriptor {
	var descriptors []ResponsesToolDescriptor
	appendDescriptor := func(tool ojson.Result, name, localName, namespace string, toolType string, sourcePriority int, direct bool, clientSearch bool) {
		if name == "" {
			return
		}
		descriptors = append(descriptors, ResponsesToolDescriptor{
			Name:           name,
			LocalName:      localName,
			Namespace:      namespace,
			ToolType:       toolType,
			Tool:           tool,
			SourcePriority: sourcePriority,
			Direct:         direct,
			Order:          len(descriptors),
			ClientSearch:   clientSearch,
		})
	}
	appendNamespaceChildren := func(namespaceTool ojson.Result, sourcePriority int) {
		namespaceName := strings.TrimSpace(namespaceTool.Get("name").String())
		children := namespaceTool.Get("tools")
		if !children.Exists() || !children.IsArray() {
			children = namespaceTool.Get("children")
		}
		if !children.Exists() || !children.IsArray() {
			return
		}
		children.ForEach(func(_, child ojson.Result) bool {
			childName := responsesToolName(child)
			if childName == "" {
				return true
			}
			qualifiedName := QualifyResponsesNamespaceToolName(namespaceName, childName)
			switch strings.TrimSpace(child.Get("type").String()) {
			case "", "function":
				appendDescriptor(child, qualifiedName, childName, namespaceName, "function", sourcePriority, false, false)
			case "custom":
				appendDescriptor(child, qualifiedName, childName, namespaceName, "custom", sourcePriority, false, false)
			}
			return true
		})
	}
	for _, source := range responsesToolSourcesFromGetter(get, includeClientToolSearch) {
		source.tools.ForEach(func(_, tool ojson.Result) bool {
			toolType := strings.TrimSpace(tool.Get("type").String())
			switch toolType {
			case "", "function":
				name := responsesToolName(tool)
				appendDescriptor(tool, name, name, "", "function", source.priority, true, false)
			case "custom":
				name := responsesToolName(tool)
				appendDescriptor(tool, name, name, "", "custom", source.priority, true, false)
			case "namespace":
				appendNamespaceChildren(tool, source.priority)
			case ResponsesToolSearchType:
				if includeClientToolSearch && source.priority < ResponsesDiscoveredToolPriority && IsResponsesClientToolSearchDeclaration(tool) {
					appendDescriptor(tool, ResponsesToolSearchFunctionName, ResponsesToolSearchFunctionName, "", ResponsesToolSearchType, source.priority, true, true)
				}
			}
			return true
		})
	}

	if !includeClientToolSearch || !responsesHasClientToolSearch(descriptors) {
		return descriptors
	}
	kept := make([]ResponsesToolDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if !descriptor.ClientSearch && descriptor.SourcePriority != ResponsesDiscoveredToolPriority && descriptor.Tool.Get("defer_loading").Bool() {
			continue
		}
		descriptor.Order = len(kept)
		kept = append(kept, descriptor)
	}
	return kept
}

func responsesHasClientToolSearch(descriptors []ResponsesToolDescriptor) bool {
	for i := range descriptors {
		if descriptors[i].ClientSearch {
			return true
		}
	}
	return false
}

func responsesToolDescriptorPrecedes(left, right ResponsesToolDescriptor) bool {
	if left.SourcePriority != right.SourcePriority {
		return left.SourcePriority < right.SourcePriority
	}
	if left.Direct != right.Direct {
		return left.Direct
	}
	return left.Order < right.Order
}


func CollectResponsesToolWinners(root ojson.Result) map[string]ResponsesToolDescriptor {
	return collectResponsesToolWinnersFromDescriptors(CollectResponsesToolDescriptors(root))
}


func CollectResponsesToolWinnersDoc(doc *ojson.Doc) map[string]ResponsesToolDescriptor {
	return collectResponsesToolWinnersFromDescriptors(CollectResponsesToolDescriptorsDoc(doc))
}

func collectResponsesToolWinnersFromDescriptors(descriptors []ResponsesToolDescriptor) map[string]ResponsesToolDescriptor {
	winners := map[string]ResponsesToolDescriptor{}
	for _, descriptor := range descriptors {
		current, exists := winners[descriptor.Name]
		if !exists || responsesToolDescriptorPrecedes(descriptor, current) {
			winners[descriptor.Name] = descriptor
		}
	}
	return winners
}

func sanitizeResponsesToolNames(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	uniqueNames := make(map[string]struct{}, len(names))
	baseCounts := make(map[string]int, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, exists := uniqueNames[name]; exists {
			continue
		}
		uniqueNames[name] = struct{}{}
		baseCounts[SanitizeFunctionName(name)]++
	}

	sortedNames := make([]string, 0, len(uniqueNames))
	for name := range uniqueNames {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	out := make(map[string]string, len(sortedNames))
	used := make(map[string]string, len(sortedNames))
	for _, name := range sortedNames {
		base := SanitizeFunctionName(name)
		mapped := base
		_, baseUsed := used[base]
		if baseCounts[base] > 1 || baseUsed {
			mapped = disambiguateResponsesSanitizedName(base, name, used)
		}
		out[name] = mapped
		used[mapped] = name
	}
	return out
}

func disambiguateResponsesSanitizedName(base, original string, used map[string]string) string {
	for attempt := 0; ; attempt++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", original, attempt)))
		suffix := "_" + hex.EncodeToString(digest[:6])
		prefix := base
		if maxPrefix := 64 - len(suffix); len(prefix) > maxPrefix {
			prefix = prefix[:maxPrefix]
		}
		candidate := prefix + suffix
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

type responsesToolWinnerKey struct {
	name         string
	clientSearch bool
}


func BuildGeminiFunctionDeclarations(root ojson.Result) ([][]byte, map[string]string, map[string]ResponsesToolIdentity) {
	return buildGeminiFunctionDeclarations(root, true)
}



func buildGeminiFunctionDeclarations(root ojson.Result, withDeclarations bool) ([][]byte, map[string]string, map[string]ResponsesToolIdentity) {
	descriptors := collectResponsesToolDescriptors(root, true)
	winners := make(map[responsesToolWinnerKey]ResponsesToolDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		key := responsesToolWinnerKey{name: descriptor.Name, clientSearch: descriptor.ClientSearch}
		current, exists := winners[key]
		if !exists || responsesToolDescriptorPrecedes(descriptor, current) {
			winners[key] = descriptor
		}
	}

	seenKeys := make(map[responsesToolWinnerKey]struct{}, len(winners))
	var winningList []ResponsesToolDescriptor
	for _, descriptor := range descriptors {
		key := responsesToolWinnerKey{name: descriptor.Name, clientSearch: descriptor.ClientSearch}
		winner, ok := winners[key]
		if !ok || winner.Order != descriptor.Order {
			continue
		}
		if _, seen := seenKeys[key]; seen {
			continue
		}
		seenKeys[key] = struct{}{}
		winningList = append(winningList, descriptor)
	}

	hasClientSearchHistory := false
	if include := root.Get("input"); include.Exists() && include.IsArray() {
		include.ForEach(func(_, item ojson.Result) bool {
			typ := item.Get("type").String()
			if (typ == ResponsesToolSearchCallType || typ == ResponsesToolSearchOutputType) && IsResponsesClientToolSearchItem(item) {
				hasClientSearchHistory = true
				return false
			}
			return true
		})
	}

	if len(winningList) == 0 {
		if !hasClientSearchHistory {
			return nil, nil, nil
		}
		forwardMap := map[string]string{
			ResponsesClientToolSearchKey:    ResponsesToolSearchFunctionName,
			ResponsesToolSearchFunctionName: ResponsesToolSearchFunctionName,
		}
		reverseMap := map[string]ResponsesToolIdentity{
			ResponsesToolSearchFunctionName: {
				Name:       ResponsesToolSearchFunctionName,
				ToolSearch: true,
			},
		}
		return nil, forwardMap, reverseMap
	}

	hasClientSearch := false
	userOwnsExactToolSearch := false
	qualifiedNames := make([]string, 0, len(winningList))
	for _, desc := range winningList {
		if desc.ClientSearch {
			hasClientSearch = true
			continue
		}
		if desc.Name == ResponsesToolSearchFunctionName {
			userOwnsExactToolSearch = true
		}
		qualifiedNames = append(qualifiedNames, desc.Name)
	}
	sanitizedMap := sanitizeResponsesToolNames(qualifiedNames)
	if sanitizedMap == nil {
		sanitizedMap = make(map[string]string, len(winningList))
	}

	clientSearchGeminiName := ""
	if hasClientSearch || hasClientSearchHistory {
		used := make(map[string]string, len(sanitizedMap)+1)
		for origName, mappedName := range sanitizedMap {
			used[mappedName] = origName
		}
		if !userOwnsExactToolSearch {


			if collidingOrig, taken := used[ResponsesToolSearchFunctionName]; taken && collidingOrig != ResponsesToolSearchFunctionName {
				used[ResponsesToolSearchFunctionName] = ResponsesClientToolSearchKey
				newMapped := disambiguateResponsesSanitizedName(ResponsesToolSearchFunctionName, collidingOrig, used)
				sanitizedMap[collidingOrig] = newMapped
				used[newMapped] = collidingOrig
			} else {
				used[ResponsesToolSearchFunctionName] = ResponsesClientToolSearchKey
			}
			clientSearchGeminiName = ResponsesToolSearchFunctionName
		} else {
			clientSearchGeminiName = disambiguateResponsesSanitizedName(ResponsesToolSearchFunctionName, ResponsesClientToolSearchKey, used)
			used[clientSearchGeminiName] = ResponsesClientToolSearchKey
		}
	}

	forwardMap := make(map[string]string, len(winningList)*2+1)
	reverseMap := make(map[string]ResponsesToolIdentity, len(winningList)*2+1)
	if clientSearchGeminiName != "" {
		forwardMap[ResponsesClientToolSearchKey] = clientSearchGeminiName
		forwardMap[clientSearchGeminiName] = clientSearchGeminiName
		if !userOwnsExactToolSearch {
			forwardMap[ResponsesToolSearchFunctionName] = clientSearchGeminiName
		}
		reverseMap[clientSearchGeminiName] = ResponsesToolIdentity{
			Name:       ResponsesToolSearchFunctionName,
			ToolSearch: true,
		}
	}
	var declarations [][]byte

	for _, desc := range winningList {
		geminiName := desc.Name
		if desc.ClientSearch {
			geminiName = clientSearchGeminiName
		} else if mapped, ok := sanitizedMap[desc.Name]; ok && mapped != "" {
			geminiName = mapped
		} else {
			geminiName = SanitizeFunctionName(desc.Name)
		}

		if !desc.ClientSearch {
			forwardMap[desc.Name] = geminiName
		}
		forwardMap[geminiName] = geminiName

		identity := ResponsesToolIdentity{
			Name:       desc.LocalName,
			Namespace:  desc.Namespace,
			Custom:     desc.ToolType == "custom",
			ApplyPatch: applypatch.IsCustomTool(desc.Tool),
			ToolSearch: desc.ClientSearch,
		}
		reverseMap[geminiName] = identity
		if !desc.ClientSearch && desc.Name != geminiName {
			reverseMap[desc.Name] = identity
		}
		if !withDeclarations {
			continue
		}

		funcDeclDoc := ojson.NewDoc([]byte(`{"name":"","description":"","parametersJsonSchema":{}}`))
		_ = funcDeclDoc.Set("name", geminiName)
		if descStr := responsesToolDescription(desc.Tool); descStr != "" {
			_ = funcDeclDoc.Set("description", descStr)
		}

		if applypatch.IsCustomTool(desc.Tool) {
			_ = funcDeclDoc.Set("description", applypatch.Description(desc.Tool))
			_ = funcDeclDoc.SetRawBytes("parametersJsonSchema", applypatch.Parameters())
		} else if desc.ToolType == "custom" {
			_ = funcDeclDoc.SetRaw("parametersJsonSchema", `{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
		} else if desc.ClientSearch {
			_ = funcDeclDoc.SetRawBytes("parametersJsonSchema", buildGeminiToolSearchParametersSchema(desc.Tool))
		} else {
			params := responsesToolParameters(desc.Tool)
			if params.Exists() {
				_ = funcDeclDoc.SetRaw("parametersJsonSchema", CleanJSONSchemaForGeminiJSONSchema(params.Raw))
			}
		}
		declarations = append(declarations, funcDeclDoc.Bytes())
	}



	for _, desc := range winningList {
		if !desc.ClientSearch && desc.LocalName != "" && desc.LocalName != desc.Name {
			if _, exists := forwardMap[desc.LocalName]; !exists {
				forwardMap[desc.LocalName] = forwardMap[desc.Name]
			}
		}
	}

	return declarations, forwardMap, reverseMap
}

func buildGeminiToolSearchParametersSchema(tool ojson.Result) []byte {
	if params := responsesToolParameters(tool); params.Exists() && params.IsObject() {
		cleaned := CleanJSONSchemaForGeminiJSONSchema(params.Raw)
		if ojson.Valid(cleaned) && ojson.Parse(cleaned).IsObject() {
			return []byte(cleaned)
		}
	}
	return []byte(responsesToolSearchDefaultParameters)
}


func ResponsesToolReverseIdentityMap(rawJSON []byte) map[string]ResponsesToolIdentity {
	if len(rawJSON) == 0 || !ojson.ValidBytes(rawJSON) {
		return nil
	}
	root := ojson.ParseBytes(rawJSON)
	if req := root.Get("request"); req.Exists() && (req.Get("model").Exists() || req.Get("input").Exists() || req.Get("tools").Exists()) {
		root = req
	}
	_, _, reverseMap := buildGeminiFunctionDeclarations(root, false)
	return reverseMap
}


func MapResponsesToolName(forwardMap map[string]string, name string) string {
	if mapped, ok := forwardMap[name]; ok && mapped != "" {
		return mapped
	}
	return SanitizeFunctionName(name)
}



func MapResponsesClientToolSearchName(forwardMap map[string]string) string {
	if mapped, ok := forwardMap[ResponsesClientToolSearchKey]; ok && mapped != "" {
		return mapped
	}
	return ResponsesToolSearchFunctionName
}


func ConvertResponsesToolChoiceToGemini(toolChoice ojson.Result, forwardMap map[string]string) ([]byte, bool) {
	if !toolChoice.Exists() {
		return nil, false
	}
	mode := ""
	var allowedNames []string
	if toolChoice.Type == ojson.String {
		switch strings.ToLower(strings.TrimSpace(toolChoice.String())) {
		case "none":
			mode = "NONE"
		case "auto":
			mode = "AUTO"
		case "required", "any":
			mode = "ANY"
		}
	} else if toolChoice.IsObject() {
		toolType := strings.ToLower(strings.TrimSpace(toolChoice.Get("type").String()))
		switch toolType {
		case "none":
			mode = "NONE"
		case "auto":
			mode = "AUTO"
		case "required", "any":
			mode = "ANY"
		case ResponsesToolSearchType:
			mode = "ANY"
			allowedNames = append(allowedNames, MapResponsesClientToolSearchName(forwardMap))
		case "function", "custom", "tool", "":
			mode = "ANY"
			name := strings.TrimSpace(toolChoice.Get("name").String())
			if name == "" {
				name = strings.TrimSpace(toolChoice.Get("function.name").String())
			}
			if name == "" {
				name = strings.TrimSpace(toolChoice.Get("custom.name").String())
			}
			namespace := strings.TrimSpace(toolChoice.Get("namespace").String())
			if namespace == "" {
				namespace = strings.TrimSpace(toolChoice.Get("function.namespace").String())
			}
			if namespace == "" {
				namespace = strings.TrimSpace(toolChoice.Get("custom.namespace").String())
			}
			if namespace != "" {
				name = QualifyResponsesNamespaceToolName(namespace, name)
			}
			if name != "" {
				geminiName := MapResponsesToolName(forwardMap, name)
				allowedNames = append(allowedNames, geminiName)
			}
		}
	}
	if mode == "" {
		return nil, false
	}
	cfgDoc := ojson.NewDoc([]byte(`{"mode":""}`))
	_ = cfgDoc.Set("mode", mode)
	if len(allowedNames) > 0 {
		_ = cfgDoc.Set("allowedFunctionNames", allowedNames)
	}
	return cfgDoc.Bytes(), true
}


func UnwrapResponsesCustomToolInput(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || arguments == "{}" {
		return ""
	}
	if ojson.Valid(arguments) {
		parsed := ojson.Parse(arguments)
		if v := parsed.Get("input"); v.Exists() {
			if v.Type == ojson.String {
				return v.String()
			}
			return v.Raw
		}
		if parsed.Type == ojson.String {
			return parsed.String()
		}
	}
	return arguments
}
