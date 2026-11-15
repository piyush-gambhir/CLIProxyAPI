



package xai

import (
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/thinking/provider/codex"
)


type Applier struct {
	codex.Applier
}

var (
	_ thinking.ProviderApplier    = (*Applier)(nil)
	_ thinking.ProviderDocApplier = (*Applier)(nil)
)


func NewApplier() *Applier {
	return &Applier{}
}

func init() {
	thinking.RegisterProvider("xai", NewApplier())
}
