package executor

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	antigravityclaude "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/antigravity/claude"
	antigravitychat "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/antigravity/openai/chat-completions"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

const sanitizeTestPayload = `{
  "request": {
    "contents": [
      {"role": "model", "parts": [{"functionCall": {"name": "manage_todo_list", "args": {
        "operation": "write",
        "todoList": [
          {"id": 1, "title": "output 1", "description": "d1", "status": "not-started"},
          {"id": 2, "title": "output 2", "description": "d2", "status": "not-started"}
        ]}}}]},
      {"role": "model", "parts": [{"functionCall": {"name": "write_file", "args": {
        "path": "a.md", "format": "markdown", "default": "x", "pattern": "p",
        "const": "c", "deprecated": false, "nullable": "n", "examples": "e",
        "additionalProperties": "ap", "x-custom": "keepme"
      }}}]}
    ],
    "tools": [{"functionDeclarations": [{
      "name": "manage_todo_list",
      "parametersJsonSchema": {
        "type": "object",
        "required": ["todoList"],
        "properties": {"todoList": {"type": "array", "items": {
          "type": "object",
          "required": ["id", "title"],
          "title": "TodoItem",
          "properties": {"id": {"type": "number"}, "title": {"type": "string", "minLength": 3}}
        }}}
      }
    }]}]
  }
}`




func TestSanitizeAntigravityRequestSchemasPreservesHistory(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		useAntigravitySchema bool
	}{
		{"gemini", false},
		{"antigravity", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeAntigravityRequestSchemas(sanitizeTestPayload, tc.useAntigravitySchema)

			before := ojson.Get(sanitizeTestPayload, "request.contents")
			after := ojson.Get(got, "request.contents")
			if before.Raw != after.Raw {
				t.Errorf("conversation history was mutated.\nbefore: %s\nafter:  %s", before.Raw, after.Raw)
			}

			todo := ojson.Get(got, `request.contents.0.parts.0.functionCall.args.todoList`)
			for i, item := range todo.Array() {
				if !item.Get("title").Exists() {
					t.Errorf("todoList[%d] lost its title: %s", i, item.Raw)
				}
			}

			args := ojson.Get(got, `request.contents.1.parts.0.functionCall.args`)
			for _, key := range []string{"format", "default", "pattern", "const", "deprecated", "examples", "additionalProperties", "x-custom"} {
				if !args.Get(ojson.Escape(key)).Exists() {
					t.Errorf("argument key %q was stripped from history: %s", key, args.Raw)
				}
			}
			if args.Get("enum").Exists() {
				t.Errorf("cleaner fabricated an enum key in history args: %s", args.Raw)
			}
		})
	}
}



func TestSanitizeAntigravityRequestSchemasStillCleansSchemas(t *testing.T) {
	got := sanitizeAntigravityRequestSchemas(sanitizeTestPayload, false)

	decl := "request.tools.0.functionDeclarations.0"
	if ojson.Get(got, decl+".parametersJsonSchema").Exists() {
		t.Errorf("parametersJsonSchema was not renamed: %s", ojson.Get(got, decl).Raw)
	}
	schema := ojson.Get(got, decl+".parameters")
	if !schema.Exists() {
		t.Fatalf("parameters missing after sanitization: %s", ojson.Get(got, decl).Raw)
	}

	items := schema.Get("properties.todoList.items")
	if items.Get("title").Exists() {
		t.Errorf("schema keyword title was not removed: %s", items.Raw)
	}
	if items.Get("properties.title.minLength").Exists() {
		t.Errorf("unsupported keyword minLength was not removed: %s", items.Raw)
	}
	if !items.Get("properties.title").Exists() {
		t.Errorf("schema property named title must be preserved: %s", items.Raw)
	}
	if req := items.Get("required").Array(); len(req) != 2 {
		t.Errorf("required list should keep id and title, got: %s", items.Get("required").Raw)
	}
}



func TestSanitizeAntigravityRequestSchemasCleansResultSchemas(t *testing.T) {
	payload := `{"request": {"tools": [{"functionDeclarations": [{
	  "name": "t",
	  "parameters": {"type": "object", "$id": "drop-a", "properties": {"a": {"type": "string"}}},
	  "response": {"type": "object", "$comment": "drop-b", "properties": {"b": {"type": "string"}}},
	  "responseJsonSchema": {"type": "object", "$id": "drop-c", "properties": {"c": {"type": "string"}}}
	}]}]}}`

	got := sanitizeAntigravityRequestSchemas(payload, false)
	decl := ojson.Get(got, "request.tools.0.functionDeclarations.0")

	for _, unsupported := range []string{`parameters.\$id`, `response.\$comment`, `responseJsonSchema.\$id`} {
		if decl.Get(unsupported).Exists() {
			t.Errorf("unsupported keyword %s survived cleaning: %s", unsupported, decl.Raw)
		}
	}
	for _, kept := range []string{"parameters.properties.a", "response.properties.b", "responseJsonSchema.properties.c"} {
		if !decl.Get(kept).Exists() {
			t.Errorf("%s should be preserved: %s", kept, decl.Raw)
		}
	}
}











