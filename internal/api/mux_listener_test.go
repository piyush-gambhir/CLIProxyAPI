package api

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestMuxListener_PutAfterClose(t *testing.T) {
	l := newMuxListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, 16)
	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for i := 0; i < 500; i++ {
		c1, c2 := net.Pipe()
		err := l.Put(c1)
		_ = c2.Close()
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Put after Close succeeded or returned unexpected error: %v, want %v", err, net.ErrClosed)
		}
	}
}

func TestMuxListener_CloseDrainsAndClosesQueuedConnections(t *testing.T) {
	l := newMuxListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, 16)
	c1, c2 := net.Pipe()
	if err := l.Put(c1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}


	buf := make([]byte, 1)
	_ = c2.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, errRead := c2.Read(buf)
	if errRead == nil {
		t.Fatalf("expected queued net.Pipe peer to observe connection close after listener Close")
	}


	_, errAccept := l.Accept()
	if !errors.Is(errAccept, net.ErrClosed) {
		t.Fatalf("Accept after Close returned %v, want %v", errAccept, net.ErrClosed)
	}
}

func TestMuxListener_ConcurrentBlockedPutDuringClose(t *testing.T) {
	const bufferSize = 2
	const numGoroutines = 16
	l := newMuxListener(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080}, bufferSize)


	for i := 0; i < bufferSize; i++ {
		c1, c2 := net.Pipe()
		defer func() {
			_ = c1.Close()
			_ = c2.Close()
		}()
		if err := l.Put(c1); err != nil {
			t.Fatalf("initial Put failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	putErrors := make([]error, numGoroutines)
	started := make(chan struct{})
	var readyCount sync.WaitGroup
	readyCount.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c1, c2 := net.Pipe()
			defer func() {
				_ = c1.Close()
				_ = c2.Close()
			}()
			readyCount.Done()
			<-started
			putErrors[idx] = l.Put(c1)
		}(i)
	}

	readyCount.Wait()
	close(started)


	if err := l.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	wg.Wait()

	for idx, err := range putErrors {
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("goroutine %d Put error = %v, want %v", idx, err, net.ErrClosed)
		}
	}


	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close returned %v, want %v", err, net.ErrClosed)
	}
}
