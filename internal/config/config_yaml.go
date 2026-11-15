package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func SaveConfigPreserveComments(configFile string, cfg *Config, migrateV8 ...bool) error {
	persistCfg := cfg
	migrating := len(migrateV8) > 0 && migrateV8[0]

	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}

	var original yaml.Node
	if err = yaml.Unmarshal(data, &original); err != nil {
		return err
	}
	if original.Kind != yaml.DocumentNode || len(original.Content) == 0 {
		return fmt.Errorf("invalid yaml document structure")
	}
	if original.Content[0] == nil || original.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("expected root mapping node")
	}
	layout := deepCopyNode(original.Content[0])
	flat, err := flattenV8(layout)
	if err != nil {
		return err
	}
	original.Content[0] = flat
	layout = expandConfigAliases(layout)
	migrating = migrating || IsV8ConfigLayout(layout)

	rendered, err := yaml.Marshal((*legacyConfig)(persistCfg))
	if err != nil {
		return err
	}
	var generated yaml.Node
	if err = yaml.Unmarshal(rendered, &generated); err != nil {
		return err
	}
	if generated.Kind != yaml.DocumentNode || len(generated.Content) == 0 || generated.Content[0] == nil {
		return fmt.Errorf("invalid generated yaml structure")
	}
	if generated.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("expected generated root mapping node")
	}

	if !migrating {
		removeLegacyAuthBlock(original.Content[0])
		removeRemovedIntegrationKeys(original.Content[0])
		removeLegacyGenerativeLanguageKeys(original.Content[0])
	}
	removeLegacyOpenAICompatAPIKeys(original.Content[0])

	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "oauth-excluded-models")
	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "oauth-model-alias")
	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "oauth-request-scoped-errors")
	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "oauth-settings")
	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "antigravity", "sensitive-words-replace")
	pruneMappingToGeneratedKeys(original.Content[0], generated.Content[0], "devin", "sensitive-words-replace")
	replacePluginConfigsSubtree(original.Content[0], generated.Content[0])

	mergeMappingPreserve(original.Content[0], generated.Content[0])
	if err = restoreV8Layout(original.Content[0], layout, data, generated.Content[0]); err != nil {
		return err
	}
	if !migrating {

		for _, alias := range v8ClientPaths {
			if yamlPath(layout, alias.old) == nil || yamlPath(layout, alias.current) != nil || yamlPath(original.Content[0], alias.current) == nil {
				continue
			}
			copy := copyYAMLPathValue(original.Content[0], alias.current)
			deleteYAMLPath(original.Content[0], alias.current)
			setYAMLPathWithComments(original.Content[0], alias.old, copy)
		}
	}
	NormalizeCollectionNodeStyles(original.Content[0])

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err = enc.Encode(&original); err != nil {
		_ = enc.Close()
		return err
	}
	if err = enc.Close(); err != nil {
		return err
	}
	data = NormalizeCommentIndentation(buf.Bytes())
	var migrated *Config
	if migrating {
		data, _, err = NormalizeConfigLayout(data, true)
		if err != nil {
			return err
		}
		migrated = new(Config)
		if err = yaml.Unmarshal(data, migrated); err != nil {
			return fmt.Errorf("decode migrated config: %w", err)
		}
	}
	if err = os.WriteFile(configFile, data, 0600); err != nil {
		return err
	}
	if migrated != nil {

		cfg.OAuthOnlyFields = migrated.OAuthOnlyFields
	}
	return nil
}

func SaveConfigPreserveCommentsUpdateNestedScalar(configFile string, path []string, value string) error {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	data, err = updateNestedScalarYAML(data, path, value)
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0600)
}

