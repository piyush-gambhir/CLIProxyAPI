package empty

type EmptyStorage struct {
	Type string `json:"type"`
}

func (ts *EmptyStorage) SaveTokenToFile(_ string) error {
	ts.Type = "empty"
	return nil
}
