package usage

import "context"





type HomeSelection struct {
	Reused           bool   `json:"reused"`
	Additional       bool   `json:"additional,omitempty"`
	Model            string `json:"model"`
	CredentialPolicy string `json:"credential_policy,omitempty"`
}

type homeSelectionContextKey struct{}


func WithHomeSelection(ctx context.Context, selection HomeSelection) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, homeSelectionContextKey{}, selection)
}



func HomeSelectionFromContext(ctx context.Context) *HomeSelection {
	if ctx == nil {
		return nil
	}
	selection, ok := ctx.Value(homeSelectionContextKey{}).(HomeSelection)
	if !ok {
		return nil
	}
	return &selection
}
