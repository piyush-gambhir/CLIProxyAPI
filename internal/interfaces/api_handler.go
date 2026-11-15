package interfaces

type APIHandler interface {
	HandlerType() string

	Models() []map[string]any
}
