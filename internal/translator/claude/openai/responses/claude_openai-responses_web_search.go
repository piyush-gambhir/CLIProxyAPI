package responses

import (
	"context"
	"regexp"
	"strings"

	"github.com/MoeForge/moego/ojson"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)










const (
	claudeWebSearchToolName = "web_search"




	responsesWebSearchIDPrefix = "ws_"



	claudeServerToolIDPrefix = "srvtoolu_"
)




var claudeServerToolIDSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func responsesWebSearchCallID(claudeToolUseID string) string {
	return responsesWebSearchIDPrefix + claudeToolUseID
}







func claudeWebSearchToolUseID(responsesItemID string) string {
	body := strings.TrimPrefix(strings.TrimSpace(responsesItemID), responsesWebSearchIDPrefix)
	body = claudeServerToolIDSanitizer.ReplaceAllString(strings.TrimPrefix(body, claudeServerToolIDPrefix), "_")
	if body == "" {
		return ""
	}
	return claudeServerToolIDPrefix + body
}



func claudeWebSearchQuery(input string) string {
	if input == "" {
		return ""
	}
	return strings.TrimSpace(ojson.Get(input, "query").String())
}








func claudeWebSearchResultsToResponses(content ojson.Result) []byte {
	if content.IsObject() {
		return []byte(content.Raw)
	}
	if !content.IsArray() {
		return nil
	}
	var results [][]byte
	content.ForEach(func(_, entry ojson.Result) bool {
		if entry.Get("type").String() == "web_search_tool_result_error" || strings.TrimSpace(entry.Get("url").String()) != "" {
			results = append(results, []byte(entry.Raw))
		}
		return true
	})
	if len(results) == 0 {
		return []byte(`[]`)
	}
	return translatorcommon.JoinRawArray(results)
}




func buildResponsesWebSearchCallItem(claudeToolUseID, query string, results []byte) []byte {
	return buildResponsesWebSearchCallItemWithStatus(claudeToolUseID, query, "completed", results)
}

func buildResponsesWebSearchCallItemWithStatus(claudeToolUseID, query, status string, results []byte) []byte {
	if status == "" {
		status = "completed"
	}
	itemDoc := ojson.NewDocString(`{"id":"","type":"web_search_call","status":"completed","action":{"type":"search","query":""}}`)
	_ = itemDoc.Set("id", responsesWebSearchCallID(claudeToolUseID))
	_ = itemDoc.Set("status", status)
	_ = itemDoc.Set("action.query", query)
	if len(results) > 0 {
		_ = itemDoc.SetRawBytes("results", results)
	}
	return itemDoc.Bytes()
}

type webSearchResultLookupContextKey struct{}




func WithWebSearchResultLookup(ctx context.Context, lookup func(toolUseID string) ([]byte, bool)) context.Context {
	if lookup == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, webSearchResultLookupContextKey{}, lookup)
}

func lookupWebSearchResultFor(ctx context.Context, toolUseID string) []byte {
	if ctx == nil || toolUseID == "" {
		return nil
	}
	lookup, ok := ctx.Value(webSearchResultLookupContextKey{}).(func(string) ([]byte, bool))
	if !ok || lookup == nil {
		return nil
	}
	content, found := lookup(toolUseID)
	if !found || len(content) == 0 {
		return nil
	}
	return content
}




func convertResponsesWebSearchCallToClaudeBlocks(item ojson.Result) [][]byte {
	return convertResponsesWebSearchCallToClaudeBlocksWithQuery(item, "", true)
}




func convertResponsesWebSearchCallToClaudeBlocksWithQuery(item ojson.Result, fallbackQuery string, withResult bool) [][]byte {
	return convertResponsesWebSearchCallToClaudeBlocksWithContext(context.Background(), item, fallbackQuery, withResult)
}

func convertResponsesWebSearchCallToClaudeBlocksWithContext(ctx context.Context, item ojson.Result, fallbackQuery string, withResult bool) [][]byte {
	toolUseID := claudeWebSearchToolUseID(strings.TrimSpace(item.Get("id").String()))
	if toolUseID == "" {
		return nil
	}

	useDoc := ojson.NewDocString(`{"type":"server_tool_use","id":"","name":"","input":{}}`)
	_ = useDoc.Set("id", toolUseID)
	_ = useDoc.Set("name", claudeWebSearchToolName)
	query := responsesWebSearchCallQuery(item)
	if query == "" {
		query = fallbackQuery
	}
	if query != "" {
		_ = useDoc.Set("input.query", query)
	}
	if !withResult {
		return [][]byte{useDoc.Bytes()}
	}

	resultDoc := ojson.NewDocString(`{"type":"web_search_tool_result","tool_use_id":"","content":[]}`)
	_ = resultDoc.Set("tool_use_id", toolUseID)
	content := responsesWebSearchResultsToClaude(item.Get("results"))
	if len(content) == 0 {
		content = lookupWebSearchResultFor(ctx, toolUseID)
	}
	if len(content) > 0 {
		_ = resultDoc.SetRawBytes("content", content)
	}
	return [][]byte{useDoc.Bytes(), resultDoc.Bytes()}
}












type responsesWebSearchHistory struct {

	boundaryAfter []bool

	skip map[int]bool

	pausedQuery map[int]string
}

func responsesWebSearchItemIsPaused(item ojson.Result) bool {
	return item.Get("type").String() == "web_search_call" &&
		strings.EqualFold(strings.TrimSpace(item.Get("status").String()), "in_progress") &&
		!item.Get("results").Exists()
}



func responsesItemIsTurnBoundary(item ojson.Result) bool {
	typ := item.Get("type").String()
	role := item.Get("role").String()
	if typ == "" && role != "" {
		typ = "message"
	}
	return typ == "message" && role != "assistant"
}

