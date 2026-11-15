package responses

import (
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	applypatch "github.com/router-for-me/CLIProxyAPI/v8/internal/client/codex/apply-patch"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

type responsesToolDeclaration struct {
	tool      ojson.Result
	chatName  string
	localName string
	namespace string
	custom    bool
	shell     bool
	search    bool
}

func walkResponsesToolDeclarations(root ojson.Result, visit func(responsesToolDeclaration) bool) {
	var declarations []responsesToolDeclaration
	emit := func(tool ojson.Result, namespaceName string) {
		var custom, shell, search bool
		switch strings.TrimSpace(tool.Get("type").String()) {
		case "", "function":
		case "custom":
			custom = true
		case "shell":
			if namespaceName != "" || tool.Get("environment.type").String() != "local" {
				return
			}
			shell = true
		case util.ResponsesToolSearchType:
			if namespaceName != "" || !util.IsResponsesClientToolSearchDeclaration(tool) {
				return
			}
			search = true
		default:
			return
		}
		localName := responsesToolName(tool)
		if shell {
			localName = "__cpa_local_shell"
		} else if search {
			localName = util.ResponsesToolSearchFunctionName
		}
		if localName == "" {
			return
		}
		declarations = append(declarations, responsesToolDeclaration{
			tool:      tool,
			chatName:  qualifyResponsesNamespaceToolName(namespaceName, localName),
			localName: localName,
			namespace: namespaceName,
			custom:    custom,
			shell:     shell,
			search:    search,
		})
	}
	scan := func(tools ojson.Result) {
		if !tools.Exists() || !tools.IsArray() {
			return
		}
		tools.ForEach(func(_, tool ojson.Result) bool {
			if strings.TrimSpace(tool.Get("type").String()) == "namespace" {
				if children := tool.Get("tools"); children.Exists() && children.IsArray() {
					namespaceName := strings.TrimSpace(tool.Get("name").String())
					children.ForEach(func(_, child ojson.Result) bool {
						emit(child, namespaceName)
						return true
					})
				}
				return true
			}
			emit(tool, "")
			return true
		})
	}

	scan(root.Get("tools"))
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		input.ForEach(func(_, item ojson.Result) bool {
			switch item.Get("type").String() {
			case "additional_tools":
				scan(item.Get("tools"))
			case util.ResponsesToolSearchOutputType:

				if util.IsResponsesClientToolSearchItem(item) {
					scan(item.Get("tools"))
				}
			}
			return true
		})
	}

	reserved := make(map[string]bool)
	for _, d := range declarations {
		if !d.shell && !d.search {
			reserved[d.localName] = true
			reserved[d.chatName] = true
			reserved[rawResponsesNamespaceQualifiedName(d.namespace, d.localName)] = true
		}
	}
	shellName := "__cpa_local_shell"
	for suffix := 1; reserved[shellName]; suffix++ {
		shellName = "__cpa_local_shell_" + strconv.Itoa(suffix)
	}
	searchName := util.ResponsesToolSearchFunctionName
	for suffix := 1; reserved[searchName]; suffix++ {
		searchName = util.ResponsesToolSearchFunctionName + "_" + strconv.Itoa(suffix)
	}
	for i := range declarations {
		if declarations[i].shell {
			declarations[i].localName = shellName
			declarations[i].chatName = shellName
		} else if declarations[i].search {
			declarations[i].localName = searchName
			declarations[i].chatName = searchName
		}
	}
	disambiguateResponsesChatToolNames(declarations)

	proceed := true
	for _, declaration := range declarations {
		if !proceed {
			break
		}
		proceed = visit(declaration)
	}
}

