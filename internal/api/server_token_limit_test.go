package api

import (
	"strconv"
	"testing"
)

func TestHomeModelTokenLimit(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  int
	}{{int64(128000), 128000}, {int64(-1), 0}, {"9223372036854775807", func() int {
		if strconv.IntSize == 32 {
			return 0
		}
		return int(^uint(0) >> 1)
	}()}, {"invalid", 0}} {
		if got := homeModelTokenLimit(map[string]any{"limit": tc.value}, "limit"); got != tc.want {
			t.Errorf("limit %v: got %d, want %d", tc.value, got, tc.want)
		}
	}
}
