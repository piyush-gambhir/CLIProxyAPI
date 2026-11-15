package builtin

import (
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"

	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
)

func Registry() *sdktranslator.Registry {
	return sdktranslator.Default()
}

func Pipeline() *sdktranslator.Pipeline {
	return sdktranslator.NewPipeline(sdktranslator.Default())
}
