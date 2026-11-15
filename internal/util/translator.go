package util

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/ojson"
	log "github.com/sirupsen/logrus"
)

func Walk(value ojson.Result, path, field string, paths *[]string) {
	walk(value, path, field, paths, false)
}

func WalkSchema(value ojson.Result, path, field string, paths *[]string) {
	walk(value, path, field, paths, true)
}

func walk(value ojson.Result, path, field string, paths *[]string, schemaAware bool) {
	switch value.Type {
	case ojson.JSON:

		value.ForEach(func(key, val ojson.Result) bool {
			var childPath string

			keyStr := key.String()
			safeKey := escapeGJSONPathKey(keyStr)

			if path == "" {
				childPath = safeKey
			} else {
				childPath = path + "." + safeKey
			}
			if keyStr == field {
				*paths = append(*paths, childPath)
			}
			if schemaAware && isLiteralValueKeyword(keyStr) && !isPropertyDefinition(path) {
				return true
			}
			walk(val, childPath, field, paths, schemaAware)
			return true
		})
	case ojson.String, ojson.Number, ojson.True, ojson.False, ojson.Null:

	}
}

func RenameKey(jsonStr, oldKeyPath, newKeyPath string) (string, error) {
	value := ojson.Get(jsonStr, oldKeyPath)

	if !value.Exists() {
		return "", fmt.Errorf("old key '%s' does not exist", oldKeyPath)
	}

	doc := ojson.NewDocString(jsonStr)
	if errSet := doc.SetRaw(newKeyPath, value.Raw); errSet != nil {
		return "", fmt.Errorf("failed to set new key '%s': %w", newKeyPath, errSet)
	}

	if errDelete := doc.Delete(oldKeyPath); errDelete != nil {
		return "", fmt.Errorf("failed to delete old key '%s': %w", oldKeyPath, errDelete)
	}

	return doc.String(), nil
}

func FixJSON(input string) string {
	var out bytes.Buffer

	inDouble := false
	inSingle := false
	escaped := false

	writeConverted := func(r rune) {
		if r == '"' {
			out.WriteByte('\\')
			out.WriteByte('"')
			return
		}
		out.WriteRune(r)
	}

	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if inDouble {
			out.WriteRune(r)
			if escaped {

				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inDouble = false
			}
			continue
		}

		if inSingle {
			if escaped {

				escaped = false
				switch r {
				case 'n', 'r', 't', 'b', 'f', '/', '"':

					out.WriteByte('\\')
					out.WriteRune(r)
				case '\\':
					out.WriteByte('\\')
					out.WriteByte('\\')
				case '\'':

					out.WriteRune('\'')
				case 'u':

					out.WriteByte('\\')
					out.WriteByte('u')

					for k := 0; k < 4 && i+1 < len(runes); k++ {
						peek := runes[i+1]

						if (peek >= '0' && peek <= '9') || (peek >= 'a' && peek <= 'f') || (peek >= 'A' && peek <= 'F') {
							out.WriteRune(peek)
							i++
						} else {
							break
						}
					}
				default:

					out.WriteByte('\\')
					out.WriteRune(r)
				}
				continue
			}

			if r == '\\' {
				escaped = true
				continue
			}
			if r == '\'' {
				out.WriteByte('"')
				inSingle = false
				continue
			}

			writeConverted(r)
			continue
		}

		if r == '"' {
			inDouble = true
			out.WriteRune(r)
			continue
		}
		if r == '\'' {
			inSingle = true
			out.WriteByte('"')
			continue
		}
		out.WriteRune(r)
	}

	if inSingle {
		out.WriteByte('"')
	}

	return out.String()
}

func CanonicalToolName(name string) string {
	canonical := strings.TrimSpace(name)
	canonical = strings.TrimLeft(canonical, "_")
	return strings.ToLower(canonical)
}

func ToolNameMapFromClaudeRequest(rawJSON []byte) map[string]string {
	return toolNameMapFromClaudeRequest(rawJSON, false)
}

func UnambiguousToolNameMapFromClaudeRequest(rawJSON []byte) map[string]string {
	return toolNameMapFromClaudeRequest(rawJSON, true)
}

func claudePayloadMayContainToolAddition(rawJSON []byte) bool {
	return bytes.Contains(rawJSON, []byte("tool_addition")) ||
		bytes.Contains(rawJSON, []byte(`\u`)) ||
		bytes.Contains(rawJSON, []byte(`\U`))
}

func forEachClaudeInlineToolAdditionDefinition(rawJSON []byte, fn func(def ojson.Result)) {
	if !claudePayloadMayContainToolAddition(rawJSON) {
		return
	}
	messages := ojson.GetBytes(rawJSON, "messages")
	if !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message ojson.Result) bool {
		if message.Get("role").String() != "system" {
			return true
		}
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, part ojson.Result) bool {
			if part.Get("type").String() != "tool_addition" {
				return true
			}
			toolSpec := part.Get("tool")
			if toolSpec.Get("type").String() == "tool_definition" || toolSpec.Get("definition").IsObject() {
				if def := toolSpec.Get("definition"); def.IsObject() {
					fn(def)
				}
			}
			return true
		})
		return true
	})
}