func updateNestedScalarYAML(data []byte, path []string, value string) ([]byte, error) {
	var root yaml.Node
	if errUnmarshal := yaml.Unmarshal(data, &root); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("invalid yaml document structure")
	}

	var decoded map[string]any
	if errDecode := root.Decode(&decoded); errDecode != nil {
		return nil, errDecode
	}
	root.Content[0] = expandConfigAliases(root.Content[0])
	node := root.Content[0]

	for i, key := range path {
		if i == len(path)-1 {

			v := getOrCreateMapValue(node, key)
			v.Kind = yaml.ScalarNode
			v.Tag = "!!str"
			v.Value = value
		} else {
			next := getOrCreateMapValue(node, key)
			if next.Kind != yaml.MappingNode {
				next.Kind = yaml.MappingNode
				next.Tag = "!!map"
			}
			node = next
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if errEncode := enc.Encode(&root); errEncode != nil {
		_ = enc.Close()
		return nil, errEncode
	}
	if errClose := enc.Close(); errClose != nil {
		return nil, errClose
	}
	return NormalizeCommentIndentation(buf.Bytes()), nil
}

func NormalizeCommentIndentation(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	changed := false
	for i, line := range lines {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) == 0 || trimmed[0] != '#' {
			continue
		}
		if len(trimmed) == len(line) {
			continue
		}
		lines[i] = append([]byte(nil), trimmed...)
		changed = true
	}
	if !changed {
		return data
	}
	return bytes.Join(lines, []byte("\n"))
}

func getOrCreateMapValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode.Kind != yaml.MappingNode {
		mapNode.Kind = yaml.MappingNode
		mapNode.Tag = "!!map"
		mapNode.Content = nil
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		k := mapNode.Content[i]
		if k.Value == key {
			return mapNode.Content[i+1]
		}
	}

	mapNode.Content = append(mapNode.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key})
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""}
	mapNode.Content = append(mapNode.Content, val)
	return val
}

func mergeMappingPreserve(dst, src *yaml.Node, path ...[]string) {
	var currentPath []string
	if len(path) > 0 {
		currentPath = path[0]
	}

	if dst == nil || src == nil {
		return
	}
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {

		copyNodeShallow(dst, src)
		return
	}
	for i := 0; i+1 < len(src.Content); i += 2 {
		sk := src.Content[i]
		sv := src.Content[i+1]
		idx := findMapKeyIndex(dst, sk.Value)
		childPath := appendPath(currentPath, sk.Value)
		if isPluginConfigsPath(childPath) {
			continue
		}
		if idx >= 0 {

			dv := dst.Content[idx+1]
			mergeNodePreserve(dv, sv, childPath)
		} else {

			candidate := deepCopyNode(sv)
			pruneKnownDefaultsInNewNode(childPath, candidate)
			if isKnownDefaultValue(childPath, candidate) {
				continue
			}
			dst.Content = append(dst.Content, deepCopyNode(sk), candidate)
		}
	}
}

func mergeNodePreserve(dst, src *yaml.Node, path ...[]string) {
	var currentPath []string
	if len(path) > 0 {
		currentPath = path[0]
	}

	if dst == nil || src == nil {
		return
	}
	switch src.Kind {
	case yaml.MappingNode:
		if dst.Kind != yaml.MappingNode {
			copyNodeShallow(dst, src)
		}
		mergeMappingPreserve(dst, src, currentPath)
		if shouldPruneNestedMappingKeys(currentPath) {
			pruneMissingMapKeys(dst, src)
		}
	case yaml.SequenceNode:

		if dst.Kind == yaml.ScalarNode && dst.Tag == "!!null" && len(src.Content) == 0 {

			return
		}
		if dst.Kind != yaml.SequenceNode {
			dst.Kind = yaml.SequenceNode
			dst.Tag = "!!seq"
			dst.Content = nil
		}
		reorderSequenceForMerge(dst, src)

		minContent := len(dst.Content)
		if len(src.Content) < minContent {
			minContent = len(src.Content)
		}
		for i := 0; i < minContent; i++ {
			if dst.Content[i] == nil {
				dst.Content[i] = deepCopyNode(src.Content[i])
				continue
			}
			mergeNodePreserve(dst.Content[i], src.Content[i], currentPath)
			if dst.Content[i] != nil && src.Content[i] != nil &&
				dst.Content[i].Kind == yaml.MappingNode && src.Content[i].Kind == yaml.MappingNode {
				pruneMissingMapKeys(dst.Content[i], src.Content[i])
			}
		}

		for i := len(dst.Content); i < len(src.Content); i++ {
			dst.Content = append(dst.Content, deepCopyNode(src.Content[i]))
		}

		if len(src.Content) < len(dst.Content) {
			dst.Content = dst.Content[:len(src.Content)]
		}
	case yaml.ScalarNode, yaml.AliasNode:

		dst.Kind = src.Kind
		dst.Tag = src.Tag
		dst.Value = src.Value

	case 0:

	default:

		copyNodeShallow(dst, src)
	}
}