func TestAntigravitySchemaPathsCoverEverySchemaLocation(t *testing.T) {
	const schema = `{"type":"object","$id":"drop","properties":{"a":{"type":"string"}}}`



	for _, declContainer := range []string{"functionDeclarations", "function_declarations"} {
		for _, genContainer := range antigravityGenerationConfigContainers {
			t.Run(declContainer+"_"+strings.TrimPrefix(genContainer, "request."), func(t *testing.T) {
				decl := `"name":"t"`
				for _, k := range antigravityDeclarationSchemaKeys {
					decl += `,"` + k + `":` + schema
				}
				gen := ""
				for i, k := range antigravityGenerationSchemaKeys {
					if i > 0 {
						gen += ","
					}
					gen += `"` + k + `":` + schema
				}
				payload := `{"request":{"tools":[{"` + declContainer + `":[{` + decl + `}]}],"` +
					strings.TrimPrefix(genContainer, "request.") + `":{` + gen + `}}}`

				if !antigravityRequestNeedsSchemaSanitization([]byte(payload)) {
					t.Fatal("sanitization must trigger for a payload carrying schemas")
				}
				got := sanitizeAntigravityRequestSchemas(payload, false)

				check := func(path string) {
					t.Helper()
					node := ojson.Get(got, path)
					if !node.Exists() {
						t.Errorf("%s disappeared: %s", path, got)
						return
					}
					if node.Get(`\$id`).Exists() {
						t.Errorf("%s was never cleaned, $id reaches upstream: %s", path, node.Raw)
					}
				}
				base := "request.tools.0." + declContainer + ".0."
				for _, k := range antigravityDeclarationSchemaKeys {


					if k == "parametersJsonSchema" {
						if ojson.Get(got, base+k).Exists() {
							t.Errorf("%s should have been renamed onto parameters: %s", k, got)
						}
						continue
					}
					check(base + k)
				}
				for _, k := range antigravityGenerationSchemaKeys {
					check(genContainer + "." + k)
				}
			})
		}
	}
}





func TestSanitizeAntigravityRequestSchemasMatchesWholePayloadCleaning(t *testing.T) {
	shapes := map[string]string{
		"optionalOnly": `{"type":"object","properties":{"flag":{"type":"string"}}}`,
		"emptyProps":   `{"type":"object","properties":{}}`,
		"noProps":      `{"type":"object"}`,
		"withRequired": `{"type":"object","required":["a"],"properties":{"a":{"type":"string","minLength":2}}}`,
		"nestedArray":  `{"type":"object","properties":{"list":{"type":"array","items":{"type":"object","title":"X","required":["id","title"],"properties":{"id":{"type":"number"},"title":{"type":"string"}}}}}}`,
		"enumAndRemoved": `{"type":"object","$comment":"c","properties":{"m":{"type":"string","enum":["a","b"],` +
			`"deprecated":true}}}`,
	}
	const schemaPath = "request.tools.0.functionDeclarations.0.parameters"

	for _, useAntigravitySchema := range []bool{false, true} {
		for name, schema := range shapes {
			doc := `{"request":{"tools":[{"functionDeclarations":[{"name":"t","parameters":` + schema + `}]}]}}`
			whole := util.CleanJSONSchemaForAntigravityTool(doc, useAntigravitySchema)
			want := ojson.Get(whole, schemaPath).Raw
			got := ojson.Get(sanitizeAntigravityRequestSchemas(doc, useAntigravitySchema), schemaPath).Raw
			if want != got {
				t.Errorf("%s (antigravity=%v) diverged from whole-payload cleaning.\nwant: %s\ngot:  %s",
					name, useAntigravitySchema, want, got)
			}
		}
	}


	doc := `{"request":{"tools":[{"functionDeclarations":[{"name":"t","parameters":` + shapes["optionalOnly"] + `}]}]}}`
	got := ojson.Get(sanitizeAntigravityRequestSchemas(doc, true), schemaPath)
	if req := got.Get("required").Array(); len(req) != 1 || req[0].String() != "_" {
		t.Errorf("Claude VALIDATED placeholder missing for an optional-only schema: %s", got.Raw)
	}
}

func TestSanitizeAntigravityRequestSchemasKeepsResponseSchemasPlaceholderFree(t *testing.T) {
	payload := `{"request":{
		"tools":[{"functionDeclarations":[{"name":"tool","parameters":{"type":"object","properties":{"value":{"type":"string"}}}}]}],
		"generationConfig":{"responseSchema":{"type":"object","properties":{
			"empty":{"type":"object"},
			"optional":{"type":"object","properties":{"value":{"type":"string"}}}
		}}}
	}}`

	got := sanitizeAntigravityRequestSchemas(payload, true)
	toolSchema := ojson.Get(got, "request.tools.0.functionDeclarations.0.parameters")
	if required := toolSchema.Get("required.0").String(); required != "_" {
		t.Fatalf("tool schema lost VALIDATED placeholder, required[0] = %q: %s", required, got)
	}

	responseSchema := ojson.Get(got, "request.generationConfig.responseSchema")
	for _, path := range []string{
		"required",
		"properties._",
		"properties.reason",
		"properties.empty.required",
		"properties.empty.properties.reason",
		"properties.optional.required",
		"properties.optional.properties._",
	} {
		if responseSchema.Get(path).Exists() {
			t.Errorf("response schema gained tool-only field %s: %s", path, responseSchema.Raw)
		}
	}
}

