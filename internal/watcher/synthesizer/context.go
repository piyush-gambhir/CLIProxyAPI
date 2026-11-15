package synthesizer

import (
	"context"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)


type PluginAuthParser interface {
	ParseAuth(context.Context, pluginapi.AuthParseRequest) (*coreauth.Auth, bool, error)
}



type PluginMultiAuthParser interface {
	ParseAuths(context.Context, pluginapi.AuthParseRequest) ([]*coreauth.Auth, bool, error)
}


type SynthesisContext struct {

	Config *config.Config

	AuthDir string

	Now time.Time

	IDGenerator *StableIDGenerator

	PluginAuthParser PluginAuthParser

	StrictFileParsing bool



	IncompleteFiles map[string]struct{}
}



func (c *SynthesisContext) RecordIncompleteFile(path string) {
	if c == nil || path == "" {
		return
	}
	if c.IncompleteFiles == nil {
		c.IncompleteFiles = make(map[string]struct{})
	}
	c.IncompleteFiles[path] = struct{}{}
}