func findMapKeyIndex(mapNode *yaml.Node, key string) int {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i] != nil && mapNode.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func appendPath(path []string, key string) []string {
	if len(path) == 0 {
		return []string{key}
	}
	newPath := make([]string, len(path)+1)
	copy(newPath, path)
	newPath[len(path)] = key
	return newPath
}

func isSensitiveWordsReplaceEntryPath(path []string) bool {
	for i, part := range path {
		if part == "sensitive-words-replace" && i < len(path)-1 {
			return true
		}
	}
	return false
}

func hasNonEmptySensitiveWordsReplace(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			k := node.Content[i]
			v := node.Content[i+1]
			if k != nil && k.Value == "sensitive-words-replace" && v != nil && v.Kind == yaml.MappingNode && len(v.Content) > 0 {
				return true
			}
			if hasNonEmptySensitiveWordsReplace(v) {
				return true
			}
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if hasNonEmptySensitiveWordsReplace(child) {
				return true
			}
		}
	}
	return false
}

func isKnownDefaultValue(path []string, node *yaml.Node) bool {
	if isPluginConfigsSubtreePath(path) || isSensitiveWordsReplaceEntryPath(path) {
		return false
	}
	if len(path) > 0 && path[len(path)-1] == "sensitive-words-replace" && node != nil && node.Kind == yaml.MappingNode && len(node.Content) > 0 {
		return false
	}
	if hasNonEmptySensitiveWordsReplace(node) {
		return false
	}
	if len(path) == 1 && path[0] == "plugins" && node != nil && node.Kind == yaml.MappingNode {
		configsIdx := findMapKeyIndex(node, "configs")
		if configsIdx >= 0 && configsIdx+1 < len(node.Content) {
			configsNode := node.Content[configsIdx+1]
			if configsNode != nil && configsNode.Kind == yaml.MappingNode && len(configsNode.Content) > 0 {
				return false
			}
		}
	}

	if len(path) > 0 && (path[len(path)-1] == "weight" || path[len(path)-1] == "request-retry") && node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!int" {
		return false
	}

	if len(path) > 0 && path[len(path)-1] == "session-date-pin-days" && node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!int" && node.Value == "0" {
		return false
	}

	if len(path) > 0 && (path[len(path)-1] == "cache-user-id" || path[len(path)-1] == "disable-cooling") && node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		return false
	}

	if node != nil && node.Kind == yaml.MappingNode {
		if pinIdx := findMapKeyIndex(node, "session-date-pin-days"); pinIdx >= 0 {
			return false
		}
	}

	if isZeroValueNode(node) {
		return true
	}

	if len(path) == 0 {
		return false
	}

	fullPath := strings.Join(path, ".")

	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		switch fullPath {
		case "pprof.addr":
			return node.Value == DefaultPprofAddr
		case "remote-management.panel-github-repository":
			return node.Value == DefaultPanelGitHubRepository
		case "plugins.dir":
			return node.Value == "plugins"
		case "routing.strategy":
			return node.Value == "round-robin"
		}
	}

	if node.Kind == yaml.ScalarNode && node.Tag == "!!int" {
		switch fullPath {
		case "error-logs-max-files":
			return node.Value == "10"
		case "claude.session-date-pin-days":
			return node.Value == strconv.Itoa(DefaultClaudeSessionDatePinDays)
		}
	}

	return false
}

