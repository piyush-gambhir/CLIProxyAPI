package test

import (
	"fmt"
	"testing"
	"time"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/antigravity"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/claude"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/codex"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/gemini"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/interactions"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/kimi"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/openai"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/xai"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type thinkingTestCase struct {
	name            string
	from            string
	to              string
	model           string
	inputJSON       string
	expectField     string
	expectValue     string
	expectField2    string
	expectValue2    string
	expectField3    string
	expectValue3    string
	expectAbsent    []string
	includeThoughts string
	expectErr       bool
}

func TestThinkingE2EMatrix_Suffix(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("thinking-e2e-suffix-%d", time.Now().UnixNano())

	reg.RegisterClient(uid, "test", getTestModels())
	defer reg.UnregisterClient(uid)

	cases := []thinkingTestCase{

		{
			name:        "1",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "2",
			from:        "openai",
			to:          "codex",
			model:       "level-model(medium)",
			inputJSON:   `{"model":"level-model(medium)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "3",
			from:        "openai",
			to:          "codex",
			model:       "level-model(xhigh)",
			inputJSON:   `{"model":"level-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "4",
			from:        "openai",
			to:          "codex",
			model:       "level-model(none)",
			inputJSON:   `{"model":"level-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "5",
			from:        "openai",
			to:          "codex",
			model:       "level-model(auto)",
			inputJSON:   `{"model":"level-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "6",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "7",
			from:        "gemini",
			to:          "codex",
			model:       "level-model(8192)",
			inputJSON:   `{"model":"level-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "8",
			from:        "gemini",
			to:          "codex",
			model:       "level-model(64000)",
			inputJSON:   `{"model":"level-model(64000)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "9",
			from:        "gemini",
			to:          "codex",
			model:       "level-model(0)",
			inputJSON:   `{"model":"level-model(0)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "10",
			from:        "gemini",
			to:          "codex",
			model:       "level-model(-1)",
			inputJSON:   `{"model":"level-model(-1)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "11",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "12",
			from:        "claude",
			to:          "openai",
			model:       "level-model(8192)",
			inputJSON:   `{"model":"level-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "13",
			from:        "claude",
			to:          "openai",
			model:       "level-model(64000)",
			inputJSON:   `{"model":"level-model(64000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "14",
			from:        "claude",
			to:          "openai",
			model:       "level-model(0)",
			inputJSON:   `{"model":"level-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning_effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "15",
			from:        "claude",
			to:          "openai",
			model:       "level-model(-1)",
			inputJSON:   `{"model":"level-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "16",
			from:        "gemini",
			to:          "openai",
			model:       "level-subset-model(8192)",
			inputJSON:   `{"model":"level-subset-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning_effort",
			expectValue: "low",
			expectErr:   false,
		},

		{
			name:            "17",
			from:            "claude",
			to:              "gemini",
			model:           "level-subset-model(1)",
			inputJSON:       `{"model":"level-subset-model(1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "17A",
			from:        "openai",
			to:          "codex",
			model:       "level-subset-model(auto)",
			inputJSON:   `{"model":"level-subset-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "low",
			expectErr:   false,
		},

		{
			name:        "18",
			from:        "openai",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "19",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model(medium)",
			inputJSON:       `{"model":"gemini-budget-model(medium)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "20",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model(xhigh)",
			inputJSON:       `{"model":"gemini-budget-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "21",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model(none)",
			inputJSON:       `{"model":"gemini-budget-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "128",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "22",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model(auto)",
			inputJSON:       `{"model":"gemini-budget-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "23",
			from:        "claude",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "24",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model(8192)",
			inputJSON:       `{"model":"gemini-budget-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "25",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model(64000)",
			inputJSON:       `{"model":"gemini-budget-model(64000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "26",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model(0)",
			inputJSON:       `{"model":"gemini-budget-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "128",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "27",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model(-1)",
			inputJSON:       `{"model":"gemini-budget-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "28",
			from:        "openai",
			to:          "gemini",
			model:       "gemini-mixed-model",
			inputJSON:   `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "29",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model(high)",
			inputJSON:       `{"model":"gemini-mixed-model(high)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "30",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model(xhigh)",
			inputJSON:       `{"model":"gemini-mixed-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "31",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model(none)",
			inputJSON:       `{"model":"gemini-mixed-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "32",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model(auto)",
			inputJSON:       `{"model":"gemini-mixed-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "33",
			from:        "claude",
			to:          "gemini",
			model:       "gemini-mixed-model",
			inputJSON:   `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "34",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model(8192)",
			inputJSON:       `{"model":"gemini-mixed-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "35",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model(64000)",
			inputJSON:       `{"model":"gemini-mixed-model(64000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "32768",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "36",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model(0)",
			inputJSON:       `{"model":"gemini-mixed-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "37",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model(-1)",
			inputJSON:       `{"model":"gemini-mixed-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "38",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "39",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model(medium)",
			inputJSON:   `{"model":"claude-budget-model(medium)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "40",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model(xhigh)",
			inputJSON:   `{"model":"claude-budget-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "32768",
			expectErr:   false,
		},

		{
			name:        "41",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model(none)",
			inputJSON:   `{"model":"claude-budget-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.type",
			expectValue: "disabled",
			expectErr:   false,
		},

		{
			name:        "42",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model(auto)",
			inputJSON:   `{"model":"claude-budget-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "64512",
			expectErr:   false,
		},

		{
			name:        "43",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "44",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model(8192)",
			inputJSON:   `{"model":"claude-budget-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "45",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model(200000)",
			inputJSON:   `{"model":"claude-budget-model(200000)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "128000",
			expectErr:   false,
		},

		{
			name:        "46",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model(0)",
			inputJSON:   `{"model":"claude-budget-model(0)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "thinking.type",
			expectValue: "disabled",
			expectErr:   false,
		},

		{
			name:        "47",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model(-1)",
			inputJSON:   `{"model":"claude-budget-model(-1)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "64512",
			expectErr:   false,
		},

		{
			name:        "48",
			from:        "gemini",
			to:          "antigravity",
			model:       "antigravity-budget-model",
			inputJSON:   `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "49",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model(medium)",
			inputJSON:       `{"model":"antigravity-budget-model(medium)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "50",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model(xhigh)",
			inputJSON:       `{"model":"antigravity-budget-model(xhigh)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "51",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model(none)",
			inputJSON:       `{"model":"antigravity-budget-model(none)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "0",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "52",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model(auto)",
			inputJSON:       `{"model":"antigravity-budget-model(auto)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "53",
			from:        "claude",
			to:          "antigravity",
			model:       "antigravity-budget-model",
			inputJSON:   `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "54",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model(8192)",
			inputJSON:       `{"model":"antigravity-budget-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "55",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model(64000)",
			inputJSON:       `{"model":"antigravity-budget-model(64000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "56",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model(0)",
			inputJSON:       `{"model":"antigravity-budget-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "0",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "57",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model(-1)",
			inputJSON:       `{"model":"antigravity-budget-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "58",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "59",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model(8192)",
			inputJSON:   `{"model":"no-thinking-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "60",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model(0)",
			inputJSON:   `{"model":"no-thinking-model(0)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "61",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model(-1)",
			inputJSON:   `{"model":"no-thinking-model(-1)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "62",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "63",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model(8192)",
			inputJSON:   `{"model":"no-thinking-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "64",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model(0)",
			inputJSON:   `{"model":"no-thinking-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "65",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model(-1)",
			inputJSON:   `{"model":"no-thinking-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "66",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "67",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model(8192)",
			inputJSON:   `{"model":"user-defined-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "68",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model(64000)",
			inputJSON:   `{"model":"user-defined-model(64000)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning_effort",
			expectValue: "xhigh",
			expectErr:   false,
		},

		{
			name:        "69",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model(0)",
			inputJSON:   `{"model":"user-defined-model(0)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning_effort",
			expectValue: "none",
			expectErr:   false,
		},

		{
			name:        "70",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model(-1)",
			inputJSON:   `{"model":"user-defined-model(-1)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning_effort",
			expectValue: "auto",
			expectErr:   false,
		},

		{
			name:        "71",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "72",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model(8192)",
			inputJSON:   `{"model":"user-defined-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "73",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model(64000)",
			inputJSON:   `{"model":"user-defined-model(64000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "xhigh",
			expectErr:   false,
		},

		{
			name:        "74",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model(0)",
			inputJSON:   `{"model":"user-defined-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "none",
			expectErr:   false,
		},

		{
			name:        "75",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model(-1)",
			inputJSON:   `{"model":"user-defined-model(-1)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "auto",
			expectErr:   false,
		},

		{
			name:            "76",
			from:            "openai",
			to:              "gemini",
			model:           "user-defined-model(8192)",
			inputJSON:       `{"model":"user-defined-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "77",
			from:        "openai",
			to:          "claude",
			model:       "user-defined-model(8192)",
			inputJSON:   `{"model":"user-defined-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:            "78",
			from:            "openai-response",
			to:              "gemini",
			model:           "user-defined-model(8192)",
			inputJSON:       `{"model":"user-defined-model(8192)","input":[{"role":"user","content":"hi"}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "79",
			from:        "openai-response",
			to:          "claude",
			model:       "user-defined-model(8192)",
			inputJSON:   `{"model":"user-defined-model(8192)","input":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "80",
			from:        "openai",
			to:          "openai",
			model:       "level-model(high)",
			inputJSON:   `{"model":"level-model(high)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "81",
			from:        "openai",
			to:          "openai",
			model:       "level-model(xhigh)",
			inputJSON:   `{"model":"level-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "82",
			from:        "openai-response",
			to:          "codex",
			model:       "level-model(high)",
			inputJSON:   `{"model":"level-model(high)","input":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "83",
			from:        "openai-response",
			to:          "codex",
			model:       "level-model(xhigh)",
			inputJSON:   `{"model":"level-model(xhigh)","input":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:            "84",
			from:            "gemini",
			to:              "gemini",
			model:           "gemini-budget-model(8192)",
			inputJSON:       `{"model":"gemini-budget-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "85",
			from:            "gemini",
			to:              "gemini",
			model:           "gemini-budget-model(64000)",
			inputJSON:       `{"model":"gemini-budget-model(64000)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "86",
			from:        "claude",
			to:          "claude",
			model:       "claude-budget-model(8192)",
			inputJSON:   `{"model":"claude-budget-model(8192)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "87",
			from:        "claude",
			to:          "claude",
			model:       "claude-budget-model(200000)",
			inputJSON:   `{"model":"claude-budget-model(200000)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "thinking.budget_tokens",
			expectValue: "128000",
			expectErr:   false,
		},

		{
			name:            "88",
			from:            "gemini",
			to:              "antigravity",
			model:           "gemini-budget-model(64000)",
			inputJSON:       `{"model":"gemini-budget-model(64000)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "89",
			from:            "gemini",
			to:              "antigravity",
			model:           "gemini-budget-model(8192)",
			inputJSON:       `{"model":"gemini-budget-model(8192)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},
	}

	runThinkingTests(t, cases)
}

func TestThinkingE2EMatrix_Body(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("thinking-e2e-body-%d", time.Now().UnixNano())

	reg.RegisterClient(uid, "test", getTestModels())
	defer reg.UnregisterClient(uid)

	cases := []thinkingTestCase{

		{
			name:        "1",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "2",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "3",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "4",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField: "reasoning.effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "5",
			from:        "openai",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"auto"}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "6",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "7",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "8",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":64000}}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "9",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`,
			expectField: "reasoning.effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "10",
			from:        "gemini",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "11",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "12",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "13",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":64000}}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "14",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField: "reasoning_effort",
			expectValue: "minimal",
			expectErr:   false,
		},

		{
			name:        "15",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "16",
			from:        "gemini",
			to:          "openai",
			model:       "level-subset-model",
			inputJSON:   `{"model":"level-subset-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "reasoning_effort",
			expectValue: "low",
			expectErr:   false,
		},

		{
			name:            "17",
			from:            "claude",
			to:              "gemini",
			model:           "level-subset-model",
			inputJSON:       `{"model":"level-subset-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":1}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "18",
			from:        "openai",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "19",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:            "20",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:            "21",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "128",
			includeThoughts: "false",
			expectErr:       false,
		},

		{
			name:            "22",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"auto"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:        "23",
			from:        "claude",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "24",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "25",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":64000}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "26",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "128",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "27",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "28",
			from:        "openai",
			to:          "gemini",
			model:       "gemini-mixed-model",
			inputJSON:   `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "29",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:            "30",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:            "31",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "false",
			expectErr:       false,
		},

		{
			name:        "31A",
			from:        "openai",
			to:          "gemini",
			model:       "gemini-toggle-mixed-model",
			inputJSON:   `{"model":"gemini-toggle-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "31B",
			from:        "openai",
			to:          "antigravity",
			model:       "gemini-toggle-mixed-model",
			inputJSON:   `{"model":"gemini-toggle-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "31C",
			from:        "openai-response",
			to:          "gemini",
			model:       "gemini-toggle-mixed-model",
			inputJSON:   `{"model":"gemini-toggle-mixed-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"none"}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "31D",
			from:        "openai-response",
			to:          "antigravity",
			model:       "gemini-toggle-mixed-model",
			inputJSON:   `{"model":"gemini-toggle-mixed-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"none"}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "32",
			from:            "openai",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"auto"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:        "33",
			from:        "claude",
			to:          "gemini",
			model:       "gemini-mixed-model",
			inputJSON:   `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "34",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "35",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":64000}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "32768",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "36",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "low",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "37",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "38",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "39",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "40",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "thinking.budget_tokens",
			expectValue: "32768",
			expectErr:   false,
		},

		{
			name:        "41",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`,
			expectField: "thinking.type",
			expectValue: "disabled",
			expectErr:   false,
		},

		{
			name:        "42",
			from:        "openai",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"auto"}`,
			expectField: "thinking.budget_tokens",
			expectValue: "64512",
			expectErr:   false,
		},

		{
			name:        "43",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "44",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "45",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":200000}}}`,
			expectField: "thinking.budget_tokens",
			expectValue: "128000",
			expectErr:   false,
		},

		{
			name:        "46",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`,
			expectField: "thinking.type",
			expectValue: "disabled",
			expectErr:   false,
		},

		{
			name:        "47",
			from:        "gemini",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField: "thinking.budget_tokens",
			expectValue: "64512",
			expectErr:   false,
		},

		{
			name:        "48",
			from:        "gemini",
			to:          "antigravity",
			model:       "antigravity-budget-model",
			inputJSON:   `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "49",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "50",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"xhigh"}}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "51",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"none"}}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "0",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "52",
			from:            "gemini",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "53",
			from:        "claude",
			to:          "antigravity",
			model:       "antigravity-budget-model",
			inputJSON:   `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "54",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "55",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":64000}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "56",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "0",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:            "57",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "-1",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "58",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "59",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "60",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "61",
			from:        "gemini",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "62",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "63",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "64",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "65",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "66",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:        "67",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "68",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":64000}}}`,
			expectField: "reasoning_effort",
			expectValue: "xhigh",
			expectErr:   false,
		},

		{
			name:        "69",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`,
			expectField: "reasoning_effort",
			expectValue: "none",
			expectErr:   false,
		},

		{
			name:        "70",
			from:        "gemini",
			to:          "openai",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField: "reasoning_effort",
			expectValue: "auto",
			expectErr:   false,
		},

		{
			name:        "71",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "72",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField: "reasoning.effort",
			expectValue: "medium",
			expectErr:   false,
		},

		{
			name:        "73",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":64000}}`,
			expectField: "reasoning.effort",
			expectValue: "xhigh",
			expectErr:   false,
		},

		{
			name:        "74",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField: "reasoning.effort",
			expectValue: "none",
			expectErr:   false,
		},

		{
			name:        "75",
			from:        "claude",
			to:          "codex",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":-1}}`,
			expectField: "reasoning.effort",
			expectValue: "auto",
			expectErr:   false,
		},

		{
			name:            "76",
			from:            "openai",
			to:              "gemini",
			model:           "user-defined-model",
			inputJSON:       `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "true",
			expectErr:       false,
		},

		{
			name:        "77",
			from:        "openai",
			to:          "claude",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:            "78",
			from:            "openai-response",
			to:              "gemini",
			model:           "user-defined-model",
			inputJSON:       `{"model":"user-defined-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"medium"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "79",
			from:        "openai-response",
			to:          "claude",
			model:       "user-defined-model",
			inputJSON:   `{"model":"user-defined-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"medium"}}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "80",
			from:        "openai",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "81",
			from:        "openai",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "82",
			from:        "openai-response",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"high"}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "83",
			from:        "openai-response",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"xhigh"}}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:            "84",
			from:            "gemini",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "85",
			from:        "gemini",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":64000}}}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "86",
			from:        "claude",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":8192}}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
			expectErr:   false,
		},

		{
			name:        "87",
			from:        "claude",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":200000}}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:        "88",
			from:        "gemini",
			to:          "antigravity",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":64000}}}`,
			expectField: "",
			expectErr:   true,
		},

		{
			name:            "89",
			from:            "gemini",
			to:              "antigravity",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},
	}

	runThinkingTests(t, cases)
}

func TestThinkingE2EProviderTargets(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("thinking-e2e-provider-targets-%d", time.Now().UnixNano())

	reg.RegisterClient(uid, "test", getTestModels())
	defer reg.UnregisterClient(uid)

	cases := []thinkingTestCase{

		{
			name:         "K1",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model(high)",
			inputJSON:    `{"model":"kimi-toggle-thinking-model(high)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K2",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model(none)",
			inputJSON:    `{"model":"kimi-toggle-thinking-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:  "thinking.type",
			expectValue:  "disabled",
			expectAbsent: []string{"thinking.effort", "reasoning_effort"},
		},
		{
			name:         "K3",
			from:         "gemini",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model(32768)",
			inputJSON:    `{"model":"kimi-toggle-thinking-model(32768)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K4",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model(auto)",
			inputJSON:    `{"model":"kimi-toggle-thinking-model(auto)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "medium",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K5",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-tiered-thinking-model(none)",
			inputJSON:    `{"model":"kimi-tiered-thinking-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "low",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K6",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K7",
			from:         "openai-response",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"none"}}`,
			expectField:  "thinking.type",
			expectValue:  "disabled",
			expectAbsent: []string{"thinking.effort", "reasoning_effort"},
		},
		{
			name:         "K8",
			from:         "gemini",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":32768}}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K9",
			from:         "gemini",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "medium",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K10",
			from:         "claude",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField:  "thinking.type",
			expectValue:  "disabled",
			expectAbsent: []string{"thinking.effort", "reasoning_effort"},
		},
		{
			name:         "K11",
			from:         "claude",
			to:           "kimi",
			model:        "kimi-tiered-thinking-model",
			inputJSON:    `{"model":"kimi-tiered-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "low",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K12",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high","thinking":{"keep":"all"}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectField3: "thinking.keep",
			expectValue3: "all",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K13",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","effort":"high","keep":"all"}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectField3: "thinking.keep",
			expectValue3: "all",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K14",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","keep":"all"}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.keep",
			expectValue2: "all",
			expectAbsent: []string{"thinking.effort", "reasoning_effort"},
		},
		{
			name:         "K15",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"effort":"high"},"reasoning_effort":"low"}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:         "K16",
			from:         "openai",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"auto"}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "medium",
			expectAbsent: []string{"reasoning_effort"},
		},

		{
			name:        "X1",
			from:        "openai",
			to:          "xai",
			model:       "xai-level-model(high)",
			inputJSON:   `{"model":"xai-level-model(high)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},
		{
			name:        "X2",
			from:        "openai",
			to:          "xai",
			model:       "xai-level-model(xhigh)",
			inputJSON:   `{"model":"xai-level-model(xhigh)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},
		{
			name:        "X3",
			from:        "openai-response",
			to:          "xai",
			model:       "xai-level-model(max)",
			inputJSON:   `{"model":"xai-level-model(max)","input":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},
		{
			name:        "X4",
			from:        "gemini",
			to:          "xai",
			model:       "xai-level-model(512)",
			inputJSON:   `{"model":"xai-level-model(512)","contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			expectField: "reasoning.effort",
			expectValue: "low",
		},
		{
			name:        "X5",
			from:        "claude",
			to:          "xai",
			model:       "xai-level-model(0)",
			inputJSON:   `{"model":"xai-level-model(0)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "reasoning.effort",
			expectValue: "none",
		},
		{
			name:        "X6",
			from:        "openai",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},
		{
			name:        "X7",
			from:        "openai-response",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","input":[{"role":"user","content":"hi"}],"reasoning":{"effort":"minimal"}}`,
			expectField: "reasoning.effort",
			expectValue: "low",
		},
		{
			name:        "X8",
			from:        "gemini",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":32768}}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},
		{
			name:        "X9",
			from:        "claude",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":0}}`,
			expectField: "reasoning.effort",
			expectValue: "none",
		},
		{
			name:        "X10",
			from:        "claude",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},

		{
			name:         "I1",
			from:         "interactions",
			to:           "interactions",
			model:        "level-model",
			inputJSON:    `{"model":"level-model","generation_config":{"thinking_level":"high","thinking_summaries":"auto"},"input":"hi"}`,
			expectField:  "generation_config.thinking_level",
			expectValue:  "high",
			expectField2: "generation_config.thinking_summaries",
			expectValue2: "auto",
		},
		{
			name:        "I2",
			from:        "interactions",
			to:          "interactions",
			model:       "level-model(8192)",
			inputJSON:   `{"model":"level-model(8192)","input":"hi"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "medium",
		},

		{
			name:        "R1",
			from:        "openai-response",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","input":"hi","reasoning":{"effort":"high"}}`,
			expectField: "reasoning_effort",
			expectValue: "high",
		},
		{
			name:        "R2",
			from:        "openai-response",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","input":"hi","reasoning":{"effort":"none"}}`,
			expectField: "reasoning_effort",
			expectValue: "minimal",
		},
		{
			name:         "R3",
			from:         "openai-response",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","input":"hi","reasoning":{"effort":"high"}}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
			expectAbsent: []string{"reasoning_effort"},
		},
		{
			name:            "R4",
			from:            "openai-response",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","input":"hi","reasoning":{"effort":"medium"}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "true",
		},
	}

	runThinkingTests(t, cases)
}

func TestThinkingE2EInteractionsMatrix(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("thinking-e2e-interactions-%d", time.Now().UnixNano())

	reg.RegisterClient(uid, "test", getTestModels())
	defer reg.UnregisterClient(uid)

	cases := []thinkingTestCase{

		{
			name:        "IN1",
			from:        "claude",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":10000}}`,
			expectField: "generation_config.thinking_level",
			expectValue: "high",
		},
		{
			name:        "IN2",
			from:        "openai",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "minimal",
		},
		{
			name:        "IN3",
			from:        "openai-response",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","input":"hi","reasoning":{"effort":"low"}}`,
			expectField: "generation_config.thinking_level",
			expectValue: "low",
		},
		{
			name:        "IN4",
			from:        "gemini",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingBudget":20000}}}`,
			expectField: "generation_config.thinking_level",
			expectValue: "high",
		},

		{
			name:        "IN5",
			from:        "openai",
			to:          "interactions",
			model:       "level-subset-model",
			inputJSON:   `{"model":"level-subset-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "high",
		},

		{
			name:        "IN6",
			from:        "claude",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"disabled"}}`,
			expectField: "generation_config.thinking_level",
			expectValue: "minimal",
		},
		{
			name:        "IN7",
			from:        "openai",
			to:          "interactions",
			model:       "level-model(none)",
			inputJSON:   `{"model":"level-model(none)","messages":[{"role":"user","content":"hi"}]}`,
			expectField: "generation_config.thinking_level",
			expectValue: "minimal",
		},
		{
			name:        "IN8",
			from:        "interactions",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","generation_config":{"thinking_level":"none"},"input":"hi"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "minimal",
		},

		{
			name:         "IN9",
			from:         "interactions",
			to:           "interactions",
			model:        "level-model",
			inputJSON:    `{"model":"level-model","generation_config":{"thinking_level":"low","thinking_summaries":"auto"},"input":"hi"}`,
			expectField:  "generation_config.thinking_level",
			expectValue:  "low",
			expectField2: "generation_config.thinking_summaries",
			expectValue2: "auto",
		},

		{
			name:        "IN10",
			from:        "interactions",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","generation_config":{"thinking_budget":400},"input":"hi"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "minimal",
		},

		{
			name:        "IN11",
			from:        "interactions",
			to:          "interactions",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","generation_config":{"thinking_budget":-1},"input":"hi"}`,
			expectField: "generation_config.thinking_level",
			expectValue: "medium",
		},

		{
			name:        "OUT1",
			from:        "interactions",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","generation_config":{"thinking_level":"medium"},"input":"hi"}`,
			expectField: "thinking.budget_tokens",
			expectValue: "8192",
		},
		{
			name:        "OUT2",
			from:        "interactions",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","generation_config":{"thinking_level":"high"},"input":"hi"}`,
			expectField: "reasoning_effort",
			expectValue: "high",
		},
		{
			name:        "OUT3",
			from:        "interactions",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","generation_config":{"thinking_level":"low"},"input":"hi"}`,
			expectField: "reasoning.effort",
			expectValue: "low",
		},
		{
			name:        "OUT4",
			from:        "interactions",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","generation_config":{"thinking_level":"medium"},"input":"hi"}`,
			expectField: "generationConfig.thinkingConfig.thinkingBudget",
			expectValue: "8192",
		},
		{
			name:            "OUT5",
			from:            "interactions",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","generation_config":{"thinking_level":"medium"},"input":"hi"}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
		},
		{
			name:         "OUT6",
			from:         "interactions",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","generation_config":{"thinking_level":"high"},"input":"hi"}`,
			expectField:  "thinking.type",
			expectValue:  "enabled",
			expectField2: "thinking.effort",
			expectValue2: "high",
		},
		{
			name:        "OUT7",
			from:        "interactions",
			to:          "xai",
			model:       "xai-level-model",
			inputJSON:   `{"model":"xai-level-model","generation_config":{"thinking_level":"high"},"input":"hi"}`,
			expectField: "reasoning.effort",
			expectValue: "high",
		},

		{
			name:        "OUT8",
			from:        "interactions",
			to:          "claude",
			model:       "claude-budget-model",
			inputJSON:   `{"model":"claude-budget-model","generation_config":{"thinking_level":"none"},"input":"hi"}`,
			expectField: "thinking.type",
			expectValue: "disabled",
		},
		{
			name:            "OUT9",
			from:            "interactions",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","generation_config":{"thinking_level":"none"},"input":"hi"}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "0",
			includeThoughts: "",
		},
		{
			name:         "OUT10",
			from:         "interactions",
			to:           "kimi",
			model:        "kimi-toggle-thinking-model",
			inputJSON:    `{"model":"kimi-toggle-thinking-model","generation_config":{"thinking_level":"none"},"input":"hi"}`,
			expectField:  "thinking.type",
			expectValue:  "disabled",
			expectAbsent: []string{"thinking.effort", "reasoning_effort"},
		},

		{
			name:         "OUT11",
			from:         "interactions",
			to:           "gemini",
			model:        "gemini-toggle-mixed-model",
			inputJSON:    `{"model":"gemini-toggle-mixed-model","generation_config":{"thinking_level":"none"},"input":"hi"}`,
			expectAbsent: []string{"generationConfig.thinkingConfig"},
		},

		{
			name:        "OUT12",
			from:        "interactions",
			to:          "gemini",
			model:       "gemini-budget-model",
			inputJSON:   `{"model":"gemini-budget-model","generation_config":{"thinking_level":"auto"},"input":"hi"}`,
			expectField: "generationConfig.thinkingConfig.thinkingBudget",
			expectValue: "-1",
		},
	}

	runThinkingTests(t, cases)
}

func TestThinkingE2EClaudeAdaptive_Body(t *testing.T) {
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("thinking-e2e-claude-adaptive-%d", time.Now().UnixNano())

	reg.RegisterClient(uid, "test", getTestModels())
	defer reg.UnregisterClient(uid)

	cases := []thinkingTestCase{

		{
			name:        "A1",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"minimal"}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "A2",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"low"}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "A3",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium"}`,
			expectField: "output_config.effort",
			expectValue: "medium",
			expectErr:   false,
		},
		{
			name:        "A4",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "A5",
			from:        "openai",
			to:          "claude",
			model:       "claude-opus-4-6-model",
			inputJSON:   `{"model":"claude-opus-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "output_config.effort",
			expectValue: "max",
			expectErr:   false,
		},
		{
			name:        "A6",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "A7",
			from:        "openai",
			to:          "claude",
			model:       "claude-opus-4-6-model",
			inputJSON:   `{"model":"claude-opus-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"max"}`,
			expectField: "output_config.effort",
			expectValue: "max",
			expectErr:   false,
		},
		{
			name:        "A8",
			from:        "openai",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"max"}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "B1",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "B2",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "B3",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}`,
			expectField: "output_config.effort",
			expectValue: "medium",
			expectErr:   false,
		},
		{
			name:        "B4",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "B5",
			from:        "gemini",
			to:          "claude",
			model:       "claude-opus-4-6-model",
			inputJSON:   `{"model":"claude-opus-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"xhigh"}}}`,
			expectField: "output_config.effort",
			expectValue: "max",
			expectErr:   false,
		},
		{
			name:        "B6",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"xhigh"}}}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "B7",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":512}}}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "B8",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":1024}}}`,
			expectField: "output_config.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "B9",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":8192}}}`,
			expectField: "output_config.effort",
			expectValue: "medium",
			expectErr:   false,
		},
		{
			name:        "B10",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":24576}}}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "B11",
			from:        "gemini",
			to:          "claude",
			model:       "claude-opus-4-6-model",
			inputJSON:   `{"model":"claude-opus-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":32768}}}`,
			expectField: "output_config.effort",
			expectValue: "max",
			expectErr:   false,
		},
		{
			name:        "B12",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":32768}}}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "B13",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`,
			expectField: "thinking.type",
			expectValue: "disabled",
			expectErr:   false,
		},
		{
			name:        "B14",
			from:        "gemini",
			to:          "claude",
			model:       "claude-sonnet-4-6-model",
			inputJSON:   `{"model":"claude-sonnet-4-6-model","contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`,
			expectField: "output_config.effort",
			expectValue: "high",
			expectErr:   false,
		},

		{
			name:        "C1",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"minimal"}}`,
			expectField: "reasoning_effort",
			expectValue: "minimal",
			expectErr:   false,
		},
		{
			name:        "C2",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`,
			expectField: "reasoning_effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "C3",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`,
			expectField: "reasoning_effort",
			expectValue: "medium",
			expectErr:   false,
		},
		{
			name:        "C4",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "C5",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "C6",
			from:        "claude",
			to:          "openai",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`,
			expectField: "reasoning_effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "C7",
			from:        "claude",
			to:          "openai",
			model:       "no-thinking-model",
			inputJSON:   `{"model":"no-thinking-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField: "",
			expectErr:   false,
		},

		{
			name:            "C8",
			from:            "claude",
			to:              "gemini",
			model:           "level-subset-model",
			inputJSON:       `{"model":"level-subset-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "",
			expectErr:       false,
		},
		{
			name:            "C9",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "1024",
			includeThoughts: "",
			expectErr:       false,
		},
		{
			name:            "C10",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "8192",
			includeThoughts: "",
			expectErr:       false,
		},
		{
			name:            "C11",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},
		{
			name:            "C12",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-budget-model",
			inputJSON:       `{"model":"gemini-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},
		{
			name:            "C13",
			from:            "claude",
			to:              "gemini",
			model:           "gemini-mixed-model",
			inputJSON:       `{"model":"gemini-mixed-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField:     "generationConfig.thinkingConfig.thinkingLevel",
			expectValue:     "high",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:        "C14",
			from:        "claude",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"minimal"}}`,
			expectField: "reasoning.effort",
			expectValue: "minimal",
			expectErr:   false,
		},
		{
			name:        "C15",
			from:        "claude",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`,
			expectField: "reasoning.effort",
			expectValue: "low",
			expectErr:   false,
		},
		{
			name:        "C16",
			from:        "claude",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "C17",
			from:        "claude",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:        "C18",
			from:        "claude",
			to:          "codex",
			model:       "level-model",
			inputJSON:   `{"model":"level-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`,
			expectField: "reasoning.effort",
			expectValue: "high",
			expectErr:   false,
		},
		{
			name:            "C19",
			from:            "claude",
			to:              "antigravity",
			model:           "antigravity-budget-model",
			inputJSON:       `{"model":"antigravity-budget-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"}}`,
			expectField:     "request.generationConfig.thinkingConfig.thinkingBudget",
			expectValue:     "20000",
			includeThoughts: "",
			expectErr:       false,
		},

		{
			name:         "C20",
			from:         "claude",
			to:           "claude",
			model:        "claude-sonnet-4-6-model",
			inputJSON:    `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`,
			expectField:  "thinking.type",
			expectValue:  "adaptive",
			expectField2: "output_config.effort",
			expectValue2: "medium",
			expectErr:    false,
		},
		{
			name:         "C21",
			from:         "claude",
			to:           "claude",
			model:        "claude-opus-4-6-model",
			inputJSON:    `{"model":"claude-opus-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`,
			expectField:  "thinking.type",
			expectValue:  "adaptive",
			expectField2: "output_config.effort",
			expectValue2: "max",
			expectErr:    false,
		},
		{
			name:      "C22",
			from:      "claude",
			to:        "claude",
			model:     "claude-opus-4-6-model",
			inputJSON: `{"model":"claude-opus-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`,
			expectErr: true,
		},
		{
			name:         "C23",
			from:         "claude",
			to:           "claude",
			model:        "claude-sonnet-4-6-model",
			inputJSON:    `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`,
			expectField:  "thinking.type",
			expectValue:  "adaptive",
			expectField2: "output_config.effort",
			expectValue2: "high",
			expectErr:    false,
		},
		{
			name:      "C24",
			from:      "claude",
			to:        "claude",
			model:     "claude-sonnet-4-6-model",
			inputJSON: `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"max"}}`,
			expectErr: true,
		},
		{
			name:      "C25",
			from:      "claude",
			to:        "claude",
			model:     "claude-sonnet-4-6-model",
			inputJSON: `{"model":"claude-sonnet-4-6-model","messages":[{"role":"user","content":"hi"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`,
			expectErr: true,
		},
	}

	runThinkingTests(t, cases)
}

func getTestModels() []*registry.ModelInfo {
	return []*registry.ModelInfo{
		{
			ID:          "level-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "openai",
			DisplayName: "Level Model",
			Thinking:    &registry.ThinkingSupport{Levels: []string{"minimal", "low", "medium", "high"}, ZeroAllowed: false, DynamicAllowed: false},
		},
		{
			ID:          "level-subset-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "gemini",
			DisplayName: "Level Subset Model",
			Thinking:    &registry.ThinkingSupport{Levels: []string{"low", "high"}, ZeroAllowed: false, DynamicAllowed: false},
		},
		{
			ID:          "gemini-budget-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "gemini",
			DisplayName: "Gemini Budget Model",
			Thinking:    &registry.ThinkingSupport{Min: 128, Max: 20000, ZeroAllowed: false, DynamicAllowed: true},
		},
		{
			ID:          "gemini-mixed-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "gemini",
			DisplayName: "Gemini Mixed Model",
			Thinking:    &registry.ThinkingSupport{Min: 128, Max: 32768, Levels: []string{"low", "high"}, ZeroAllowed: false, DynamicAllowed: true},
		},
		{
			ID:          "gemini-toggle-mixed-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "gemini",
			DisplayName: "Gemini Toggle Mixed Model",
			Thinking:    &registry.ThinkingSupport{Min: 128, Max: 32768, Levels: []string{"low", "high"}, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:          "claude-budget-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "claude",
			DisplayName: "Claude Budget Model",
			Thinking:    &registry.ThinkingSupport{Min: 1024, Max: 128000, ZeroAllowed: true, DynamicAllowed: false},
		},
		{
			ID:                  "claude-opus-4-6-model",
			Object:              "model",
			Created:             1770318000,
			OwnedBy:             "anthropic",
			Type:                "claude",
			DisplayName:         "Claude 4.6 Opus",
			Description:         "Premium model combining maximum intelligence with practical performance",
			ContextLength:       1000000,
			MaxCompletionTokens: 128000,
			Thinking:            &registry.ThinkingSupport{Min: 1024, Max: 128000, ZeroAllowed: true, DynamicAllowed: false, Levels: []string{"low", "medium", "high", "max"}},
		},
		{
			ID:                  "claude-sonnet-4-6-model",
			Object:              "model",
			Created:             1771372800,
			OwnedBy:             "anthropic",
			Type:                "claude",
			DisplayName:         "Claude 4.6 Sonnet",
			ContextLength:       200000,
			MaxCompletionTokens: 64000,
			Thinking:            &registry.ThinkingSupport{Min: 1024, Max: 128000, ZeroAllowed: true, DynamicAllowed: false, Levels: []string{"low", "medium", "high"}},
		},
		{
			ID:          "antigravity-budget-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "antigravity",
			DisplayName: "Antigravity Budget Model",
			Thinking:    &registry.ThinkingSupport{Min: 128, Max: 20000, ZeroAllowed: true, DynamicAllowed: true},
		},
		{
			ID:          "kimi-toggle-thinking-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "moonshot",
			Type:        "kimi",
			DisplayName: "Kimi Toggle Thinking Model",
			Thinking:    &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}, ZeroAllowed: true, DynamicAllowed: false},
		},
		{
			ID:          "kimi-tiered-thinking-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "moonshot",
			Type:        "kimi",
			DisplayName: "Kimi Tiered Thinking Model",
			Thinking:    &registry.ThinkingSupport{Levels: []string{"low", "medium", "high"}, ZeroAllowed: false, DynamicAllowed: false},
		},
		{
			ID:          "xai-level-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "xai",
			Type:        "xai",
			DisplayName: "xAI Level Model",
			Thinking:    &registry.ThinkingSupport{Levels: []string{"none", "low", "medium", "high"}, ZeroAllowed: true, DynamicAllowed: false},
		},
		{
			ID:          "no-thinking-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "openai",
			DisplayName: "No Thinking Model",
			Thinking:    nil,
		},
		{
			ID:          "user-defined-model",
			Object:      "model",
			Created:     1700000000,
			OwnedBy:     "test",
			Type:        "openai",
			DisplayName: "User Defined Model",
			UserDefined: true,
			Thinking:    nil,
		},
	}
}

func runThinkingTests(t *testing.T, cases []thinkingTestCase) {
	for _, tc := range cases {
		tc := tc
		testName := fmt.Sprintf("Case%s_%s->%s_%s", tc.name, tc.from, tc.to, tc.model)
		t.Run(testName, func(t *testing.T) {
			suffixResult := thinking.ParseSuffix(tc.model)
			baseModel := suffixResult.ModelName

			translateTo := tc.to
			applyTo := tc.to
			switch applyTo {
			case "kimi":
				translateTo = "openai"
			case "xai":
				translateTo = "codex"
			}

			body := sdktranslator.TranslateRequest(
				sdktranslator.FromString(tc.from),
				sdktranslator.FromString(translateTo),
				baseModel,
				[]byte(tc.inputJSON),
				true,
			)
			if applyTo == "claude" {
				body, _ = ojson.SetBytes(body, "max_tokens", 200000)
			}

			body, err := thinking.ApplyThinking(body, tc.model, tc.from, applyTo, applyTo)

			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error but got none, body=%s", string(body))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v, body=%s", err, string(body))
			}

			for _, fieldPath := range tc.expectAbsent {
				if ojson.GetBytesNoCopy(body, fieldPath).Exists() {
					t.Fatalf("expected field %s to be absent, body=%s", fieldPath, string(body))
				}
			}

			if tc.expectField == "" {
				var hasThinking bool
				switch tc.to {
				case "gemini":
					hasThinking = ojson.GetBytesNoCopy(body, "generationConfig.thinkingConfig").Exists()
				case "antigravity":
					hasThinking = ojson.GetBytesNoCopy(body, "request.generationConfig.thinkingConfig").Exists()
				case "claude":
					hasThinking = ojson.GetBytesNoCopy(body, "thinking").Exists()
				case "openai":
					hasThinking = ojson.GetBytesNoCopy(body, "reasoning_effort").Exists()
				case "codex":
					hasThinking = ojson.GetBytesNoCopy(body, "reasoning.effort").Exists() || ojson.GetBytesNoCopy(body, "reasoning").Exists()
				case "kimi":
					hasThinking = ojson.GetBytesNoCopy(body, "thinking").Exists() || ojson.GetBytesNoCopy(body, "reasoning_effort").Exists()
				}
				if hasThinking {
					t.Fatalf("expected no thinking field but found one, body=%s", string(body))
				}
				return
			}

			assertField := func(fieldPath, expected string) {
				val := ojson.GetBytesNoCopy(body, fieldPath)
				if !val.Exists() {
					t.Fatalf("expected field %s not found, body=%s", fieldPath, string(body))
				}
				actualValue := val.String()
				if val.Type == ojson.Number {
					actualValue = fmt.Sprintf("%d", val.Int())
				}
				if actualValue != expected {
					t.Fatalf("field %s: expected %q, got %q, body=%s", fieldPath, expected, actualValue, string(body))
				}
			}

			assertField(tc.expectField, tc.expectValue)
			if tc.expectField2 != "" {
				assertField(tc.expectField2, tc.expectValue2)
			}

			if tc.to == "claude" && ojson.GetBytesNoCopy(body, "output_config.effort").Exists() {
				assertField("thinking.type", "adaptive")
			}
			if tc.expectField3 != "" {
				assertField(tc.expectField3, tc.expectValue3)
			}

			if tc.to == "gemini" || tc.to == "antigravity" {
				path := "generationConfig.thinkingConfig.includeThoughts"
				if tc.to == "antigravity" {
					path = "request.generationConfig.thinkingConfig.includeThoughts"
				}

				wantIncludeThoughts := tc.includeThoughts
				itVal := ojson.GetBytesNoCopy(body, path)
				if wantIncludeThoughts == "" {
					if itVal.Exists() {
						t.Fatalf("includeThoughts should be absent without summary intent, body=%s", string(body))
					}
				} else {
					if !itVal.Exists() {
						t.Fatalf("expected includeThoughts field not found, body=%s", string(body))
					}
					actual := fmt.Sprintf("%v", itVal.Bool())
					if actual != wantIncludeThoughts {
						t.Fatalf("includeThoughts: expected %s, got %s, body=%s", wantIncludeThoughts, actual, string(body))
					}
				}
			}
		})
	}
}
