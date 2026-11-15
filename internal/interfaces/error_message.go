package interfaces

import "net/http"

type ErrorMessage struct {
	StatusCode int

	Error error

	Addon http.Header

	DirectResponse bool

	Body []byte

	Headers http.Header
}