func pruneKnownDefaultsInNewNode(path []string, node *yaml.Node) {
	if node == nil || isPluginConfigsSubtreePath(path) || isSensitiveWordsReplaceEntryPath(path) {
		return
	}
	if len(path) > 0 && path[len(path)-1] == "sensitive-words-replace" {
		return
	}

	switch node.Kind {
	case yaml.MappingNode:
		filtered := make([]*yaml.Node, 0, len(node.Content))
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			if keyNode == nil || valueNode == nil {
				continue
			}

			childPath := appendPath(path, keyNode.Value)
			if isKnownDefaultValue(childPath, valueNode) {
				continue
			}

			pruneKnownDefaultsInNewNode(childPath, valueNode)
			if (valueNode.Kind == yaml.MappingNode || valueNode.Kind == yaml.SequenceNode) &&
				len(valueNode.Content) == 0 {
				continue
			}

			filtered = append(filtered, keyNode, valueNode)
		}
		node.Content = filtered
	case yaml.SequenceNode:
		for _, child := range node.Content {
			pruneKnownDefaultsInNewNode(path, child)
		}
	}
}

func isZeroValueNode(node *yaml.Node) bool {
	if node == nil {
		return true
	}
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!bool":
			return node.Value == "false"
		case "!!int", "!!float":
			return node.Value == "0" || node.Value == "0.0"
		case "!!str":
			return node.Value == ""
		case "!!null":
			return true
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return true
		}

		for _, child := range node.Content {
			if !isZeroValueNode(child) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			return true
		}

		for i := 1; i < len(node.Content); i += 2 {
			if !isZeroValueNode(node.Content[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func deepCopyNode(n *yaml.Node) *yaml.Node {
	return deepCopyNodeSeen(n, map[*yaml.Node]*yaml.Node{})
}

func deepCopyNodeSeen(n *yaml.Node, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if cp, ok := seen[n]; ok {
		return cp
	}
	cp := *n
	seen[n] = &cp
	if n.Alias != nil {
		cp.Alias = deepCopyNodeSeen(n.Alias, seen)
	}
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i := range n.Content {
			cp.Content[i] = deepCopyNodeSeen(n.Content[i], seen)
		}
	}
	return &cp
}

func copyNodeShallow(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	dst.Kind = src.Kind
	dst.Tag = src.Tag
	dst.Value = src.Value

	if len(src.Content) > 0 {
		dst.Content = make([]*yaml.Node, len(src.Content))
		for i := range src.Content {
			dst.Content[i] = deepCopyNode(src.Content[i])
		}
	} else {
		dst.Content = nil
	}
}

func reorderSequenceForMerge(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}
	if len(dst.Content) == 0 {
		return
	}
	if len(src.Content) == 0 {
		return
	}
	original := append([]*yaml.Node(nil), dst.Content...)
	used := make([]bool, len(original))
	ordered := make([]*yaml.Node, len(src.Content))
	for i := range src.Content {
		if idx := matchSequenceElement(original, used, src.Content[i]); idx >= 0 {
			ordered[i] = original[idx]
			used[idx] = true
		}
	}
	dst.Content = ordered
}

func matchSequenceElement(original []*yaml.Node, used []bool, target *yaml.Node) int {
	if target == nil {
		return -1
	}
	switch target.Kind {
	case yaml.MappingNode:
		id := sequenceElementIdentity(target)
		if id != "" {
			for i := range original {
				if used[i] || original[i] == nil || original[i].Kind != yaml.MappingNode {
					continue
				}
				if sequenceElementIdentity(original[i]) == id {
					return i
				}
			}
		}
	case yaml.ScalarNode:
		val := strings.TrimSpace(target.Value)
		if val != "" {
			for i := range original {
				if used[i] || original[i] == nil || original[i].Kind != yaml.ScalarNode {
					continue
				}
				if strings.TrimSpace(original[i].Value) == val {
					return i
				}
			}
		}
	default:
	}

	for i := range original {
		if used[i] || original[i] == nil {
			continue
		}
		if nodesStructurallyEqual(original[i], target) {
			return i
		}
	}
	return -1
}

func sequenceElementIdentity(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	identityKeys := []string{"id", "name", "alias", "api-key", "api_key", "apikey", "key", "provider", "model"}
	for _, k := range identityKeys {
		if v := mappingScalarValue(node, k); v != "" {
			return k + "=" + v
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode == nil || valNode == nil || valNode.Kind != yaml.ScalarNode {
			continue
		}
		val := strings.TrimSpace(valNode.Value)
		if val != "" {
			return strings.ToLower(strings.TrimSpace(keyNode.Value)) + "=" + val
		}
	}
	return ""
}

func mappingScalarValue(node *yaml.Node, key string) string {
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	lowerKey := strings.ToLower(key)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode == nil || valNode == nil || valNode.Kind != yaml.ScalarNode {
			continue
		}
		if strings.ToLower(strings.TrimSpace(keyNode.Value)) == lowerKey {
			return strings.TrimSpace(valNode.Value)
		}
	}
	return ""
}

func nodesStructurallyEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			if !nodesStructurallyEqual(a.Content[i], b.Content[i]) {
				return false
			}
			if !nodesStructurallyEqual(a.Content[i+1], b.Content[i+1]) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !nodesStructurallyEqual(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	case yaml.ScalarNode:
		return strings.TrimSpace(a.Value) == strings.TrimSpace(b.Value)
	case yaml.AliasNode:
		return nodesStructurallyEqual(a.Alias, b.Alias)
	default:
		return strings.TrimSpace(a.Value) == strings.TrimSpace(b.Value)
	}
}

