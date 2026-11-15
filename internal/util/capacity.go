package util

func ByteCapacity(lengths ...int) int {
	total := 0
	for _, length := range lengths {
		if length < 0 || length > int(^uint(0)>>1)-total {
			return 0
		}
		total += length
	}
	return total
}
