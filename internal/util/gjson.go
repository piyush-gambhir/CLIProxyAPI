package util

import (
	"unsafe"

	gjson "github.com/MoeForge/moego/ojson"
)

func GetGJSONBytesNoCopy(data []byte, path string) gjson.Result {
	if len(data) == 0 {
		return gjson.Result{}
	}
	return gjson.GetBytesNoCopy(data, path)
}

func ParseGJSONBytesNoCopy(data []byte) gjson.Result {
	if len(data) == 0 {
		return gjson.Result{}
	}
	return gjson.Parse(unsafe.String(unsafe.SliceData(data), len(data)))
}