func disambiguateResponsesChatToolNames(declarations []responsesToolDeclaration) {
	claimed := make(map[string]string, len(declarations))
	claim := func(candidate, identity string) bool {
		if ownerClaim, taken := claimed[candidate]; !taken {
			claimed[candidate] = identity
			return true
		} else {
			return ownerClaim == identity
		}
	}
	longDeclarations := make([]int, 0)
	identities := make([]string, len(declarations))

	localOwners := make(map[string]string)
	ambiguousLocalNames := make(map[string]struct{})
	for i := range declarations {
		identity := rawResponsesNamespaceQualifiedName(declarations[i].namespace, declarations[i].localName)
		identities[i] = identity
		if len(identity) > responsesChatToolNameLimit {
			longDeclarations = append(longDeclarations, i)
		} else {
			claim(identity, identity)
		}
		local := declarations[i].localName
		if local == "" || local == identity || len(local) > responsesChatToolNameLimit {
			continue
		}
		if ownerLocal, seen := localOwners[local]; !seen {
			localOwners[local] = identity
		} else if ownerLocal != "" && ownerLocal != identity {
			localOwners[local] = ""
		}
	}
	for local, ownerLocal := range localOwners {

		claim(local, ownerLocal)
		if ownerLocal == "" {
			ambiguousLocalNames[local] = struct{}{}
		}
	}
	isAmbiguous := func(name string) bool {
		_, ambiguous := ambiguousLocalNames[name]
		return ambiguous
	}
	for _, i := range longDeclarations {
		identity := identities[i]
		name := declarations[i].chatName
		if !isAmbiguous(name) && claim(name, identity) {
			continue
		}
		for suffix := 1; ; suffix++ {
			candidate := capResponsesChatToolName(name + "_" + strconv.Itoa(suffix))
			if isAmbiguous(candidate) {
				continue
			}
			if claim(candidate, identity) {
				declarations[i].chatName = candidate
				break
			}
		}
	}
}

func mergeResponsesRequestChatTools(root ojson.Result) [][]byte {
	return newResponsesToolIndex(root).chatTools()
}

func convertResponsesCustomToolToOpenAIChat(tool ojson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}
	chatToolDoc := ojson.NewDocString(`{"type":"function","function":{"name":"","description":"","parameters":{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}}}`)
	_ = chatToolDoc.Set("function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		_ = chatToolDoc.Set("function.description", description)
	}
	if applypatch.IsCustomTool(tool) {
		_ = chatToolDoc.Set("function.description", applypatch.Description(tool))
		_ = chatToolDoc.SetRawBytes("function.parameters", applypatch.Parameters())
	}
	return chatToolDoc.Bytes(), true
}

func (idx *responsesToolIndex) isApplyPatch(name string) bool {
	d, ok := idx.byChat[name]
	return ok && d.custom && applypatch.IsCustomTool(d.tool)
}

func convertResponsesFunctionToolToOpenAIChat(tool ojson.Result, overrideName string) ([]byte, bool) {
	name := strings.TrimSpace(overrideName)
	if name == "" {
		name = responsesToolName(tool)
	}
	if name == "" {
		return nil, false
	}

	chatToolDoc := ojson.NewDocString(`{"type":"function","function":{"name":"","description":"","parameters":{}}}`)
	_ = chatToolDoc.Set("function.name", name)
	if description := responsesToolDescription(tool); description != "" {
		_ = chatToolDoc.Set("function.description", description)
	}
	if parameters := responsesToolParameters(tool); parameters.Exists() {
		_ = chatToolDoc.SetRaw("function.parameters", parameters.Raw)
	}
	return chatToolDoc.Bytes(), true
}

func responsesToolName(tool ojson.Result) string {
	if name := strings.TrimSpace(tool.Get("name").String()); name != "" {
		return name
	}
	return strings.TrimSpace(tool.Get("function.name").String())
}

func responsesToolDescription(tool ojson.Result) string {
	if description := tool.Get("description").String(); description != "" {
		return description
	}
	return tool.Get("function.description").String()
}

