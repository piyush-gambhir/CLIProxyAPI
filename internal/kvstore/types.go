

package kvstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)


var ErrCompareAndSwapUnsupported = errors.New("kvstore: compare-and-swap command is not supported")


type KVSetOptions struct {
	EX time.Duration
	PX time.Duration
	NX bool
	XX bool
}


type KVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts KVSetOptions) (bool, error)
	KVSetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	KVTTL(ctx context.Context, key string) (time.Duration, bool, error)
	KVIncrBy(ctx context.Context, key string, delta int64) (int64, error)
	KVMGet(ctx context.Context, keys ...string) ([][]byte, []bool, error)
	KVMSet(ctx context.Context, pairs map[string][]byte) error
}


func BuildSetArgs(key string, value []byte, opts KVSetOptions) ([]any, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("home kv: key is empty")
	}
	if opts.EX > 0 && opts.PX > 0 {
		return nil, fmt.Errorf("home kv: EX and PX are mutually exclusive")
	}
	if opts.EX < 0 || opts.PX < 0 {
		return nil, fmt.Errorf("home kv: ttl must not be negative")
	}
	if opts.NX && opts.XX {
		return nil, fmt.Errorf("home kv: NX and XX are mutually exclusive")
	}

	args := []any{key, append([]byte(nil), value...)}
	if opts.EX > 0 {
		args = append(args, "EX", DurationCeil(opts.EX, time.Second))
	}
	if opts.PX > 0 {
		args = append(args, "PX", DurationCeil(opts.PX, time.Millisecond))
	}
	if opts.NX {
		args = append(args, "NX")
	}
	if opts.XX {
		args = append(args, "XX")
	}
	return args, nil
}


func DurationCeil(value time.Duration, unit time.Duration) int64 {
	if value <= 0 || unit <= 0 {
		return 0
	}
	q := int64(value / unit)
	if value%unit != 0 && q < math.MaxInt64 {
		q++
	}
	return q
}
