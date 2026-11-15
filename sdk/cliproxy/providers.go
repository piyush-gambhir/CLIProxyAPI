package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)


func NewFileTokenClientProvider() TokenClientProvider {
	return &fileTokenClientProvider{}
}

type fileTokenClientProvider struct{}

func (p *fileTokenClientProvider) Load(ctx context.Context, cfg *config.Config) (*TokenClientResult, error) {

	_ = ctx
	_ = cfg
	return &TokenClientResult{SuccessfulAuthed: 0}, nil
}


func NewAPIKeyClientProvider() APIKeyClientProvider {
	return &apiKeyClientProvider{}
}

type apiKeyClientProvider struct{}

func (p *apiKeyClientProvider) Load(ctx context.Context, cfg *config.Config) (*APIKeyClientResult, error) {
	geminiCount, vertexCompatCount, claudeCount, codexCount, xaiCount, metaCount, openAICompat := watcher.BuildAPIKeyClients(cfg)
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	return &APIKeyClientResult{
		GeminiKeyCount:       geminiCount,
		VertexCompatKeyCount: vertexCompatCount,
		ClaudeKeyCount:       claudeCount,
		CodexKeyCount:        codexCount,
		XAIKeyCount:          xaiCount,
		MetaKeyCount:         metaCount,
		OpenAICompatCount:    openAICompat,
	}, nil
}