func TestSanitizeAntigravityRequestSchemasProjectsUnionsAndPreservesEnumTypes(t *testing.T) {
	payload := `{"request":{
		"tools":[{"functionDeclarations":[{"name":"tool","parameters":{"type":"object","properties":{
			"choice":{"anyOf":[{"type":"string"},{"type":"null"}]},
			"item":{"anyOf":[
				{"type":"string"},
				{"type":"object","properties":{"image":{"type":"string"}},"required":["image"]},
				{"type":"object","properties":{"audio":{"type":"string"}},"required":["audio"]}
			]},
			"variant":{"oneOf":[
				{"type":"object","properties":{"image":{"type":"string"}},"required":["image"]},
				{"type":"object","properties":{"audio":{"type":"string"}},"required":["audio"]}
			]},
			"level":{"type":"number","enum":[1,2]}
		}}}]}],
		"generationConfig":{"responseSchema":{"type":"object","properties":{
			"action":{"anyOf":[
				{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]},
				{"type":"null"}
			]},
			"conviction":{"type":"number","enum":[0.25,0.5,1]}
		}}}
	}}`

	got := sanitizeAntigravityRequestSchemas(payload, true)
	responseSchema := ojson.Get(got, "request.generationConfig.responseSchema")
	action := responseSchema.Get("properties.action")
	if action.Get("anyOf").Exists() || action.Get("type").String() != "object" || !action.Get("nullable").Bool() {
		t.Errorf("response anyOf was not projected to nullable object: %s", responseSchema.Raw)
	}
	conviction := responseSchema.Get("properties.conviction")
	if gotType := conviction.Get("type").String(); gotType != "number" {
		t.Errorf("response enum type = %q, want number: %s", gotType, responseSchema.Raw)
	}
	for _, enumValue := range conviction.Get("enum").Array() {
		if enumValue.Type != ojson.String {
			t.Errorf("response enum value is not a string: %s", conviction.Raw)
		}
	}

	toolSchema := ojson.Get(got, "request.tools.0.functionDeclarations.0.parameters")
	if toolSchema.Get("properties.choice.anyOf").Exists() {
		t.Errorf("tool nullable single-branch anyOf union was not flattened: %s", toolSchema.Raw)
	}
	if gotBranches := len(toolSchema.Get("properties.item.anyOf").Array()); gotBranches != 3 {
		t.Errorf("tool multi-branch anyOf count = %d, want 3: %s", gotBranches, toolSchema.Raw)
	}
	if !toolSchema.Get("properties.item.anyOf.2.properties.audio").Exists() {
		t.Errorf("tool multi-branch anyOf lost audio branch: %s", toolSchema.Raw)
	}
	if toolSchema.Get("properties.variant.oneOf").Exists() {
		t.Errorf("tool multi-branch oneOf should be converted to anyOf for parameters Schema carrier: %s", toolSchema.Raw)
	}
	if gotVariantBranches := len(toolSchema.Get("properties.variant.anyOf").Array()); gotVariantBranches != 2 {
		t.Errorf("tool multi-branch variant.anyOf count = %d, want 2: %s", gotVariantBranches, toolSchema.Raw)
	}
	if !toolSchema.Get("properties.variant.anyOf.1.properties.audio").Exists() {
		t.Errorf("tool multi-branch variant.anyOf lost audio branch: %s", toolSchema.Raw)
	}
	if gotType := toolSchema.Get("properties.level.type").String(); gotType != "number" {
		t.Errorf("tool enum type = %q, want number: %s", gotType, toolSchema.Raw)
	}
}

func TestSanitizeAntigravityToolSchemasKeepNativeTypeAndNullableOnBothPaths(t *testing.T) {
	payload := `{"request":{"tools":[{"functionDeclarations":[{"name":"tool","parameters":{
		"type":"object",
		"properties":{
			"level":{"type":"number","enum":[1,2]},
			"note":{"type":["string","null"]}
		},
		"required":["level","note"]
	}}]}]}}`

	for _, requirePlaceholder := range []bool{false, true} {
		got := sanitizeAntigravityRequestSchemas(payload, requirePlaceholder)
		schema := ojson.Get(got, "request.tools.0.functionDeclarations.0.parameters")
		if schema.Get("properties.level.type").String() != "number" {
			t.Fatalf("placeholder=%v changed numeric tool argument type: %s", requirePlaceholder, schema.Raw)
		}
		for _, member := range schema.Get("properties.level.enum").Array() {
			if member.Type != ojson.String {
				t.Fatalf("placeholder=%v left non-string proto enum: %s", requirePlaceholder, schema.Raw)
			}
		}
		if !schema.Get("properties.note.nullable").Bool() || schema.Get("required.1").String() != "note" {
			t.Fatalf("placeholder=%v lost native nullable/required semantics: %s", requirePlaceholder, schema.Raw)
		}
	}
}

func TestAntigravityBuildRequestKeepsJSONObjectMimeOnly(t *testing.T) {
	input := []byte(`{"model":"gemini-3.1-pro-low","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`)
	translated, _ := antigravitychat.ConvertOpenAIRequestToAntigravity("gemini-3.1-pro-low", input, false)
	body := buildRequestBodyFromRawPayload(t, "gemini-3.1-pro-low", translated)
	encoded, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}

	generationConfig := ojson.GetBytes(encoded, "request.generationConfig")
	if got := generationConfig.Get("responseMimeType").String(); got != "application/json" {
		t.Fatalf("responseMimeType = %q, want application/json: %s", got, encoded)
	}
	if generationConfig.Get("responseSchema").Exists() {
		t.Fatalf("responseSchema should not be set for json_object: %s", encoded)
	}
}

