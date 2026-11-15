



package synthesizer

import (
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)


type AuthSynthesizer interface {


	Synthesize(ctx *SynthesisContext) ([]*coreauth.Auth, error)
}
