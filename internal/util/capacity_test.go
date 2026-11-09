package util

import "testing"

func TestByteCapacity(t *testing.T) {
	max := int(^uint(0) >> 1)
	for _, tc := range []struct {
		lengths []int
		want    int
	}{{[]int{4, 7, 2}, 13}, {[]int{max, 1}, 0}, {[]int{-1, 5}, 0}, {nil, 0}, {[]int{max - 2, 2}, max}} {
		if got := ByteCapacity(tc.lengths...); got != tc.want {
			t.Errorf("%v: got %d, want %d", tc.lengths, got, tc.want)
		}
	}
}