func TestAntigravityBuildRequestPreservesGenerationResponseSchemaMetadata(t *testing.T) {
	payload := []byte(`{"request":{"generationConfig":{"responseSchema":{
		"type":"object",
		"nullable":true,
		"properties":{"_":{"type":"string","nullable":true}},
		"required":["_"]
	}}}}`)

	for _, modelName := range []string{"gemini-3.6-flash-high", "gemini-3.1-pro-low"} {
		t.Run(modelName, func(t *testing.T) {
			body := buildRequestBodyFromRawPayload(t, modelName, payload)
			encoded, errMarshal := json.Marshal(body)
			if errMarshal != nil {
				t.Fatal(errMarshal)
			}

			schema := ojson.GetBytes(encoded, "request.generationConfig.responseSchema")
			if !schema.Get("nullable").Bool() || !schema.Get("properties._.nullable").Bool() {
				t.Fatalf("response schema nullable metadata was removed: %s", schema.Raw)
			}
			if !schema.Get("properties._").Exists() {
				t.Fatalf("legitimate underscore property was removed: %s", schema.Raw)
			}
			if required := schema.Get("required.0").String(); required != "_" {
				t.Fatalf("required[0] = %q, want underscore: %s", required, schema.Raw)
			}
		})
	}
}

func TestAntigravityBuildRequestSanitizesSnakeCaseGenerationResponseSchemas(t *testing.T) {
	for _, testCase := range []struct {
		alias     string
		canonical string
	}{
		{alias: "response_schema", canonical: "responseSchema"},
		{alias: "response_json_schema", canonical: "responseJsonSchema"},
	} {
		t.Run(testCase.alias, func(t *testing.T) {
			input := []byte(`{"model":"gemini-3.6-flash-high","messages":[{"role":"user","content":"hi"}],"generation_config":{"` + testCase.alias + `":{"type":"object","$id":"drop-me","properties":{"title":{"type":"string"}}}}}`)
			translated, _ := antigravitychat.ConvertOpenAIRequestToAntigravity("gemini-3.6-flash-high", input, false)
			body := buildRequestBodyFromRawPayload(t, "gemini-3.6-flash-high", translated)
			encoded, errMarshal := json.Marshal(body)
			if errMarshal != nil {
				t.Fatal(errMarshal)
			}

			base := "request.generationConfig."


			schema := ojson.GetBytes(encoded, base+testCase.alias)
			if !schema.Exists() {
				t.Fatalf("snake_case response schema was renamed or dropped: %s", encoded)
			}
			if ojson.GetBytes(encoded, base+testCase.canonical).Exists() {
				t.Fatalf("cleaning must not add a second spelling: %s", encoded)
			}
			if schema.Get(`\$id`).Exists() {
				t.Fatalf("unsupported $id survived cleaning: %s", schema.Raw)
			}
			if !schema.Get("properties.title").Exists() {
				t.Fatalf("schema property named title was removed: %s", schema.Raw)
			}
		})
	}
}



func TestSanitizeAntigravityRequestSchemasCleansBothSpellingsInPlace(t *testing.T) {
	payload := `{"request":{"generationConfig":{` +
		`"responseSchema":{"type":"object","$id":"drop-a","properties":{"canonical":{"type":"string"}}},` +
		`"response_schema":{"type":"object","$id":"drop-b","properties":{"alias":{"type":"string"}}}}}}`

	got := sanitizeAntigravityRequestSchemas(payload, false)

	for path, prop := range map[string]string{
		"request.generationConfig.responseSchema":  "canonical",
		"request.generationConfig.response_schema": "alias",
	} {
		schema := ojson.Get(got, path)
		if !schema.Exists() {
			t.Errorf("%s was dropped: %s", path, got)
			continue
		}
		if schema.Get(`\$id`).Exists() {
			t.Errorf("%s kept unsupported $id: %s", path, schema.Raw)
		}
		if !schema.Get("properties." + prop).Exists() {
			t.Errorf("%s lost its property: %s", path, schema.Raw)
		}
	}
}



func TestSanitizeAntigravityRequestSchemasIsIdempotent(t *testing.T) {




	payload := `{"request": {"tools": [{"functionDeclarations": [{
	  "name": "manage_todo_list",
	  "parameters": {"type": "object", "properties": {
	    "withDesc": {"type": "string", "enum": ["write", "read"], "description": "pick one"},
	    "bare": {"type": "string", "enum": ["not-started", "in-progress", "completed"]},
	    "compound": {"type": "string", "enum": ["a", "b"], "minLength": 1}}}
	}]}]}}`

	once := sanitizeAntigravityRequestSchemas(payload, false)
	twice := sanitizeAntigravityRequestSchemas(once, false)

	base := "request.tools.0.functionDeclarations.0.parameters.properties."
	for _, prop := range []string{"withDesc", "bare", "compound"} {
		descPath := base + prop + ".description"
		first, second := ojson.Get(once, descPath).String(), ojson.Get(twice, descPath).String()
		if first != second {
			t.Errorf("%s: cleaning is not idempotent.\nonce:  %s\ntwice: %s", prop, first, second)
		}
		if strings.Count(second, "Allowed:") != 1 {
			t.Errorf("%s: hint duplicated: %s", prop, second)
		}
	}
}