func toolNameMapFromClaudeRequest(rawJSON []byte, rejectAmbiguous bool) map[string]string {
	if len(rawJSON) == 0 || !ojson.ValidBytes(rawJSON) {
		return nil
	}

	tools := ojson.GetBytes(rawJSON, "tools")
	hasTools := tools.Exists() && tools.IsArray()
	hasToolAddition := claudePayloadMayContainToolAddition(rawJSON)
	if !hasTools && !hasToolAddition {
		return nil
	}

	var out map[string]string
	recordTool := func(tool ojson.Result) {
		name := strings.Clone(strings.TrimSpace(tool.Get("name").String()))
		if name == "" {
			name = strings.Clone(strings.TrimSpace(tool.Get("function.name").String()))
		}
		if name == "" {
			return
		}
		key := strings.Clone(CanonicalToolName(name))
		if key == "" {
			return
		}
		if out == nil {
			out = make(map[string]string, 4)
		}
		if previous, exists := out[key]; !exists {
			out[key] = name
		} else if rejectAmbiguous && previous != name {

			out[key] = ""
		}
	}

	if hasTools {
		tools.ForEach(func(_, tool ojson.Result) bool {
			recordTool(tool)
			return true
		})
	}
	if hasToolAddition {
		forEachClaudeInlineToolAdditionDefinition(rawJSON, recordTool)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func MapToolName(toolNameMap map[string]string, name string) string {
	if name == "" || toolNameMap == nil {
		return name
	}
	if mapped, ok := toolNameMap[CanonicalToolName(name)]; ok && mapped != "" {
		return mapped
	}
	return name
}

func MapClaudeToolName(toolNameMap map[string]string, originalRequestRawJSON []byte, name string) string {
	if name == "" {
		return name
	}
	if len(originalRequestRawJSON) > 0 && ojson.ValidBytes(originalRequestRawJSON) {
		tools := ojson.GetBytes(originalRequestRawJSON, "tools")
		hasTools := tools.IsArray()
		hasToolAddition := claudePayloadMayContainToolAddition(originalRequestRawJSON)
		if hasTools || hasToolAddition {
			canonical := CanonicalToolName(name)
			canonicalNames := map[string]bool{}
			exactMatch := false
			inspectTool := func(tool ojson.Result) {
				declared := strings.TrimSpace(tool.Get("name").String())
				if declared == "" {
					declared = strings.TrimSpace(tool.Get("function.name").String())
				}
				if declared == name {
					exactMatch = true
					return
				}
				if declared != "" && CanonicalToolName(declared) == canonical {
					canonicalNames[declared] = true
				}
			}
			if hasTools {
				for _, tool := range tools.Array() {
					inspectTool(tool)
					if exactMatch {
						return name
					}
				}
			}
			if hasToolAddition {
				forEachClaudeInlineToolAdditionDefinition(originalRequestRawJSON, inspectTool)
				if exactMatch {
					return name
				}
			}
			if len(canonicalNames) > 1 {
				return name
			}
		}
	}
	return MapToolName(toolNameMap, name)
}

func SanitizedFunctionNameMap(rawJSON []byte) map[string]string {
	names := functionNamesFromRequest(rawJSON)
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
			mapped = disambiguateSanitizedFunctionName(base, name, used)
		}
		out[name] = mapped
		used[mapped] = name
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func MapSanitizedFunctionName(nameMap map[string]string, name string) string {
	if mapped := nameMap[name]; mapped != "" {
		return mapped
	}
	return SanitizeFunctionName(name)
}

func DisambiguatedToolNameMap(rawJSON []byte) map[string]string {
	forward := SanitizedFunctionNameMap(rawJSON)
	if len(forward) == 0 {
		return nil
	}

	out := make(map[string]string, len(forward))
	for original, sanitized := range forward {
		if sanitized != original {
			out[sanitized] = original
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func SanitizedToolNameMap(rawJSON []byte) map[string]string {
	if len(rawJSON) == 0 || !ojson.ValidBytes(rawJSON) {
		return nil
	}
	tools := ojson.GetBytes(rawJSON, "tools")
	if !tools.IsArray() {
		return nil
	}

	out := make(map[string]string)
	tools.ForEach(func(_, tool ojson.Result) bool {
		name := strings.Clone(strings.TrimSpace(tool.Get("name").String()))
		if name == "" {
			return true
		}
		sanitized := strings.Clone(SanitizeFunctionName(name))
		if sanitized == name {
			return true
		}
		if existing, exists := out[sanitized]; !exists {
			out[sanitized] = name
		} else {
			log.Warnf("sanitized tool name collision: %q and %q both map to %q, keeping first", existing, name, sanitized)
		}
		return true
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func functionNamesFromRequest(rawJSON []byte) []string {
	if len(rawJSON) == 0 || !ojson.ValidBytes(rawJSON) {
		return nil
	}
	tools := ojson.GetBytes(rawJSON, "tools")
	if !tools.IsArray() {
		return nil
	}

	names := make([]string, 0, len(tools.Array()))
	var collectTool func(ojson.Result)
	collectDeclarations := func(declarations ojson.Result) {
		if !declarations.IsArray() {
			return
		}
		declarations.ForEach(func(_, declaration ojson.Result) bool {
			if name := declaration.Get("name").String(); name != "" {
				names = append(names, strings.Clone(name))
			}
			return true
		})
	}
	collectTool = func(tool ojson.Result) {
		if nestedTools := tool.Get("tools"); nestedTools.IsArray() {
			nestedTools.ForEach(func(_, nestedTool ojson.Result) bool {
				collectTool(nestedTool)
				return true
			})
			return
		}
		hasDeclarations := false
		if declarations := tool.Get("functionDeclarations"); declarations.IsArray() {
			collectDeclarations(declarations)
			hasDeclarations = true
		}
		if declarations := tool.Get("function_declarations"); declarations.IsArray() {
			collectDeclarations(declarations)
			hasDeclarations = true
		}
		if hasDeclarations {
			return
		}
		if name := tool.Get("function.name").String(); name != "" {
			names = append(names, strings.Clone(name))
			return
		}
		if name := tool.Get("name").String(); name != "" {
			names = append(names, strings.Clone(name))
		}
	}
	tools.ForEach(func(_, tool ojson.Result) bool {
		collectTool(tool)
		return true
	})
	return names
}

func disambiguateSanitizedFunctionName(base, original string, used map[string]string) string {
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

func DeduplicateFunctionDeclarations(raw []byte) []byte {
	result := ParseGJSONBytesNoCopy(raw)
	if !result.IsArray() {
		return raw
	}

	seen := make(map[string]struct{}, len(result.Array()))
	parts := make([]string, 0, len(result.Array()))
	for _, declaration := range result.Array() {
		name := declaration.Get("name").String()
		if name != "" {
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
		}
		parts = append(parts, declaration.Raw)
	}
	return []byte("[" + strings.Join(parts, ",") + "]")
}

func RestoreSanitizedToolName(toolNameMap map[string]string, sanitizedName string) string {
	if sanitizedName == "" || toolNameMap == nil {
		return sanitizedName
	}
	if original, ok := toolNameMap[sanitizedName]; ok {
		return original
	}
	return sanitizedName
}

type claudeObjectSchemaInfo struct {
	properties           map[string][]ojson.Result
	required             map[string]bool
	hasProperties        bool
	allowsDynamic        bool
	forbidsDynamic       bool
	hasPatternProperties bool
	complex              bool
	unsafeComplex        bool
	declaresObject       bool
	branchInfos          []claudeObjectSchemaInfo
}

func claudeSchemaAllowsDynamic(schema ojson.Result) bool {

	if schema.Get("patternProperties").IsObject() && len(schema.Get("patternProperties").Map()) > 0 {
		return true
	}
	additionalProperties := schema.Get("additionalProperties")
	if additionalProperties.Exists() && additionalProperties.Type != ojson.False {
		return true
	}
	return false
}

func claudeSchemaHasPatternProperties(schema ojson.Result) bool {
	return schema.Get("patternProperties").IsObject() && len(schema.Get("patternProperties").Map()) > 0
}

func claudeSchemaForbidsDynamic(schema ojson.Result) bool {
	additionalProperties := schema.Get("additionalProperties")
	return additionalProperties.Exists() && additionalProperties.Type == ojson.False
}

func claudeResolveLocalRef(root ojson.Result, ref string) (ojson.Result, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return ojson.Result{}, false
	}
	current := root
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		next, ok := current.Map()[segment]
		if !ok {
			if current.IsArray() {
				index, errAtoi := strconv.Atoi(segment)
				if errAtoi != nil || index < 0 || index >= len(current.Array()) {
					return ojson.Result{}, false
				}
				next = current.Array()[index]
			} else {
				return ojson.Result{}, false
			}
		}
		current = next
	}
	return current, true
}

func analyzeClaudeObjectSchema(schema ojson.Result, root ojson.Result) claudeObjectSchemaInfo {
	return analyzeClaudeObjectSchemaDepth(schema, root, 0, map[string]bool{})
}

func claudeSchemaDeclaresObject(schema ojson.Result) bool {
	typeValue := schema.Get("type")
	if typeValue.String() == "object" {
		return true
	}
	if typeValue.IsArray() {
		for _, item := range typeValue.Array() {
			if item.String() == "object" {
				return true
			}
		}
	}
	return false
}

func claudeUnionBranches(schema ojson.Result) ([]ojson.Result, bool) {
	var branches []ojson.Result
	seen := map[string]bool{}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		union := schema.Get(keyword)
		if !union.Exists() {
			continue
		}
		if !union.IsArray() {
			return nil, false
		}
		for _, branch := range union.Array() {
			branchType := branch.Get("type")
			if branchType.String() == "null" {
				continue
			}
			if branchType.IsArray() && len(branchType.Array()) == 1 && branchType.Array()[0].String() == "null" {
				continue
			}
			if seen[branch.Raw] {
				continue
			}
			seen[branch.Raw] = true
			branches = append(branches, branch)
		}
	}
	return branches, true
}

var claudeAnalyzedSchemaKeywords = map[string]bool{
	"type": true, "description": true, "title": true, "properties": true,
	"required": true, "items": true, "additionalProperties": true,
	"patternProperties": true, "anyOf": true, "oneOf": true, "allOf": true,
	"$ref": true, "$defs": true, "definitions": true, "nullable": true,
	"minLength": true, "maxLength": true, "minimum": true, "maximum": true,
	"exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"pattern": true, "format": true, "default": true, "examples": true,
	"minItems": true, "maxItems": true, "maxProperties": true,
	"$schema": true, "$id": true, "id": true,
	"$comment": true, "deprecated": true, "readOnly": true, "writeOnly": true,
	"example": true, "contentEncoding": true, "contentMediaType": true,
}

func claudeSchemaHasUnanalyzedKeyword(schema ojson.Result) bool {
	for key := range schema.Map() {
		if strings.HasPrefix(key, "x-") {
			continue
		}
		if !claudeAnalyzedSchemaKeywords[key] {
			return true
		}
	}
	return false
}

func analyzeClaudeObjectSchemaDepth(schema ojson.Result, root ojson.Result, depth int, visitedRefs map[string]bool) claudeObjectSchemaInfo {
	info := claudeObjectSchemaInfo{properties: map[string][]ojson.Result{}, required: map[string]bool{}}
	if depth > 16 || !schema.Exists() || !schema.IsObject() {
		info.complex = true
		return info
	}
	if claudeSchemaDeclaresObject(schema) {
		info.declaresObject = true
	}
	if claudeSchemaHasUnanalyzedKeyword(schema) {

		info.complex = true
		info.unsafeComplex = true
		return info
	}
	if schema.Get("anyOf").Exists() || schema.Get("oneOf").Exists() {
		branches, okBranches := claudeUnionBranches(schema)
		if !okBranches || len(branches) == 0 {
			info.complex = true
			return info
		}
		if len(branches) > 1 {

			for _, branch := range branches {
				branchInfo := analyzeClaudeObjectSchemaDepth(branch, root, depth+1, visitedRefs)
				info.branchInfos = append(info.branchInfos, branchInfo)
				mergeClaudeObjectSchemaInfo(&info, branchInfo)
			}
			if claudeSchemaAllowsDynamic(schema) {
				info.allowsDynamic = true
			}
			if claudeSchemaForbidsDynamic(schema) {
				info.forbidsDynamic = true
			}
			if claudeSchemaHasPatternProperties(schema) {
				info.hasPatternProperties = true
			}
			for _, requiredName := range schema.Get("required").Array() {
				info.required[requiredName.String()] = true
			}
			props := schema.Get("properties")
			if props.IsObject() {
				info.hasProperties = true
				info.declaresObject = true
				for key, value := range props.Map() {
					info.properties[key] = appendUniqueSchema(info.properties[key], value)
				}
			}

			if ref := schema.Get("$ref"); ref.Exists() {
				refValue := ref.String()
				if !visitedRefs[refValue] {
					if target, ok := claudeResolveLocalRef(root, refValue); ok {
						visitedRefs[refValue] = true
						resolved := analyzeClaudeObjectSchemaDepth(target, root, depth+1, visitedRefs)
						delete(visitedRefs, refValue)
						mergeClaudeObjectSchemaInfo(&info, resolved)
					}
				}
			}
			if allOf := schema.Get("allOf"); allOf.IsArray() {
				for _, branch := range allOf.Array() {
					branchInfo := analyzeClaudeObjectSchemaDepth(branch, root, depth+1, visitedRefs)
					mergeClaudeObjectSchemaInfo(&info, branchInfo)
				}
			}
			info.complex = true
			return info
		}
		branchInfo := analyzeClaudeObjectSchemaDepth(branches[0], root, depth+1, visitedRefs)
		mergeClaudeObjectSchemaInfo(&info, branchInfo)
	}
	if ref := schema.Get("$ref"); ref.Exists() {
		refValue := ref.String()
		if visitedRefs[refValue] {
			info.complex = true
			return info
		}
		target, ok := claudeResolveLocalRef(root, refValue)
		if !ok {
			info.complex = true
			return info
		}
		visitedRefs[refValue] = true
		resolved := analyzeClaudeObjectSchemaDepth(target, root, depth+1, visitedRefs)
		delete(visitedRefs, refValue)
		mergeClaudeObjectSchemaInfo(&info, resolved)
	}
	if allOf := schema.Get("allOf"); allOf.IsArray() {
		for _, branch := range allOf.Array() {
			branchInfo := analyzeClaudeObjectSchemaDepth(branch, root, depth+1, visitedRefs)
			mergeClaudeObjectSchemaInfo(&info, branchInfo)
		}
	} else if allOf.Exists() {
		info.complex = true
	}
	if claudeSchemaAllowsDynamic(schema) {
		info.allowsDynamic = true
	}
	if claudeSchemaForbidsDynamic(schema) {
		info.forbidsDynamic = true
	}
	if claudeSchemaHasPatternProperties(schema) {
		info.hasPatternProperties = true
	}
	props := schema.Get("properties")
	if props.Exists() {
		if !props.IsObject() {
			info.complex = true
		} else {
			info.hasProperties = true
			info.declaresObject = true
			for key, value := range props.Map() {
				info.properties[key] = appendUniqueSchema(info.properties[key], value)
			}
		}
	}
	for _, requiredName := range schema.Get("required").Array() {
		info.required[requiredName.String()] = true
	}
	return info
}

func appendUniqueSchema(schemas []ojson.Result, value ojson.Result) []ojson.Result {
	for _, existing := range schemas {
		if existing.Raw == value.Raw {
			return schemas
		}
	}
	return append(schemas, value)
}

func mergeClaudeObjectSchemaInfo(dst *claudeObjectSchemaInfo, src claudeObjectSchemaInfo) {
	if dst.properties == nil {
		dst.properties = map[string][]ojson.Result{}
	}
	if dst.required == nil {
		dst.required = map[string]bool{}
	}
	if src.hasProperties {
		dst.hasProperties = true
		for key, values := range src.properties {
			for _, value := range values {
				dst.properties[key] = appendUniqueSchema(dst.properties[key], value)
			}
		}
	}
	for name := range src.required {
		dst.required[name] = true
	}
	dst.branchInfos = append(dst.branchInfos, src.branchInfos...)
	dst.allowsDynamic = dst.allowsDynamic || src.allowsDynamic
	dst.forbidsDynamic = dst.forbidsDynamic || src.forbidsDynamic
	dst.hasPatternProperties = dst.hasPatternProperties || src.hasPatternProperties
	dst.complex = dst.complex || src.complex
	dst.unsafeComplex = dst.unsafeComplex || src.unsafeComplex
	dst.declaresObject = dst.declaresObject || src.declaresObject
}

func findClaudeToolSchema(originalRequestRawJSON []byte, toolName string) (ojson.Result, ojson.Result, bool) {
	if toolName == "" || len(originalRequestRawJSON) == 0 || !ojson.ValidBytes(originalRequestRawJSON) {
		return ojson.Result{}, ojson.Result{}, false
	}
	root := ojson.ParseBytes(originalRequestRawJSON)
	tools := root.Get("tools")
	if !tools.IsArray() {
		return ojson.Result{}, root, false
	}
	type candidate struct {
		name   string
		schema ojson.Result
		found  bool
	}
	var exact []candidate
	canonicalToolName := CanonicalToolName(toolName)
	canonicalMatches := map[string][]candidate{}
	for _, tool := range tools.Array() {
		name := strings.TrimSpace(tool.Get("name").String())
		if name == "" {
			name = strings.TrimSpace(tool.Get("function.name").String())
		}
		if name == "" {
			continue
		}
		schema := tool.Get("input_schema")
		if !schema.Exists() {
			schema = tool.Get("parameters")
		}
		if !schema.Exists() {
			schema = tool.Get("function.parameters")
		}
		c := candidate{name: name, schema: schema, found: true}
		if name == toolName {
			exact = append(exact, c)
		}
		if CanonicalToolName(name) == canonicalToolName {
			canonicalMatches[name] = append(canonicalMatches[name], c)
		}
	}
	if len(exact) == 1 {
		return exact[0].schema, root, true
	}
	if len(exact) > 1 {
		firstRaw := exact[0].schema.Raw
		identical := true
		for _, other := range exact[1:] {
			if other.schema.Raw != firstRaw {
				identical = false
				break
			}
		}
		if identical {
			return exact[0].schema, root, true
		}
		return ojson.Result{}, root, false
	}
	if len(canonicalMatches) == 1 {
		for _, matches := range canonicalMatches {
			if len(matches) == 1 {
				return matches[0].schema, root, true
			}
			firstRaw := matches[0].schema.Raw
			identical := true
			for _, other := range matches[1:] {
				if other.schema.Raw != firstRaw {
					identical = false
					break
				}
			}
			if identical {
				return matches[0].schema, root, true
			}
		}
	}
	return ojson.Result{}, root, false
}

func ClaudeToolIsParameterless(toolName string, originalRequestRawJSON []byte) bool {
	schema, root, found := findClaudeToolSchema(originalRequestRawJSON, toolName)
	if !found {
		return false
	}
	if !schema.Exists() {
		return true
	}
	_ = root
	info := analyzeClaudeObjectSchema(schema, schema)
	if info.complex || claudeInfoAnyBranchAllowsDynamic(info) || !info.declaresObject || !info.forbidsDynamic {
		return false
	}
	return len(info.required) == 0 && (!info.hasProperties || len(info.properties) == 0)
}

func resolveClaudeArrayItemsSchema(schema ojson.Result, root ojson.Result, depth int, visitedRefs map[string]bool) ([]ojson.Result, bool) {
	if depth > 16 || !schema.Exists() || !schema.IsObject() {
		return nil, false
	}
	var items []ojson.Result
	deterministic := true
	collect := func(candidate ojson.Result) {
		items = appendUniqueSchema(items, candidate)
	}
	if ref := schema.Get("$ref"); ref.Exists() {
		refValue := ref.String()
		if visitedRefs[refValue] {
			return nil, false
		}
		target, ok := claudeResolveLocalRef(root, refValue)
		if !ok {
			return nil, false
		}
		visitedRefs[refValue] = true
		targetItems, targetOK := resolveClaudeArrayItemsSchema(target, root, depth+1, visitedRefs)
		delete(visitedRefs, refValue)
		if !targetOK {
			deterministic = false
		} else {
			for _, item := range targetItems {
				collect(item)
			}
		}
	}
	if schema.Get("anyOf").Exists() || schema.Get("oneOf").Exists() {
		branches, okBranches := claudeUnionBranches(schema)
		if !okBranches || len(branches) != 1 {
			deterministic = false
		} else {
			branchItems, branchOK := resolveClaudeArrayItemsSchema(branches[0], root, depth+1, visitedRefs)
			if !branchOK {
				deterministic = false
			} else {
				for _, item := range branchItems {
					collect(item)
				}
			}
		}
	}
	if allOf := schema.Get("allOf"); allOf.IsArray() {
		for _, branch := range allOf.Array() {
			branchItems, branchOK := resolveClaudeArrayItemsSchema(branch, root, depth+1, visitedRefs)
			if !branchOK {
				deterministic = false
				continue
			}
			for _, item := range branchItems {
				collect(item)
			}
		}
	} else if allOf.Exists() {
		deterministic = false
	}
	if directItems := schema.Get("items"); directItems.Exists() {
		collect(directItems)
	}
	return items, deterministic
}

func claudeDeclaredPropertyPlaceholders(info claudeObjectSchemaInfo) (bool, bool) {
	_, reasonDeclared := info.properties["reason"]
	_, underscoreDeclared := info.properties["_"]
	return reasonDeclared, underscoreDeclared
}

func claudeArraySchemaUnsafe(schema ojson.Result, root ojson.Result) bool {
	return analyzeClaudeObjectSchema(schema, root).unsafeComplex
}

func cleanClaudeToolArray(argsJSON string, schemas []ojson.Result, root ojson.Result, path string) string {
	for _, schema := range schemas {
		if claudeArraySchemaUnsafe(schema, root) {

			return argsJSON
		}
	}
	var itemSchemas []ojson.Result
	deterministic := true
	for _, schema := range schemas {
		resolvedItems, okItems := resolveClaudeArrayItemsSchema(schema, root, 0, map[string]bool{})
		if !okItems {
			deterministic = false
			continue
		}
		if len(resolvedItems) == 0 {

			itemSchemas = appendUniqueSchema(itemSchemas, schema)
			continue
		}
		for _, item := range resolvedItems {
			itemSchemas = appendUniqueSchema(itemSchemas, item)
		}
	}
	if !deterministic || len(itemSchemas) == 0 {
		return argsJSON
	}
	cleaned := argsJSON
	elementCount := len(ojson.Get(cleaned, path).Array())
	for index := 0; index < elementCount; index++ {
		elementPath := joinPath(path, strconv.Itoa(index))
		element := ojson.Get(cleaned, elementPath)
		if element.IsObject() {
			cleaned = cleanClaudeToolObject(cleaned, itemSchemas, root, elementPath, false)
		} else if element.IsArray() {
			cleaned = cleanClaudeToolArray(cleaned, itemSchemas, root, elementPath)
		}
	}
	return cleaned
}

func cleanClaudeToolObject(argsJSON string, schemas []ojson.Result, root ojson.Result, path string, topLevel bool) string {
	argsResult := ojson.Get(argsJSON, path)
	if path == "" {
		argsResult = ojson.Parse(argsJSON)
	}
	if !argsResult.IsObject() || len(schemas) == 0 {
		return argsJSON
	}
	merged := claudeObjectSchemaInfo{properties: map[string][]ojson.Result{}}
	for _, schema := range schemas {
		mergeClaudeObjectSchemaInfo(&merged, analyzeClaudeObjectSchema(schema, root))
	}
	if merged.complex || !merged.declaresObject {
		return argsJSON
	}
	isEmptySchema := len(merged.required) == 0 && (!merged.hasProperties || len(merged.properties) == 0)
	allSchemasDeclareObject := true
	for _, schema := range schemas {
		if !analyzeClaudeObjectSchema(schema, root).declaresObject {
			allSchemasDeclareObject = false
			break
		}
	}
	if isEmptySchema && allSchemasDeclareObject && merged.forbidsDynamic && !claudeInfoAnyBranchAllowsDynamic(merged) && !claudeOriginalsAnyBranchAllowsDynamic(schemas, root) && !claudeOriginalsAnyCandidateAcceptsImplicitly(schemas, root, argsResult) {
		if len(argsResult.Map()) == 0 {
			return argsJSON
		}
		if topLevel {
			return "{}"
		}
		updated, _ := ojson.SetRaw(argsJSON, path, "{}")
		return updated
	}
	keys := make([]string, 0, len(merged.properties))
	for key := range merged.properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cleaned := argsJSON
	for _, key := range keys {
		propertySchemas := merged.properties[key]
		childPath := joinPath(path, escapeGJSONPathKey(key))
		child := ojson.Get(cleaned, childPath)
		if !child.Exists() {
			continue
		}
		currentForBranch := ojson.Parse(cleaned)
		if path != "" {
			currentForBranch = ojson.Get(cleaned, path)
		}
		if claudeParentBranchAcceptsChild(merged, key, currentForBranch) || claudeOriginalsMayPermitChild(schemas, root, key, currentForBranch) {
			continue
		}
		if child.IsObject() {
			cleaned = cleanClaudeToolObject(cleaned, propertySchemas, root, childPath, false)
		} else if child.IsArray() {
			cleaned = cleanClaudeToolArray(cleaned, propertySchemas, root, childPath)
		}
	}
	if claudeInfoAnyBranchAllowsDynamic(merged) {
		return cleaned
	}
	current := ojson.Parse(cleaned)
	if path != "" {
		current = ojson.Get(cleaned, path)
	}
	if !current.IsObject() {
		return cleaned
	}
	reasonDeclared, underscoreDeclared := claudeDeclaredPropertyPlaceholders(merged)
	doc := ojson.NewDocString(cleaned)
	docModified := false
	for _, placeholder := range []string{"reason", "_"} {
		if placeholder == "reason" && reasonDeclared {
			continue
		}
		if placeholder == "_" && underscoreDeclared {
			continue
		}
		if merged.required[placeholder] {
			continue
		}
		if claudeInfoAnyBranchAllowsDynamic(merged) || claudeOriginalsAnyBranchAllowsDynamic(schemas, root) {
			continue
		}
		if claudeParentBranchAcceptsChild(merged, placeholder, current) {
			continue
		}
		if claudeOriginalsAnyCandidateAcceptsImplicitly(schemas, root, current) {
			continue
		}
		if _, ok := current.Map()[placeholder]; ok {
			placeholderPath := joinPath(path, placeholder)
			if doc.Delete(placeholderPath) == nil {
				docModified = true
			}
		}
	}
	if docModified {
		cleaned = doc.String()
	}
	parsedCleaned := ojson.Parse(cleaned)
	if path != "" {
		parsedCleaned = ojson.Get(cleaned, path)
	}
	if parsedCleaned.IsObject() && len(parsedCleaned.Map()) == 0 && path == "" {
		return "{}"
	}
	return cleaned
}

func findTranslatedClaudeToolSchemas(translatedRequestRawJSON []byte, toolName string, originalRequestRawJSON []byte) []ojson.Result {
	if toolName == "" || len(translatedRequestRawJSON) == 0 || !ojson.ValidBytes(translatedRequestRawJSON) {
		return nil
	}
	root := ojson.ParseBytes(translatedRequestRawJSON)
	type namedSchema struct {
		name   string
		schema ojson.Result
	}
	var named []namedSchema
	collectNamed := func(tools ojson.Result) {
		if !tools.IsArray() {
			return
		}
		for _, toolGroup := range tools.Array() {
			decls := toolGroup.Get("functionDeclarations")
			if !decls.IsArray() {
				continue
			}
			for _, decl := range decls.Array() {
				name := strings.TrimSpace(decl.Get("name").String())
				if name == "" {
					continue
				}
				schema := decl.Get("parametersJsonSchema")
				if !schema.Exists() {
					schema = decl.Get("parameters")
				}
				if schema.Exists() && schema.IsObject() {
					named = append(named, namedSchema{name: name, schema: schema})
				}
			}
		}
	}
	collectNamed(root.Get("tools"))
	collectNamed(root.Get("request.tools"))
	if len(named) == 0 {
		return nil
	}
	canonicalToolName := CanonicalToolName(toolName)
	sanitizedToolName := SanitizeFunctionName(toolName)

	sentName := ""
	if forward := SanitizedFunctionNameMap(originalRequestRawJSON); forward != nil {
		sentName = forward[toolName]
	}
	var exact []ojson.Result
	canonicalByName := map[string][]ojson.Result{}
	for _, entry := range named {
		if entry.name == toolName || (sentName != "" && entry.name == sentName) {
			exact = append(exact, entry.schema)
		}
		if CanonicalToolName(entry.name) == canonicalToolName || entry.name == sanitizedToolName || SanitizeFunctionName(entry.name) == sanitizedToolName {
			canonicalByName[entry.name] = append(canonicalByName[entry.name], entry.schema)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(canonicalByName) == 1 {
		for _, schemas := range canonicalByName {
			return schemas
		}
	}
	return nil
}

func claudeOriginalDeclaresPropertyDeep(schema ojson.Result, root ojson.Result, property string, depth int, visitedRefs map[string]bool) bool {
	if depth > 16 || !schema.Exists() || !schema.IsObject() {
		return false
	}
	if ref := schema.Get("$ref"); ref.Exists() {
		refValue := ref.String()
		if !visitedRefs[refValue] {
			if target, ok := claudeResolveLocalRef(root, refValue); ok {
				visitedRefs[refValue] = true
				declared := claudeOriginalDeclaresPropertyDeep(target, root, property, depth+1, visitedRefs)
				delete(visitedRefs, refValue)
				if declared {
					return true
				}
			}
		}
	}
	if _, ok := schema.Get("properties").Map()[property]; ok {
		return true
	}
	for _, requiredName := range schema.Get("required").Array() {
		if requiredName.String() == property {
			return true
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		for _, branch := range schema.Get(keyword).Array() {
			if claudeOriginalDeclaresPropertyDeep(branch, root, property, depth+1, visitedRefs) {
				return true
			}
		}
	}
	return false
}

func claudeInfoAnyBranchAllowsDynamic(info claudeObjectSchemaInfo) bool {
	if info.hasPatternProperties {
		return true
	}
	if len(info.branchInfos) == 0 {
		return info.allowsDynamic && !info.forbidsDynamic
	}
	for _, branch := range info.branchInfos {
		if claudeInfoAnyBranchAllowsDynamic(branch) {
			return true
		}
	}
	return info.allowsDynamic && !info.forbidsDynamic
}

func claudeBranchMayAccept(info claudeObjectSchemaInfo, key string, current ojson.Result) bool {

	if info.hasPatternProperties {
		return true
	}

	if !info.forbidsDynamic {
		matches := true
		for requiredName := range info.required {
			if _, ok := current.Map()[requiredName]; !ok {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	if info.allowsDynamic && !info.forbidsDynamic {
		return true
	}
	for _, branch := range info.branchInfos {
		_, branchDeclaresKey := branch.properties[key]
		if branchDeclaresKey {

			continue
		}
		if claudeBranchMayAccept(branch, key, current) {
			return true
		}
	}
	return false
}

func claudeParentBranchAcceptsChild(originalInfo claudeObjectSchemaInfo, key string, current ojson.Result) bool {
	if len(originalInfo.branchInfos) == 0 {
		return false
	}
	for _, branch := range originalInfo.branchInfos {
		_, branchDeclaresKey := branch.properties[key]
		if branchDeclaresKey {
			continue
		}
		if claudeBranchMayAccept(branch, key, current) {
			return true
		}
	}
	return false
}

func claudeCandidateMayPermitChild(info claudeObjectSchemaInfo, key string, current ojson.Result) bool {
	if _, declaresKey := info.properties[key]; declaresKey {
		return false
	}
	if info.hasPatternProperties {
		return true
	}
	if info.forbidsDynamic {
		return false
	}
	for requiredName := range info.required {
		if _, ok := current.Map()[requiredName]; !ok {
			return false
		}
	}
	return true
}

func claudeOriginalsMayPermitChild(originals []ojson.Result, root ojson.Result, key string, current ojson.Result) bool {
	if len(originals) <= 1 {
		return false
	}
	for _, original := range originals {
		info := analyzeClaudeObjectSchema(original, root)
		if info.complex {
			continue
		}
		if claudeCandidateMayPermitChild(info, key, current) {
			return true
		}
	}
	return false
}

func claudeOriginalsAnyCandidateAcceptsImplicitly(originals []ojson.Result, root ojson.Result, current ojson.Result) bool {
	if len(originals) <= 1 {
		return false
	}
	for _, original := range originals {
		info := analyzeClaudeObjectSchema(original, root)
		if info.complex || info.forbidsDynamic {
			continue
		}
		matches := true
		for requiredName := range info.required {
			if _, ok := current.Map()[requiredName]; !ok {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func claudeOriginalsAnyBranchAllowsDynamic(originals []ojson.Result, root ojson.Result) bool {
	for _, original := range originals {
		if info := analyzeClaudeObjectSchema(original, root); claudeInfoAnyBranchAllowsDynamic(info) {
			return true
		}
	}
	return false
}

func mergedClaudeOriginalInfo(originals []ojson.Result, root ojson.Result) claudeObjectSchemaInfo {
	merged := claudeObjectSchemaInfo{properties: map[string][]ojson.Result{}}
	for _, original := range originals {
		mergeClaudeObjectSchemaInfo(&merged, analyzeClaudeObjectSchema(original, root))
	}
	return merged
}

func cleanClaudeToolArgumentsByTranslatedSchema(argsJSON string, translatedSchemas []ojson.Result, originals []ojson.Result, root ojson.Result, path string, topLevel bool) string {
	current := ojson.Parse(argsJSON)
	if path != "" {
		current = ojson.Get(argsJSON, path)
	}
	if !current.IsObject() || len(translatedSchemas) == 0 || len(originals) == 0 {
		return argsJSON
	}
	for _, original := range originals {
		if !original.IsObject() {
			return argsJSON
		}
	}
	originalInfo := mergedClaudeOriginalInfo(originals, root)
	if originalInfo.unsafeComplex {
		return argsJSON
	}
	if originalInfo.complex {

		hasUnion := false
		for _, original := range originals {
			if original.Get("anyOf").IsArray() || original.Get("oneOf").IsArray() {
				hasUnion = true
			}
		}
		if !hasUnion {
			return argsJSON
		}
	}
	cleaned := argsJSON

	if originalInfo.hasProperties {
		keys := make([]string, 0, len(originalInfo.properties))
		for key := range originalInfo.properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := joinPath(path, escapeGJSONPathKey(key))
			child := ojson.Get(cleaned, childPath)
			if !child.Exists() {
				continue
			}
			var translatedChildren []ojson.Result
			for _, translated := range translatedSchemas {
				translatedInfo := analyzeClaudeObjectSchema(translated, translated)
				for _, childSchema := range translatedInfo.properties[key] {
					translatedChildren = appendUniqueSchema(translatedChildren, childSchema)
				}
			}
			if len(translatedChildren) == 0 {
				continue
			}
			originalChildren := originalInfo.properties[key]
			currentForBranch := ojson.Parse(cleaned)
			if path != "" {
				currentForBranch = ojson.Get(cleaned, path)
			}
			if claudeParentBranchAcceptsChild(originalInfo, key, currentForBranch) || claudeOriginalsMayPermitChild(originals, root, key, currentForBranch) {

				continue
			}
			if child.IsObject() && len(originalChildren) > 0 {
				cleaned = cleanClaudeToolArgumentsByTranslatedSchema(cleaned, translatedChildren, originalChildren, root, childPath, false)
			} else if child.IsArray() && len(originalChildren) > 0 {
				cleaned = cleanClaudeTranslatedArray(cleaned, translatedChildren, originalChildren, root, childPath)
			}
		}
	}
	currentAfterChildren := ojson.Parse(cleaned)
	if path != "" {
		currentAfterChildren = ojson.Get(cleaned, path)
	}
	if !currentAfterChildren.IsObject() {
		return cleaned
	}
	var placeholdersToDelete []string
	for _, placeholder := range []string{"reason", "_"} {
		if _, declared := originalInfo.properties[placeholder]; declared {
			continue
		}
		if originalInfo.required[placeholder] {
			continue
		}
		if claudeInfoAnyBranchAllowsDynamic(originalInfo) || claudeOriginalsAnyBranchAllowsDynamic(originals, root) {

			continue
		}
		if claudeOriginalsAnyCandidateAcceptsImplicitly(originals, root, currentAfterChildren) {

			continue
		}
		if claudeParentBranchAcceptsChild(originalInfo, placeholder, currentAfterChildren) {

			continue
		}
		declaredDeep := false
		for _, original := range originals {
			if claudeOriginalDeclaresPropertyDeep(original, root, placeholder, 0, map[string]bool{}) {
				declaredDeep = true
				break
			}
		}
		if declaredDeep {
			continue
		}
		if _, ok := currentAfterChildren.Map()[placeholder]; !ok {
			continue
		}
		injected := false
		for _, translated := range translatedSchemas {
			translatedInfo := analyzeClaudeObjectSchema(translated, translated)
			placeholderSchemas, declaredTranslated := translatedInfo.properties[placeholder]
			if !declaredTranslated {
				continue
			}
			if placeholder == "reason" {
				for _, placeholderSchema := range placeholderSchemas {
					required := false
					for _, req := range translated.Get("required").Array() {
						if req.String() == "reason" {
							required = true
						}
					}
					if required || placeholderSchema.Get("description").String() == placeholderReasonDescription {
						injected = true
					}
				}

				if !injected {
					for _, req := range translated.Get("required").Array() {
						if req.String() == "reason" {
							injected = true
						}
					}
				}
			} else {
				for _, req := range translated.Get("required").Array() {
					if req.String() == "_" {
						injected = true
					}
				}

				if !injected && !topLevel {
					for _, placeholderSchema := range placeholderSchemas {
						if placeholderSchema.Get("type").String() == "boolean" {
							injected = true
						}
					}
				}
			}
		}

		if !injected {
			for _, translated := range translatedSchemas {
				for _, keyword := range []string{"anyOf", "oneOf"} {
					for _, branch := range translated.Get(keyword).Array() {
						branchInfo := analyzeClaudeObjectSchema(branch, branch)
						if _, declaredBranch := branchInfo.properties[placeholder]; !declaredBranch {
							continue
						}
						for _, req := range branch.Get("required").Array() {
							if req.String() == placeholder {
								injected = true
							}
						}
					}
				}
			}
		}

		if !injected && topLevel && originalInfo.forbidsDynamic {
			for _, translated := range translatedSchemas {

				wrapped, errWrap := ojson.SetRaw("{}", "schema", translated.Raw)
				if errWrap != nil {
					continue
				}
				recleaned := ojson.Get(CleanJSONSchemaForAntigravity(wrapped), "schema")
				if !recleaned.Exists() {
					continue
				}
				recleanedInfo := analyzeClaudeObjectSchema(recleaned, recleaned)
				if _, declaredRecleaned := recleanedInfo.properties[placeholder]; !declaredRecleaned {
					continue
				}
				for _, req := range recleaned.Get("required").Array() {
					if req.String() == placeholder {
						injected = true
					}
				}
			}
		}
		if injected {
			placeholdersToDelete = append(placeholdersToDelete, joinPath(path, placeholder))
		}
	}
	doc := ojson.NewDocString(cleaned)
	docModified := false
	for _, placeholderPath := range placeholdersToDelete {
		if doc.Delete(placeholderPath) == nil {
			docModified = true
		}
	}

	allCandidatesDeclareObject := true
	for _, original := range originals {
		if !analyzeClaudeObjectSchema(original, root).declaresObject {
			allCandidatesDeclareObject = false
			break
		}
	}
	currentForNormalize := doc.Get(path)
	if path == "" {
		if docModified {
			cleaned = doc.String()
			docModified = false
		}
		currentForNormalize = ojson.Parse(cleaned)
	}
	if !originalInfo.complex && allCandidatesDeclareObject && originalInfo.forbidsDynamic && !claudeInfoAnyBranchAllowsDynamic(originalInfo) && !claudeOriginalsAnyBranchAllowsDynamic(originals, root) && !claudeOriginalsAnyCandidateAcceptsImplicitly(originals, root, currentForNormalize) && originalInfo.declaresObject && len(originalInfo.required) == 0 && (!originalInfo.hasProperties || len(originalInfo.properties) == 0) {
		if topLevel {
			return "{}"
		}
		if doc.SetRaw(path, "{}") == nil {
			return doc.String()
		}
		return cleaned
	}
	if docModified {
		cleaned = doc.String()
	}
	parsedCleaned := ojson.Parse(cleaned)
	if path != "" {
		parsedCleaned = ojson.Get(cleaned, path)
	}
	if parsedCleaned.IsObject() && len(parsedCleaned.Map()) == 0 && path == "" {
		return "{}"
	}
	_ = root
	return cleaned
}

func cleanClaudeTranslatedArray(argsJSON string, translatedArraySchemas []ojson.Result, originalArraySchemas []ojson.Result, root ojson.Result, path string) string {
	for _, schema := range originalArraySchemas {
		if claudeArraySchemaUnsafe(schema, root) {
			return argsJSON
		}
	}
	var translatedItems []ojson.Result
	var originalItems []ojson.Result
	deterministic := true
	for _, schema := range translatedArraySchemas {
		items, okItems := resolveClaudeArrayItemsSchema(schema, schema, 0, map[string]bool{})
		if !okItems {
			deterministic = false
		}
		for _, item := range items {
			translatedItems = appendUniqueSchema(translatedItems, item)
		}
	}
	for _, schema := range originalArraySchemas {
		items, okItems := resolveClaudeArrayItemsSchema(schema, root, 0, map[string]bool{})
		if !okItems {
			deterministic = false
		}
		if okItems && len(items) == 0 {
			originalItems = appendUniqueSchema(originalItems, schema)
			continue
		}
		for _, item := range items {
			originalItems = appendUniqueSchema(originalItems, item)
		}
	}
	if !deterministic || len(translatedItems) == 0 || len(originalItems) == 0 {
		return argsJSON
	}
	cleaned := argsJSON
	elementCount := len(ojson.Get(cleaned, path).Array())
	for index := 0; index < elementCount; index++ {
		elementPath := joinPath(path, strconv.Itoa(index))
		element := ojson.Get(cleaned, elementPath)
		if element.IsObject() {
			cleaned = cleanClaudeToolArgumentsByTranslatedSchema(cleaned, translatedItems, originalItems, root, elementPath, false)
		} else if element.IsArray() {
			cleaned = cleanClaudeTranslatedArray(cleaned, translatedItems, originalItems, root, elementPath)
		}
	}
	return cleaned
}

func CleanClaudeToolArgumentsWithTranslated(rawArgs string, toolName string, originalRequestRawJSON []byte, translatedRequestRawJSON []byte) string {
	if translatedSchemas := findTranslatedClaudeToolSchemas(translatedRequestRawJSON, toolName, originalRequestRawJSON); len(translatedSchemas) > 0 {
		rawArgs = strings.TrimSpace(rawArgs)
		if rawArgs == "" || rawArgs == "{}" {
			return "{}"
		}
		if !ojson.Valid(rawArgs) || !ojson.Parse(rawArgs).IsObject() {
			return rawArgs
		}
		schema, _, found := findClaudeToolSchema(originalRequestRawJSON, toolName)
		if !found {
			return rawArgs
		}
		if !schema.Exists() {
			return "{}"
		}
		return cleanClaudeToolArgumentsByTranslatedSchema(rawArgs, translatedSchemas, []ojson.Result{schema}, schema, "", true)
	}
	return CleanClaudeToolArguments(rawArgs, toolName, originalRequestRawJSON)
}

func CleanClaudeToolArguments(rawArgs string, toolName string, originalRequestRawJSON []byte) string {
	rawArgs = strings.TrimSpace(rawArgs)
	if rawArgs == "" || rawArgs == "{}" {
		return "{}"
	}
	if !ojson.Valid(rawArgs) {
		return rawArgs
	}
	schema, root, found := findClaudeToolSchema(originalRequestRawJSON, toolName)
	if !found {
		return rawArgs
	}
	if !schema.Exists() {
		return "{}"
	}
	argsResult := ojson.Parse(rawArgs)
	if !argsResult.IsObject() {
		return rawArgs
	}
	_ = root
	info := analyzeClaudeObjectSchema(schema, schema)
	if info.complex || !info.declaresObject {
		return rawArgs
	}
	if len(info.required) == 0 && (!info.hasProperties || len(info.properties) == 0) && info.forbidsDynamic && !claudeInfoAnyBranchAllowsDynamic(info) {
		return "{}"
	}
	return cleanClaudeToolObject(rawArgs, []ojson.Result{schema}, schema, "", true)
}