func responsesToolParameters(tool ojson.Result) ojson.Result {
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

func responsesToolOutputText(output ojson.Result) string {
	if output.Type == ojson.String {
		return output.String()
	}
	if output.IsArray() {
		var b strings.Builder
		output.ForEach(func(_, part ojson.Result) bool {
			if part.Type == ojson.String {
				b.WriteString(part.String())
				return true
			}
			if text := part.Get("text"); text.Exists() {
				b.WriteString(text.String())
			}
			return true
		})
		return b.String()
	}
	if output.Exists() {
		return output.Raw
	}
	return ""
}

func responsesCustomToolNames(requestRawJSON []byte) map[string]struct{} {
	return newResponsesToolIndex(util.ParseGJSONBytesNoCopy(requestRawJSON)).custom
}

func responsesSingleCustomToolName(requestRawJSON []byte) (string, bool) {
	return newResponsesToolIndex(util.ParseGJSONBytesNoCopy(requestRawJSON)).singleCustomName()
}

func unwrapCustomToolInput(arguments string) string {
	if v := ojson.Get(arguments, "input"); v.Exists() {
		if v.Type == ojson.String {
			return v.String()
		}
		return v.Raw
	}
	return arguments
}

const responsesChatToolNameLimit = 64

func qualifyResponsesNamespaceToolName(namespaceName, childName string) string {
	return capResponsesChatToolName(rawResponsesNamespaceQualifiedName(namespaceName, childName))
}

func rawResponsesNamespaceQualifiedName(namespaceName, childName string) string {
	childName = strings.TrimSpace(childName)
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

func capResponsesChatToolName(name string) string {
	if len(name) <= responsesChatToolNameLimit {
		return name
	}
	truncated := name[len(name)-responsesChatToolNameLimit:]
	if trimmed := strings.TrimLeft(truncated, "_-"); trimmed != "" {
		return trimmed
	}
	return truncated
}

func resolveResponsesQualifiedToolIdentity(root ojson.Result, qualifiedName string) (name, namespace string, found bool) {
	d, found := newResponsesToolIndex(root).byChat[qualifiedName]
	return d.localName, d.namespace, found
}

func chatNameForResponsesNamespaceToolCall(requestRawJSON []byte, namespace, localName string) string {
	return newResponsesToolIndex(util.ParseGJSONBytesNoCopy(requestRawJSON)).namespaceName(namespace, localName)
}

func canonicalResponsesToolName(requestRawJSON []byte, name string) string {
	return newResponsesToolIndex(util.ParseGJSONBytesNoCopy(requestRawJSON)).canonicalName(name)
}

func avoidResponsesDeclaredChatAliases(root ojson.Result, candidate string) string {
	return newResponsesToolIndex(root).avoidAlias(candidate)
}

func splitResponsesQualifiedFunctionCallFromRequest(requestRawJSON []byte, qualifiedName string) (name, namespace string) {
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return "", ""
	}

	if resolvedName, resolvedNamespace, ok := resolveResponsesQualifiedToolIdentity(util.ParseGJSONBytesNoCopy(requestRawJSON), qualifiedName); ok {
		return resolvedName, resolvedNamespace
	}
	return qualifiedName, ""
}

func pickRequestJSON(originalRequestRawJSON, requestRawJSON []byte) []byte {
	if len(originalRequestRawJSON) > 0 && ojson.ValidBytes(originalRequestRawJSON) {
		return originalRequestRawJSON
	}
	if len(requestRawJSON) > 0 && ojson.ValidBytes(requestRawJSON) {
		return requestRawJSON
	}
	return nil
}

func applyResponsesFunctionCallNamespaceFields(item []byte, requestRawJSON []byte, qualifiedName string, itemPath string) []byte {
	name, namespace := splitResponsesQualifiedFunctionCallFromRequest(requestRawJSON, qualifiedName)
	return translatorcommon.SetResponsesToolCallIdentity(item, name, namespace, itemPath)
}