var propertyNamesShapes = map[string]string{

	"arrayItem": `{"type":"object","properties":{"records":{"type":"array","items":{"type":"object",` +
		`"properties":{"name":{"type":"string"}},"propertyNames":{"type":"string"}}}}}`,

	"propertyNamedProperties": `{"type":"object","properties":{"properties":{"type":"object",` +
		`"propertyNames":{"type":"string"}}}}`,
}




func TestSanitizeAntigravityRequestSchemasStripsPropertyNamesEverywhere(t *testing.T) {
	for shapeName, schema := range propertyNamesShapes {
		for _, declContainer := range []string{"functionDeclarations", "function_declarations"} {
			for _, genContainer := range antigravityGenerationConfigContainers {
				name := shapeName + "_" + declContainer + "_" + strings.TrimPrefix(genContainer, "request.")
				t.Run(name, func(t *testing.T) {
					decl := `"name":"t"`
					for _, k := range antigravityDeclarationSchemaKeys {
						decl += `,"` + k + `":` + schema
					}
					gen := ""
					for i, k := range antigravityGenerationSchemaKeys {
						if i > 0 {
							gen += ","
						}
						gen += `"` + k + `":` + schema
					}
					payload := `{"request":{"tools":[{"` + declContainer + `":[{` + decl + `}]}],"` +
						strings.TrimPrefix(genContainer, "request.") + `":{` + gen + `}}}`

					for _, useAntigravitySchema := range []bool{false, true} {
						got := sanitizeAntigravityRequestSchemas(payload, useAntigravitySchema)
						if strings.Contains(got, `"propertyNames"`) {
							t.Errorf("antigravity=%v: propertyNames reaches upstream: %s", useAntigravitySchema, got)
						}
					}
				})
			}
		}
	}
}




func TestSanitizeAntigravityRequestSchemasKeepsPropertyNamesInHistory(t *testing.T) {
	payload := `{"request":{
		"contents":[{"role":"model","parts":[{"functionCall":{"name":"t","args":{
			"propertyNames":"keep-me",
			"properties":{"propertyNames":"keep-me-too"}
		}}}]}],
		"tools":[{"functionDeclarations":[{"name":"t","parameters":{"type":"object","properties":{
			"propertyNames":{"type":"string"},
			"properties":{"type":"object","propertyNames":{"type":"string"}}
		}}}]}]
	}}`

	for _, useAntigravitySchema := range []bool{false, true} {
		got := sanitizeAntigravityRequestSchemas(payload, useAntigravitySchema)

		before := ojson.Get(payload, "request.contents")
		after := ojson.Get(got, "request.contents")
		if before.Raw != after.Raw {
			t.Errorf("antigravity=%v: history was mutated.\nbefore: %s\nafter:  %s", useAntigravitySchema, before.Raw, after.Raw)
		}

		schema := ojson.Get(got, "request.tools.0.functionDeclarations.0.parameters")
		if !schema.Get("properties.propertyNames").Exists() {
			t.Errorf("antigravity=%v: property named propertyNames was removed: %s", useAntigravitySchema, schema.Raw)
		}
		if schema.Get("properties.properties.propertyNames").Exists() {
			t.Errorf("antigravity=%v: propertyNames keyword survived inside a property named properties: %s", useAntigravitySchema, schema.Raw)
		}
	}
}



func TestAntigravityBuildRequestStripsPropertyNamesFromOutboundBody(t *testing.T) {
	for shapeName, schema := range propertyNamesShapes {
		for _, modelName := range []string{"gemini-3.1-pro", "claude-opus-4-6"} {
			t.Run(shapeName+"_"+modelName, func(t *testing.T) {
				payload := []byte(`{"request":{
					"contents":[{"role":"model","parts":[{"functionCall":{"name":"t","args":{"propertyNames":"keep-me"}}}]}],
					"tools":[{"function_declarations":[{"name":"t","parametersJsonSchema":` + schema + `}]}],
					"generationConfig":{"responseSchema":` + schema + `}
				}}`)

				body := buildRequestBodyFromRawPayload(t, modelName, payload)
				encoded, errMarshal := json.Marshal(body)
				if errMarshal != nil {
					t.Fatal(errMarshal)
				}

				for _, path := range []string{"request.tools", "request.generationConfig"} {
					if node := ojson.GetBytes(encoded, path); strings.Contains(node.Raw, `"propertyNames"`) {
						t.Errorf("%s still carries propertyNames: %s", path, node.Raw)
					}
				}
				args := ojson.GetBytes(encoded, "request.contents.0.parts.0.functionCall.args")
				if args.Get("propertyNames").String() != "keep-me" {
					t.Errorf("functionCall argument named propertyNames was rewritten: %s", args.Raw)
				}
			})
		}
	}
}



func TestSanitizeAntigravityRequestSchemasStripsEncryptedMetadata(t *testing.T) {
	encryptedSchema := `{"type":"object","properties":{"key":{"type":"string","encrypted":true},"timeout":{"type":"integer","encrypted":false}},"required":["key"]}`

	for _, declContainer := range []string{"functionDeclarations", "function_declarations"} {
		payload := `{"request":{"tools":[{"` + declContainer + `":[{"name":"test_tool","parameters":` + encryptedSchema + `}]}]}}`

		for _, useAntigravitySchema := range []bool{false, true} {
			got := sanitizeAntigravityRequestSchemas(payload, useAntigravitySchema)
			if strings.Contains(got, `"encrypted"`) {
				t.Errorf("declContainer=%s antigravity=%v: 'encrypted' marker survived sanitization: %s", declContainer, useAntigravitySchema, got)
			}
			schema := ojson.Get(got, "request.tools.0."+declContainer+".0.parameters")
			if !schema.Get("properties.key.type").Exists() || schema.Get("properties.key.type").String() != "string" {
				t.Errorf("declContainer=%s antigravity=%v: key property was corrupted: %s", declContainer, useAntigravitySchema, schema.Raw)
			}
		}
	}
}

