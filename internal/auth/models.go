package auth

type TokenStorage interface {
	SaveTokenToFile(authFilePath string) error
}