func removeMapKey(mapNode *yaml.Node, key string) {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode || key == "" {
		return
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i] != nil && mapNode.Content[i].Value == key {
			mapNode.Content = append(mapNode.Content[:i], mapNode.Content[i+2:]...)
			return
		}
	}
}

func pruneMappingToGeneratedKeys(dstRoot, srcRoot *yaml.Node, keyPath ...string) {
	if len(keyPath) == 0 || dstRoot == nil || srcRoot == nil {
		return
	}
	if len(keyPath) > 1 {
		dstParent := dstRoot
		srcParent := srcRoot
		for _, key := range keyPath[:len(keyPath)-1] {
			if key == "" || dstParent == nil || dstParent.Kind != yaml.MappingNode {
				return
			}
			dstIdx := findMapKeyIndex(dstParent, key)
			if dstIdx < 0 || dstIdx+1 >= len(dstParent.Content) {
				return
			}
			dstParent = dstParent.Content[dstIdx+1]

			if srcParent != nil && srcParent.Kind == yaml.MappingNode {
				srcIdx := findMapKeyIndex(srcParent, key)
				if srcIdx >= 0 && srcIdx+1 < len(srcParent.Content) {
					srcParent = srcParent.Content[srcIdx+1]
				} else {
					srcParent = nil
				}
			}
		}
		if srcParent == nil || srcParent.Kind != yaml.MappingNode {
			removeMapKey(dstParent, keyPath[len(keyPath)-1])
			return
		}
		pruneMappingToGeneratedKeys(dstParent, srcParent, keyPath[len(keyPath)-1])
		return
	}
	key := keyPath[0]
	if key == "" {
		return
	}
	if dstRoot.Kind != yaml.MappingNode || srcRoot.Kind != yaml.MappingNode {
		return
	}
	dstIdx := findMapKeyIndex(dstRoot, key)
	if dstIdx < 0 || dstIdx+1 >= len(dstRoot.Content) {
		return
	}
	srcIdx := findMapKeyIndex(srcRoot, key)
	if srcIdx < 0 {

		if key == "oauth-excluded-models" || key == "oauth-model-alias" || key == "oauth-request-scoped-errors" || key == "oauth-settings" {
			dstRoot.Content[dstIdx+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			return
		}
		removeMapKey(dstRoot, key)
		return
	}
	if srcIdx+1 >= len(srcRoot.Content) {
		return
	}
	srcVal := srcRoot.Content[srcIdx+1]
	dstVal := dstRoot.Content[dstIdx+1]
	if srcVal == nil {
		dstRoot.Content[dstIdx+1] = nil
		return
	}
	if srcVal.Kind != yaml.MappingNode {
		dstRoot.Content[dstIdx+1] = deepCopyNode(srcVal)
		return
	}
	if dstVal == nil || dstVal.Kind != yaml.MappingNode {
		dstRoot.Content[dstIdx+1] = deepCopyNode(srcVal)
		return
	}
	pruneMissingMapKeys(dstVal, srcVal)
}

func pruneMissingMapKeys(dstMap, srcMap *yaml.Node) {
	if dstMap == nil || srcMap == nil || dstMap.Kind != yaml.MappingNode || srcMap.Kind != yaml.MappingNode {
		return
	}
	keep := make(map[string]struct{}, len(srcMap.Content)/2)
	for i := 0; i+1 < len(srcMap.Content); i += 2 {
		keyNode := srcMap.Content[i]
		if keyNode == nil {
			continue
		}
		key := strings.TrimSpace(keyNode.Value)
		if key == "" {
			continue
		}
		keep[key] = struct{}{}
	}
	for i := 0; i+1 < len(dstMap.Content); {
		keyNode := dstMap.Content[i]
		if keyNode == nil {
			i += 2
			continue
		}
		key := strings.TrimSpace(keyNode.Value)
		if _, ok := keep[key]; !ok {
			dstMap.Content = append(dstMap.Content[:i], dstMap.Content[i+2:]...)
			continue
		}
		i += 2
	}
}

func shouldPruneNestedMappingKeys(path []string) bool {
	if len(path) < 2 {
		return false
	}
	parent := path[len(path)-2]
	last := path[len(path)-1]
	if last == "sensitive-words-replace" && (parent == "antigravity" || parent == "devin" || parent == "cloak") {
		return true
	}
	switch parent {
	case "claude-api-key":
		return last == "cloak" || last == "headers"
	case "codex-api-key", "gemini-api-key", "interactions-api-key", "xai-api-key", "meta-api-key", "vertex-api-key", "openai-compatibility":
		return last == "headers"
	default:
		return false
	}
}

func NormalizeCollectionNodeStyles(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for i := range node.Content {
			NormalizeCollectionNodeStyles(node.Content[i])
		}
	case yaml.MappingNode:
		node.Style = 0
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key != nil && key.Kind == yaml.ScalarNode && key.Tag == "!!str" && key.Style == yaml.DoubleQuotedStyle {
				key.Style = 0
			}
			NormalizeCollectionNodeStyles(key)
			if i+1 < len(node.Content) {
				NormalizeCollectionNodeStyles(node.Content[i+1])
			}
		}
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			node.Style = yaml.FlowStyle
		} else {
			node.Style = 0
		}
		for i := range node.Content {
			NormalizeCollectionNodeStyles(node.Content[i])
		}
	default:

	}
}