func scanResponsesWebSearchHistory(items []ojson.Result) responsesWebSearchHistory {
	history := responsesWebSearchHistory{
		boundaryAfter: make([]bool, len(items)),
		skip:          map[int]bool{},
		pausedQuery:   map[int]string{},
	}
	boundary := false
	for index := len(items) - 1; index >= 0; index-- {
		history.boundaryAfter[index] = boundary
		if responsesItemIsTurnBoundary(items[index]) {
			boundary = true
		}
	}
	for index, item := range items {
		if !responsesWebSearchItemIsPaused(item) {
			continue
		}
		id := strings.TrimSpace(item.Get("id").String())
		if id == "" {
			continue
		}
		for next := index + 1; next < len(items); next++ {
			if responsesItemIsTurnBoundary(items[next]) {
				break
			}
			if items[next].Get("type").String() == "web_search_call" && strings.TrimSpace(items[next].Get("id").String()) == id {
				history.skip[index] = true
				query := responsesWebSearchCallQuery(item)
				if previous, ok := history.pausedQuery[index]; ok && query == "" {
					query = previous
				}
				history.pausedQuery[next] = query
				break
			}
		}
	}
	return history
}

func (h responsesWebSearchHistory) claudeBlocks(index int, item ojson.Result) [][]byte {
	return h.claudeBlocksWithContext(context.Background(), index, item)
}

func (h responsesWebSearchHistory) claudeBlocksWithContext(ctx context.Context, index int, item ojson.Result) [][]byte {
	if h.skip[index] {
		return nil
	}
	query := h.pausedQuery[index]
	tail := index >= len(h.boundaryAfter) || !h.boundaryAfter[index]
	if responsesWebSearchItemIsPaused(item) && tail {
		return convertResponsesWebSearchCallToClaudeBlocksWithContext(ctx, item, query, false)
	}
	return convertResponsesWebSearchCallToClaudeBlocksWithContext(ctx, item, query, true)
}




func responsesWebSearchCallQuery(item ojson.Result) string {
	if query := strings.TrimSpace(item.Get("action.query").String()); query != "" {
		return query
	}
	if query := strings.TrimSpace(item.Get("action.queries.0").String()); query != "" {
		return query
	}
	return strings.TrimSpace(item.Get("action.url").String())
}

func responsesWebSearchResultsToClaude(results ojson.Result) []byte {
	if results.IsObject() {
		return []byte(results.Raw)
	}
	if !results.IsArray() {
		return nil
	}
	var blocks [][]byte
	results.ForEach(func(_, entry ojson.Result) bool {
		if entry.Get("type").String() == "web_search_tool_result_error" {
			blocks = append(blocks, []byte(entry.Raw))
			return true
		}




		if strings.TrimSpace(entry.Get("encrypted_content").String()) == "" {
			return true
		}
		block := []byte(entry.Raw)
		block, _ = ojson.SetBytes(block, "type", "web_search_result")
		blocks = append(blocks, block)
		return true
	})
	if len(blocks) == 0 {
		return nil
	}
	return translatorcommon.JoinRawArray(blocks)
}





func attachClaudeCitations(textBlock []byte, annotations ojson.Result) []byte {
	if !annotations.IsArray() {
		return textBlock
	}
	var citations [][]byte
	annotations.ForEach(func(_, annotation ojson.Result) bool {
		if strings.TrimSpace(annotation.Get("encrypted_index").String()) != "" {
			citations = append(citations, []byte(annotation.Raw))
		}
		return true
	})
	if len(citations) == 0 {
		return textBlock
	}
	updated, err := ojson.SetRawBytes(textBlock, "citations", translatorcommon.JoinRawArray(citations))
	if err != nil {
		return textBlock
	}
	return updated
}

func claudeBlocksHaveEmptyWebSearchResult(blocks [][]byte) bool {
	for _, raw := range blocks {
		block := util.ParseGJSONBytesNoCopy(raw)
		if block.Get("type").String() != "web_search_tool_result" {
			continue
		}
		content := block.Get("content")
		if !content.Exists() || content.Type == ojson.Null || (content.IsArray() && len(content.Array()) == 0) {
			return true
		}
	}
	return false
}





func stripClaudeWebSearchCitationsFromParts(parts [][]byte) ([][]byte, int) {
	droppedTotal := 0
	for i, raw := range parts {
		part := util.ParseGJSONBytesNoCopy(raw)
		if part.Get("type").String() != "text" {
			continue
		}
		updated, dropped := stripClaudeWebSearchCitationsFromTextBlock(raw, part.Get("citations"))
		if dropped > 0 {
			parts[i] = updated
			droppedTotal += dropped
		}
	}
	return parts, droppedTotal
}

func stripClaudeWebSearchCitationsFromTextBlock(textBlock []byte, citations ojson.Result) ([]byte, int) {
	if !citations.Exists() || !citations.IsArray() {
		return textBlock, 0
	}
	var kept [][]byte
	dropped := 0
	citations.ForEach(func(_, citation ojson.Result) bool {
		if citation.Get("type").String() == "web_search_result_location" {
			dropped++
			return true
		}
		kept = append(kept, []byte(citation.Raw))
		return true
	})
	if dropped == 0 {
		return textBlock, 0
	}
	if len(kept) == 0 {
		updated, errDelete := ojson.DeleteBytes(textBlock, "citations")
		if errDelete != nil {
			return textBlock, 0
		}
		return updated, dropped
	}
	updated, errSet := ojson.SetRawBytes(textBlock, "citations", translatorcommon.JoinRawArray(kept))
	if errSet != nil {
		return textBlock, 0
	}
	return updated, dropped
}
