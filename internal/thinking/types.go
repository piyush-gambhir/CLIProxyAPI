package thinking

import (
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

type ThinkingMode int

const (
	ModeBudget ThinkingMode = iota

	ModeLevel

	ModeNone

	ModeAuto
)

func (m ThinkingMode) String() string {
	switch m {
	case ModeBudget:
		return "budget"
	case ModeLevel:
		return "level"
	case ModeNone:
		return "none"
	case ModeAuto:
		return "auto"
	default:
		return "unknown"
	}
}

type ThinkingLevel string

const (
	LevelNone ThinkingLevel = "none"

	LevelAuto ThinkingLevel = "auto"

	LevelMinimal ThinkingLevel = "minimal"

	LevelLow ThinkingLevel = "low"

	LevelMedium ThinkingLevel = "medium"

	LevelHigh ThinkingLevel = "high"

	LevelXHigh ThinkingLevel = "xhigh"

	LevelMax ThinkingLevel = "max"
)

type ThinkingConfig struct {
	Mode ThinkingMode

	Budget int

	Level ThinkingLevel
}

type SuffixResult struct {
	ModelName string

	HasSuffix bool

	RawSuffix string
}

type ProviderApplier interface {
	Apply(body []byte, config ThinkingConfig, modelInfo *registry.ModelInfo) ([]byte, error)
}

type ProviderDocApplier interface {
	ProviderApplier
	ApplyDoc(doc *ojson.Doc, config ThinkingConfig, modelInfo *registry.ModelInfo) error
}
