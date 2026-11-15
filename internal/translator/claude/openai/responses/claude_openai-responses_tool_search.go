package responses

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)














const (


	responsesToolSearchType = "tool_search"

	responsesToolSearchIDPrefix = "tsc_"


	responsesDeferredAnchorName = "tool_search_deferred_anchor"
)





var clientToolSearchEnabled atomic.Bool


func SetClientToolSearchEnabled(enabled bool) {
	clientToolSearchEnabled.Store(enabled)
}


func ClientToolSearchEnabled() bool {
	return clientToolSearchEnabled.Load()
}

type clientToolSearchContextKey struct{}




func WithClientToolSearch(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, clientToolSearchContextKey{}, enabled)
}



func ClientToolSearchEnabledFor(ctx context.Context) bool {
	if ctx != nil {
		if enabled, ok := ctx.Value(clientToolSearchContextKey{}).(bool); ok {
			return enabled
		}
	}
	return ClientToolSearchEnabled()
}

const claudeToolSearchDefaultSchema = `{"type":"object","properties":{"query":{"type":"string","description":"Search query for deferred tools."},"limit":{"type":"number","description":"Maximum number of tools to return."}},"required":["query"],"additionalProperties":false}`

const responsesDeferredAnchorJSON = `{"name":"` + responsesDeferredAnchorName + `","description":"Placeholder that is never loaded; tool search returns the real tools.","input_schema":{"type":"object","properties":{}},"defer_loading":true}`





func responsesDeferredAnchorTool(toolItems [][]byte) ([]byte, bool) {
	for _, tool := range toolItems {
		if ojson.GetBytesNoCopy(tool, "name").String() == responsesDeferredAnchorName {
			return nil, false
		}
	}
	return []byte(responsesDeferredAnchorJSON), true
}



func isResponsesClientToolSearch(tool ojson.Result) bool {
	return strings.TrimSpace(tool.Get("type").String()) == responsesToolSearchType &&
		strings.TrimSpace(tool.Get("execution").String()) == "client"
}



func convertResponsesToolSearchToolToClaude(tool ojson.Result, claudeName string) ([]byte, bool) {
	schema := []byte(claudeToolSearchDefaultSchema)
	if parameters := tool.Get("parameters"); parameters.IsObject() {
		schema = util.NormalizeClaudeToolInputSchema([]byte(parameters.Raw))
	}
	needQueryProp := ojson.GetBytesNoCopy(schema, "properties.query.type").String() != "string"
	needQueryReq := !ojson.GetBytesNoCopy(schema, `required.#(=="query")`).Exists()
	if needQueryProp || needQueryReq {
		schemaDoc := ojson.NewDoc(schema)
		if needQueryProp {
			_ = schemaDoc.SetRawBytes("properties.query", []byte(`{"type":"string","description":"Search query for deferred tools."}`))
		}
		if needQueryReq {
			_ = schemaDoc.Set("required.-1", "query")
		}
		schema = schemaDoc.Bytes()
	}

	tDoc := ojson.NewDocString(`{"name":"","description":"","input_schema":{}}`)
	_ = tDoc.Set("name", claudeName)
	_ = tDoc.Set("description", responsesToolDescription(tool))
	_ = tDoc.SetRawBytes("input_schema", schema)
	return tDoc.Bytes(), true
}




func convertResponsesToolSearchCallToClaude(item ojson.Result, claudeName string) ([]byte, string) {
	callID := strings.TrimSpace(item.Get("call_id").String())
	if callID == "" || strings.TrimSpace(item.Get("execution").String()) != "client" {
		return nil, ""
	}
	toolUseDoc := ojson.NewDocString(`{"type":"tool_use","id":"","name":"","input":{}}`)
	_ = toolUseDoc.Set("id", util.SanitizeClaudeToolID(callID))
	_ = toolUseDoc.Set("name", claudeName)
	arguments := item.Get("arguments")
	if arguments.Type == ojson.String {
		arguments = ojson.Parse(arguments.String())
	}
	if arguments.IsObject() {
		_ = toolUseDoc.SetRawBytes("input", []byte(arguments.Raw))
	}
	return toolUseDoc.Bytes(), callID
}



func responsesToolSearchOutputText(item ojson.Result, names claudeToolNames) string {
	var discovered []string
	for _, descriptor := range appendResponsesToolDescriptors(nil, item.Get("tools"), responsesDiscoveredToolPriority, true) {
		discovered = append(discovered, names.claudeName(descriptor.name))
	}
	if len(discovered) == 0 {
		return "No matching tools were found."
	}
	return "Tools now available to call: " + strings.Join(discovered, ", ") + "."
}






func responsesToolSearchOutputContent(item ojson.Result, names claudeToolNames, winners map[string]responsesToolDescriptor, nativeReferences bool) []byte {
	if nativeReferences {
		var references [][]byte
		seen := make(map[string]struct{})
		for _, descriptor := range appendResponsesToolDescriptors(nil, item.Get("tools"), responsesDiscoveredToolPriority, true) {
			winner, ok := winners[descriptor.name]
			if !ok || winner.sourcePriority != responsesDiscoveredToolPriority || (winner.toolType != "function" && winner.toolType != "custom") {
				continue
			}
			claudeName := names.claudeName(descriptor.name)
			if _, duplicate := seen[claudeName]; duplicate {
				continue
			}
			seen[claudeName] = struct{}{}
			reference, _ := ojson.SetBytes([]byte(`{"type":"tool_reference","tool_name":""}`), "tool_name", claudeName)
			references = append(references, reference)
		}
		if len(references) > 0 {
			return common.JoinRawArray(references)
		}
	}
	text, _ := ojson.SetBytes([]byte(`{"text":""}`), "text", responsesToolSearchOutputText(item, names))
	return []byte(ojson.GetBytesNoCopy(text, "text").Raw)
}



func toolSearchCallStatus(status, arguments string) string {
	if status == "completed" && !isJSONObject(arguments) {
		return "incomplete"
	}
	return status
}

func isJSONObject(raw string) bool {
	return ojson.Valid(raw) && ojson.Parse(raw).IsObject()
}



func buildResponsesToolSearchCallItem(callID, status, arguments string) []byte {
	itemDoc := ojson.NewDocString(`{"id":"","type":"tool_search_call","status":"","call_id":"","execution":"client","arguments":{}}`)
	_ = itemDoc.Set("id", responsesToolSearchIDPrefix+callID)
	_ = itemDoc.Set("status", status)
	_ = itemDoc.Set("call_id", callID)
	if isJSONObject(arguments) {
		_ = itemDoc.SetRawBytes("arguments", []byte(arguments))
	}
	return itemDoc.Bytes()
}




func toolSearchCallEvent(event string, sequence, outputIndex int, item []byte) []byte {
	payloadDoc := ojson.NewDocString(`{"type":"","sequence_number":0,"output_index":0,"item":{}}`)
	_ = payloadDoc.Set("type", event)
	_ = payloadDoc.Set("sequence_number", sequence)
	_ = payloadDoc.Set("output_index", outputIndex)
	_ = payloadDoc.SetRawBytes("item", item)
	return emitEvent(event, payloadDoc.Bytes())
}