func TestSanitizeAntigravityRequestSchemasNormalizesTrueBooleanSubschemas(t *testing.T) {


	payload := `{"request":{
		"tools":[{"functionDeclarations":[{
			"name":"mcp__revenuecat_create_product_store_state_plan",
			"parametersJsonSchema":{
				"type":"object",
				"properties":{
					"screenshot_id":true,
					"filename":true,
					"file_size":true,
					"source_file_checksum":true,
					"disabled":false,
					"metadata":{
						"type":"object",
						"properties":{
							"nested_flag":true
						}
					},
					"tags":{
						"type":"array",
						"items":true
					},
					"variants":{
						"anyOf":[true,{"type":"string"}]
					},
					"large_int":9007199254740993
				},
				"additionalProperties":true
			}
		}]}]
	}}`

	for _, useAntigravitySchema := range []bool{false, true} {
		got := sanitizeAntigravityRequestSchemas(payload, useAntigravitySchema)
		decl := ojson.Get(got, "request.tools.0.functionDeclarations.0")
		params := decl.Get("parameters")
		if !params.Exists() {
			t.Fatalf("parameters missing in sanitized declaration: %s", decl.Raw)
		}

		for _, prop := range []string{"screenshot_id", "filename", "file_size", "source_file_checksum"} {
			val := params.Get("properties." + prop)
			if !val.Exists() {
				t.Errorf("useAntigravitySchema=%v: property %s missing: %s", useAntigravitySchema, prop, params.Raw)
			}
			if val.Type != ojson.JSON || val.Raw != "{}" {
				t.Errorf("useAntigravitySchema=%v: property %s should be {}, got %s", useAntigravitySchema, prop, val.Raw)
			}
		}


		if val := params.Get("properties.disabled"); val.Type != ojson.False {
			t.Errorf("useAntigravitySchema=%v: disabled should be false, got %s", useAntigravitySchema, val.Raw)
		}


		if val := params.Get("properties.metadata.properties.nested_flag"); val.Type != ojson.JSON || val.Raw != "{}" {
			t.Errorf("useAntigravitySchema=%v: nested_flag should be {}, got %s", useAntigravitySchema, val.Raw)
		}


		if val := params.Get("properties.tags.items"); val.Type != ojson.JSON || val.Raw != "{}" {
			t.Errorf("useAntigravitySchema=%v: tags.items should be {}, got %s", useAntigravitySchema, val.Raw)
		}


		if val := params.Get("properties.large_int"); val.Raw != "9007199254740993" {
			t.Errorf("useAntigravitySchema=%v: large_int corrupted, got %s", useAntigravitySchema, val.Raw)
		}
	}
}