func removeLegacyOpenAICompatAPIKeys(root *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	idx := findMapKeyIndex(root, "openai-compatibility")
	if idx < 0 || idx+1 >= len(root.Content) {
		return
	}
	seq := root.Content[idx+1]
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	for i := range seq.Content {
		if seq.Content[i] != nil && seq.Content[i].Kind == yaml.MappingNode {
			removeMapKey(seq.Content[i], "api-keys")
		}
	}
}

func removeRemovedIntegrationKeys(root *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	removeMapKey(root, "ampcode")
	removeMapKey(root, "amp-upstream-url")
	removeMapKey(root, "amp-upstream-api-key")
	removeMapKey(root, "amp-restrict-management-to-localhost")
	removeMapKey(root, "amp-model-mappings")
}

func removeLegacyGenerativeLanguageKeys(root *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	removeMapKey(root, "generative-language-api-key")
}

func removeLegacyAuthBlock(root *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode {
		return
	}
	removeMapKey(root, "auth")
}

func isPluginConfigsPath(path []string) bool {
	return len(path) == 2 && path[0] == "plugins" && path[1] == "configs"
}

func isPluginConfigsSubtreePath(path []string) bool {
	return len(path) >= 2 && path[0] == "plugins" && path[1] == "configs"
}

func replacePluginConfigsSubtree(dstRoot, srcRoot *yaml.Node) {
	if dstRoot == nil || srcRoot == nil || dstRoot.Kind != yaml.MappingNode || srcRoot.Kind != yaml.MappingNode {
		return
	}

	srcPluginsIdx := findMapKeyIndex(srcRoot, "plugins")
	var srcConfigs *yaml.Node
	if srcPluginsIdx >= 0 && srcPluginsIdx+1 < len(srcRoot.Content) {
		srcPlugins := srcRoot.Content[srcPluginsIdx+1]
		if srcPlugins != nil && srcPlugins.Kind == yaml.MappingNode {
			srcConfigsIdx := findMapKeyIndex(srcPlugins, "configs")
			if srcConfigsIdx >= 0 && srcConfigsIdx+1 < len(srcPlugins.Content) {
				srcConfigs = srcPlugins.Content[srcConfigsIdx+1]
			}
		}
	}

	dstPluginsIdx := findMapKeyIndex(dstRoot, "plugins")
	if srcConfigs == nil || srcConfigs.Kind != yaml.MappingNode || len(srcConfigs.Content) == 0 {
		if dstPluginsIdx >= 0 && dstPluginsIdx+1 < len(dstRoot.Content) {
			dstPlugins := dstRoot.Content[dstPluginsIdx+1]
			if dstPlugins != nil && dstPlugins.Kind == yaml.MappingNode {
				removeMapKey(dstPlugins, "configs")
			}
		}
		return
	}

	copiedConfigs := deepCopyNode(srcConfigs)
	if dstPluginsIdx < 0 {
		dstPlugins := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		dstPlugins.Content = append(dstPlugins.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "configs"},
			copiedConfigs,
		)
		dstRoot.Content = append(dstRoot.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugins"},
			dstPlugins,
		)
		return
	}

	if dstPluginsIdx+1 >= len(dstRoot.Content) {
		return
	}
	dstPlugins := dstRoot.Content[dstPluginsIdx+1]
	if dstPlugins == nil || dstPlugins.Kind != yaml.MappingNode {
		return
	}
	dstConfigsIdx := findMapKeyIndex(dstPlugins, "configs")
	if dstConfigsIdx >= 0 && dstConfigsIdx+1 < len(dstPlugins.Content) {
		if dstPlugins.Content[dstConfigsIdx+1] != nil {
			if copiedConfigs.HeadComment == "" {
				copiedConfigs.HeadComment = dstPlugins.Content[dstConfigsIdx+1].HeadComment
			}
			if copiedConfigs.LineComment == "" {
				copiedConfigs.LineComment = dstPlugins.Content[dstConfigsIdx+1].LineComment
			}
			if copiedConfigs.FootComment == "" {
				copiedConfigs.FootComment = dstPlugins.Content[dstConfigsIdx+1].FootComment
			}
		}
		dstPlugins.Content[dstConfigsIdx+1] = copiedConfigs
	} else {
		dstPlugins.Content = append(dstPlugins.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "configs"},
			copiedConfigs,
		)
	}
}
