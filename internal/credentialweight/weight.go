package credentialweight

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	Default int64 = 1

	Max int64 = 1_000_000
)

func Normalize(weight int64) (int64, error) {
	if weight <= 0 {
		return 0, nil
	}
	if weight > Max {
		return 0, fmt.Errorf("weight must not exceed %d", Max)
	}
	return weight, nil
}

func ParseString(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Default, nil
	}
	weight, errParse := strconv.ParseInt(raw, 10, 64)
	if errParse != nil {
		return 0, fmt.Errorf("weight must be an integer: %w", errParse)
	}
	return Normalize(weight)
}

func ParseValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return Normalize(int64(typed))
	case int8:
		return Normalize(int64(typed))
	case int16:
		return Normalize(int64(typed))
	case int32:
		return Normalize(int64(typed))
	case int64:
		return Normalize(typed)
	case uint:
		if uint64(typed) > uint64(Max) {
			return 0, fmt.Errorf("weight must not exceed %d", Max)
		}
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint32:
		if uint64(typed) > uint64(Max) {
			return 0, fmt.Errorf("weight must not exceed %d", Max)
		}
		return int64(typed), nil
	case uint64:
		if typed > uint64(Max) {
			return 0, fmt.Errorf("weight must not exceed %d", Max)
		}
		return int64(typed), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return 0, fmt.Errorf("weight must be an integer")
		}
		if typed <= 0 {
			return 0, nil
		}
		if typed > float64(Max) {
			return 0, fmt.Errorf("weight must not exceed %d", Max)
		}
		return int64(typed), nil
	case float32:
		return ParseValue(float64(typed))
	case json.Number:
		weight, errParse := typed.Int64()
		if errParse != nil {
			return 0, fmt.Errorf("weight must be an integer: %w", errParse)
		}
		return Normalize(weight)
	case string:
		return ParseString(typed)
	default:
		return 0, fmt.Errorf("weight must be an integer")
	}
}
