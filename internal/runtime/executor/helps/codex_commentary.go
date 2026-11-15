package helps

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	translatorcommon "github.com/router-for-me/CLIProxyAPI/v8/internal/translator/common"
)



func WithCodexCommentaryPolicy(ctx context.Context, cfg *config.Config) context.Context {
	enabled := translatorcommon.VisibleCodexCommentary(ctx)
	if cfg != nil && cfg.Client.Claude.ShowCodexCommentary {
		enabled = true
	}
	return translatorcommon.WithVisibleCodexCommentary(ctx, enabled)
}