func TestAntigravityBuildRequestFlattensToolUnionsForClaude_Issue3662Regression(t *testing.T) {



	payload := []byte(`{"request":{"tools":[{"functionDeclarations":[{"name":"diagnostic_union","parametersJsonSchema":{` +
		`"type":"object",` +
		`"properties":{` +
		`"value":{"anyOf":[{"type":"boolean"},{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"]},{"type":"array","items":{"type":"string"}}]},` +
		`"duo":{"anyOf":[{"type":"string"},{"type":"string"}]}` +
		`},` +
		`"required":["value"]` +
		`}}]}]}}`)

	declParams := func(t *testing.T, modelName string) ojson.Result {
		t.Helper()
		body := buildRequestBodyFromRawPayload(t, modelName, payload)
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			t.Fatal(errMarshal)
		}
		decl := ojson.GetBytes(encoded, "request.tools.0.functionDeclarations.0")
		if !decl.Exists() {
			t.Fatalf("model %s: function declaration missing: %s", modelName, encoded)
		}
		if decl.Get("parametersJsonSchema").Exists() {
			t.Fatalf("model %s: parametersJsonSchema was not renamed for Antigravity: %s", modelName, decl.Raw)
		}
		params := decl.Get("parameters")
		if !params.Exists() {
			t.Fatalf("model %s: parameters missing after sanitization: %s", modelName, decl.Raw)
		}
		return params
	}

	t.Run("claude flattens nested tool unions", func(t *testing.T) {
		params := declParams(t, "claude-opus-5-5-high")

		value := params.Get("properties.value")
		if value.Get("anyOf").Exists() || value.Get("oneOf").Exists() {
			t.Fatalf("claude tool union survived sanitization; Antigravity Claude rejects nested multi-branch unions: %s", params.Raw)
		}

		if got := value.Get("type").String(); got != "object" {
			t.Fatalf("claude flattened value type = %q, want object (strongest branch): %s", got, params.Raw)
		}
		if !value.Get("properties.enabled").Exists() {
			t.Fatalf("claude flattened value lost the object branch properties: %s", params.Raw)
		}
		if desc := value.Get("description").String(); !strings.Contains(desc, "Accepts:") || !strings.Contains(desc, "boolean") || !strings.Contains(desc, "array") {
			t.Fatalf("claude flattened value lost the dropped-branch hint: %s", params.Raw)
		}

		duo := params.Get("properties.duo")
		if duo.Get("anyOf").Exists() || duo.Get("oneOf").Exists() {
			t.Fatalf("claude two-string-branch union survived sanitization: %s", params.Raw)
		}
		if got := duo.Get("type").String(); got != "string" {
			t.Fatalf("claude flattened duo type = %q, want string: %s", got, params.Raw)
		}
	})









	t.Run("claude keeps unions in function return schemas", func(t *testing.T) {
		unionSchema := `{"type":"object","properties":{"item":{"anyOf":[{"type":"string"},{"type":"object","properties":{"image":{"type":"string"}},"required":["image"]},{"type":"object","properties":{"audio":{"type":"string"}},"required":["audio"]}]}},"required":["item"]}`
		decl := `{"name":"diagnostic_union"` +
			`,"parametersJsonSchema":` + unionSchema +
			`,"response":` + unionSchema +
			`,"responseJsonSchema":` + unionSchema +
			`,"response_json_schema":` + unionSchema + `}`
		returnPayload := []byte(`{"request":{"tools":[{"functionDeclarations":[` + decl + `]}]}}`)

		body := buildRequestBodyFromRawPayload(t, "claude-opus-5-5-high", returnPayload)
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			t.Fatal(errMarshal)
		}
		gotDecl := ojson.GetBytes(encoded, "request.tools.0.functionDeclarations.0")
		params := gotDecl.Get("parameters")
		if params.Get("properties.item.anyOf").Exists() || params.Get("properties.item.type").String() != "object" || !params.Get("properties.item.properties.image").Exists() {
			t.Fatalf("claude parameter schema union was not flattened: %s", params.Raw)
		}
		for _, key := range []string{"response", "responseJsonSchema", "response_json_schema"} {
			ret := gotDecl.Get(key)
			if !ret.Exists() {
				t.Fatalf("claude return schema %q missing after cleaning: %s", key, gotDecl.Raw)
			}
			if got := len(ret.Get("properties.item.anyOf").Array()); got != 3 {
				t.Fatalf("claude return schema %q anyOf branch count = %d, want 3 preserved: %s", key, got, ret.Raw)
			}
			if !ret.Get("properties.item.anyOf.2.properties.audio").Exists() {
				t.Fatalf("claude return schema %q lost the audio branch: %s", key, ret.Raw)
			}
		}
	})





	t.Run("claude chain keeps nullable marker after flattening", func(t *testing.T) {
		input := []byte(`{"model":"claude-opus-5-5-high","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"diagnostic_union","input_schema":{"type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"integer"},{"type":"null"}]}},"required":["value"]}}]}`)
		translated, errConv := antigravityclaude.ConvertClaudeRequestToAntigravity("claude-opus-5-5-high", input, false)
		if errConv != nil {
			t.Fatalf("convert error: %v", errConv)
		}
		translatedValue := ojson.GetBytes(translated, "request.tools.0.functionDeclarations.0.parametersJsonSchema.properties.value")
		if len(translatedValue.Get("anyOf").Array()) != 2 || !translatedValue.Get("nullable").Bool() {
			t.Fatalf("translator must preserve the union and set nullable:true: %s", translatedValue.Raw)
		}

		body := buildRequestBodyFromRawPayload(t, "claude-opus-5-5-high", translated)
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			t.Fatal(errMarshal)
		}
		finalValue := ojson.GetBytes(encoded, "request.tools.0.functionDeclarations.0.parameters.properties.value")
		if finalValue.Get("anyOf").Exists() || finalValue.Get("type").String() != "string" {
			t.Fatalf("executor must flatten the union for Claude: %s", finalValue.Raw)
		}
		if !finalValue.Get("nullable").Bool() {
			t.Fatalf("executor dropped nullable:true while flattening: %s", finalValue.Raw)
		}
	})

	t.Run("claude translator then executor honors the selected model", func(t *testing.T) {
		for _, modelName := range []string{"gemini-3.6-flash-high", "claude-opus-5-5-high"} {
			input := []byte(`{"model":"` + modelName + `","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"diagnostic_union","input_schema":{"type":"object","properties":{"item":{"anyOf":[{"type":"string"},{"type":"object","properties":{"image":{"type":"string"}},"required":["image"]},{"type":"object","properties":{"audio":{"type":"string"}},"required":["audio"]}]}},"required":["item"]}}]}`)
			translated, errConv := antigravityclaude.ConvertClaudeRequestToAntigravity(modelName, input, false)
			if errConv != nil {
				t.Fatalf("model %s: convert error: %v", modelName, errConv)
			}
			translatedParams := ojson.GetBytes(translated, "request.tools.0.functionDeclarations.0.parametersJsonSchema")
			if got := len(translatedParams.Get("properties.item.anyOf").Array()); got != 3 {
				t.Fatalf("model %s: translator anyOf branch count = %d, want 3 (flattening belongs to the executor, not the translator): %s", modelName, got, translatedParams.Raw)
			}
			if !translatedParams.Get("properties.item.anyOf.2.properties.audio").Exists() {
				t.Fatalf("model %s: translator lost the audio branch: %s", modelName, translatedParams.Raw)
			}

			body := buildRequestBodyFromRawPayload(t, modelName, translated)
			encoded, errMarshal := json.Marshal(body)
			if errMarshal != nil {
				t.Fatal(errMarshal)
			}
			finalParams := ojson.GetBytes(encoded, "request.tools.0.functionDeclarations.0.parameters")
			if !finalParams.Exists() {
				t.Fatalf("model %s: final parameters missing: %s", modelName, encoded)
			}
			if strings.Contains(modelName, "claude") {
				if finalParams.Get("properties.item.anyOf").Exists() || finalParams.Get("properties.item.type").String() != "object" || !finalParams.Get("properties.item.properties.image").Exists() {
					t.Fatalf("model %s: executor must flatten the union for Claude: %s", modelName, finalParams.Raw)
				}
			} else if got := len(finalParams.Get("properties.item.anyOf").Array()); got != 3 || !finalParams.Get("properties.item.anyOf.2.properties.audio").Exists() {
				t.Fatalf("model %s: executor must keep 3 preserved branches incl. audio for Gemini, got %d: %s", modelName, got, finalParams.Raw)
			}
		}
	})

	t.Run("gemini keeps preserved tool unions", func(t *testing.T) {



		for _, modelName := range []string{"gemini-3.1-pro-low", "gemini-3.6-flash-high"} {
			params := declParams(t, modelName)

			if got := len(params.Get("properties.value.anyOf").Array()); got != 3 {
				t.Fatalf("model %s: preserved anyOf branch count = %d, want 3: %s", modelName, got, params.Raw)
			}
			if !params.Get("properties.value.anyOf.1.properties.enabled").Exists() {
				t.Fatalf("model %s: preserved anyOf lost the object branch: %s", modelName, params.Raw)
			}
			if got := len(params.Get("properties.duo.anyOf").Array()); got != 2 {
				t.Fatalf("model %s: preserved two-branch anyOf count = %d, want 2: %s", modelName, got, params.Raw)
			}
		}
	})
}






