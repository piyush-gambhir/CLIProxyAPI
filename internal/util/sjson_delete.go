package util

import (
	"github.com/MoeForge/moego/ojson"
)










func DeleteJSONBytesIfExists(data []byte, path string) ([]byte, error) {
	if !GetGJSONBytesNoCopy(data, path).Exists() {
		return data, nil
	}
	return ojson.DeleteBytes(data, path)
}


func DeleteJSONDocIfExists(doc *ojson.Doc, path string) error {
	if doc == nil {
		return nil
	}
	if !doc.Get(path).Exists() {
		return nil
	}
	return doc.Delete(path)
}
