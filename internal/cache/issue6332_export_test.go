package cache

import (
	"sync/atomic"
	"testing"
)

func Issue6332BlockCommit(t *testing.T) (<-chan struct{}, chan<- struct{}, func() int32) {
	t.Helper()
	original := currentAntigravityReasoningReplayKVClient
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	currentAntigravityReasoningReplayKVClient = func() (antigravityReasoningReplayKVClient, bool, error) {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		return original()
	}
	t.Cleanup(func() { currentAntigravityReasoningReplayKVClient = original })
	return entered, release, calls.Load
}