func TestSanitizeAntigravityRequestSchemasScalesLinearlyWithDeclarations(t *testing.T) {
	const declCount = 256

	var input, want strings.Builder
	input.WriteString(`{"request":{"tools":[{"functionDeclarations":[`)
	want.WriteString(`{"request":{"tools":[{"functionDeclarations":[`)
	for i := 0; i < declCount; i++ {
		if i > 0 {
			input.WriteByte(',')
			want.WriteByte(',')
		}





		fmt.Fprintf(&input, `{"name":"tool_%d","description":"%s","parametersJsonSchema":{"type":"object","$id":"drop","properties":{"q":{"type":"string"}}}}`, i, strings.Repeat("x", 4096))


		fmt.Fprintf(&want, `{"name":"tool_%d","description":"%s","parameters":{"type":"object","properties":{"q":{"type":"string"},"_":{"type":"boolean"}},"required":["_"]}}`, i, strings.Repeat("x", 4096))
	}
	input.WriteString(`]}]}}`)
	want.WriteString(`]}]}}`)

	payload := input.String()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := sanitizeAntigravityRequestSchemasForModel(payload, true, false)
	runtime.ReadMemStats(&after)

	if got != want.String() {
		t.Fatalf("sanitized tools diverged from the expected bytes\nfirst declaration want: %s\nfirst declaration got:  %s",
			ojson.Get(want.String(), "request.tools.0.functionDeclarations.0").Raw,
			ojson.Get(got, "request.tools.0.functionDeclarations.0").Raw)
	}





	const allowedCopies = 32
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > uint64(allowedCopies*len(payload)) {
		t.Fatalf("sanitizing %d declarations allocated %d bytes for a %d byte payload, want at most %d (cost must be linear in the tools size, not per declaration)",
			declCount, allocated, len(payload), allowedCopies*len(payload))
	}
}







func TestSanitizeAntigravityRequestSchemasPreservesToolArrayWhitespace(t *testing.T) {
	input := "{\n" +
		"  \"request\": {\n" +
		"    \"tools\": [\n" +
		"      {\"functionDeclarations\": [\n" +
		"        {\"name\": \"a\", \"parametersJsonSchema\": {\"type\": \"object\", \"$id\": \"drop\", \"properties\": {\"q\": {\"type\": \"string\"}}}} ,\n" +
		"        {\"name\": \"b\", \"parameters\": {\"type\": \"object\", \"required\": [\"q\"], \"properties\": {\"q\": {\"type\": \"string\"}}}} ,\n" +
		"        {\"name\": \"c\"}\n" +
		"      ]} ,\n" +
		"      {\"googleSearch\": {}}\n" +
		"    ]\n" +
		"  }\n" +
		"}\n"








	want := strings.Replace(input,
		`{"name": "a", "parametersJsonSchema": {"type": "object", "$id": "drop", "properties": {"q": {"type": "string"}}}}`,
		`{"name": "a","parameters":{"type": "object", "properties": {"q": {"type": "string"},"_":{"type":"boolean"}},"required":["_"]}}`, 1)
	if want == input {
		t.Fatal("test fixture is broken: declaration a was not replaced in the expected value")
	}

	if got := sanitizeAntigravityRequestSchemasForModel(input, true, false); got != want {
		t.Fatalf("pretty-printed tools lost whitespace outside the cleaned declaration\nwant:\n%s\ngot:\n%s", want, got)
	}
}
