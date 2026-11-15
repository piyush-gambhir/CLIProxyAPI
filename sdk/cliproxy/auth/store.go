package auth

import "context"

type Store interface {
	List(ctx context.Context) ([]*Auth, error)

	Save(ctx context.Context, auth *Auth) (string, error)

	Delete(ctx context.Context, id string) error
}
