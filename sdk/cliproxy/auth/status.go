package auth


type Status string

const (

	StatusUnknown Status = "unknown"

	StatusActive Status = "active"

	StatusPending Status = "pending"

	StatusRefreshing Status = "refreshing"

	StatusError Status = "error"

	StatusDisabled Status = "disabled"
)
