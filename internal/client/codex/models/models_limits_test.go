package models

import (
	"math"
	"strconv"
	"testing"
)

func TestIntModelValueRejectsOverflow(t *testing.T) {
	for _, value := range []any{int64(-1), math.Inf(1), math.NaN(), math.Exp2(strconv.IntSize - 1)} {
		if got := intModelValue(map[string]any{"limit": value}, "limit"); got != 0 {
			t.Errorf("%v: got %d", value, got)
		}
	}
	for _, value := range []any{int64(128000), float64(128000)} {
		if got := intModelValue(map[string]any{"limit": value}, "limit"); got != 128000 {
			t.Errorf("%v: got %d", value, got)
		}
	}
}
