package helps

import (
	"fmt"
	"strings"

	"github.com/MoeForge/moego/ojson"
	log "github.com/sirupsen/logrus"
)













const (
	claudePropertyKeyMaxLength      = 64
	claudeToolSchemaMaxInspectDepth = 256
	claudeToolSchemaFallbackPrefix  = "The input object must conform to the following JSON Schema. Use its property names exactly as written, including any characters such as brackets:\n"
	claudeToolSchemaFallbackSchema  = `{"type":"object"}`
)



func claudePropertyKeyAccepted(name string) bool {
	if len(name) == 0 || len(name) > claudePropertyKeyMaxLength {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' {
			continue
		}
		return false
	}
	return true
}









func claudeSchemaHasRejectedPropertyKey(schema ojson.Result, depth int) bool {
	if !schema.IsObject() {
		return false
	}
	if depth > claudeToolSchemaMaxInspectDepth {
		return true
	}
	found := false
	subschema := func(value ojson.Result) bool {
		found = claudeSchemaHasRejectedPropertyKey(value, depth+1)
		return !found
	}
	subschemas := func(value ojson.Result) {
		value.ForEach(func(_, item ojson.Result) bool { return subschema(item) })
	}
	schema.ForEach(func(key, value ojson.Result) bool {
		switch key.String() {
		case "properties":
			if !value.IsObject() {
				return true
			}
			value.ForEach(func(name, item ojson.Result) bool {
				if !claudePropertyKeyAccepted(name.String()) {
					found = true
					return false
				}
				return subschema(item)
			})
		case "$defs", "definitions", "patternProperties", "dependentSchemas", "dependencies":


			if value.IsObject() {
				subschemas(value)
			}
		case "allOf", "anyOf", "oneOf", "prefixItems":
			if value.IsArray() {
				subschemas(value)
			}
		case "items":
			if value.IsArray() {
				subschemas(value)
			} else {
				subschema(value)
			}
		case "additionalProperties", "unevaluatedProperties", "additionalItems", "unevaluatedItems",
			"contains", "propertyNames", "not", "if", "then", "else", "contentSchema":
			subschema(value)
		}
		return !found
	})
	return found
}



func claudeToolNeedsSchemaFallback(tool ojson.Result) bool {
	if IsClaudeServerToolType(tool.Get("type").String()) {
		return false
	}
	schema := tool.Get("input_schema")
	return schema.IsObject() && claudeSchemaHasRejectedPropertyKey(schema, 0)
}





func NormalizeClaudeToolPropertyNamesDoc(doc *ojson.Doc) {
	if doc == nil || doc.Len() == 0 {
		return
	}
	var paths []string
	doc.Get("tools").ForEach(func(index, tool ojson.Result) bool {
		if claudeToolNeedsSchemaFallback(tool) {
			paths = append(paths, fmt.Sprintf("tools.%d", index.Int()))
		}
		return true
	})
	doc.Get("messages").ForEach(func(messageIndex, message ojson.Result) bool {
		message.Get("content").ForEach(func(partIndex, part ojson.Result) bool {
			if part.Get("type").String() != "tool_addition" || part.Get("tool.type").String() != "tool_definition" {
				return true
			}
			if claudeToolNeedsSchemaFallback(part.Get("tool.definition")) {
				paths = append(paths, fmt.Sprintf("messages.%d.content.%d.tool.definition", messageIndex.Int(), partIndex.Int()))
			}
			return true
		})
		return true
	})
	for _, path := range paths {
		applyClaudeToolSchemaFallbackDoc(doc, path)
	}
}

func applyClaudeToolSchemaFallbackDoc(doc *ojson.Doc, path string) {
	tool := doc.Get(path)
	name := strings.Clone(tool.Get("name").String())
	var description strings.Builder
	if existing := tool.Get("description").String(); strings.TrimSpace(existing) != "" {
		description.WriteString(existing)
		description.WriteString("\n\n")
	}
	description.WriteString(claudeToolSchemaFallbackPrefix)
	description.WriteString(tool.Get("input_schema").Raw)
	hasStrict := tool.Get("strict").Exists()

	if errSet := doc.Set(path+".description", description.String()); errSet != nil {
		log.Warnf("claude tool %q: failed to embed input_schema in description: %v", name, errSet)
		return
	}
	if errSet := doc.SetRaw(path+".input_schema", claudeToolSchemaFallbackSchema); errSet != nil {
		log.Warnf("claude tool %q: failed to replace input_schema: %v", name, errSet)
		return
	}
	if hasStrict {
		if errDelete := doc.Delete(path + ".strict"); errDelete != nil {
			log.Warnf("claude tool %q: failed to remove strict: %v", name, errDelete)
		}
	}
	log.Debugf("claude tool %q: input_schema has property names Claude rejects; embedded the schema in the description", name)
}
