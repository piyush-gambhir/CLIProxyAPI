package helps

import (
	"container/list"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)







const DefaultTransportCacheCapacity = 64








type TransportCache[K comparable] struct {
	mu       sync.Mutex
	capacity int

	order *list.List
	items map[K]*list.Element
}

type transportCacheEntry[K comparable] struct {
	key       K
	transport *http.Transport
	retired   atomic.Bool
}








func attachTransportRetirementGuard(transport *http.Transport, retired *atomic.Bool) {
	if transport == nil || retired == nil {
		return
	}
	dial := transport.DialContext
	if dial == nil && transport.DialTLSContext == nil && transport.Dial == nil && transport.DialTLS == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	if dial != nil {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if retired.Load() {
				transport.CloseIdleConnections()
			}
			return conn, nil
		}
	}
	if dialTLS := transport.DialTLSContext; dialTLS != nil {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialTLS(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if retired.Load() {
				transport.CloseIdleConnections()
			}
			return conn, nil
		}
	}
}



func NewTransportCache[K comparable](capacity int) *TransportCache[K] {
	if capacity <= 0 {
		capacity = DefaultTransportCacheCapacity
	}
	return &TransportCache[K]{
		capacity: capacity,
		order:    list.New(),
		items:    make(map[K]*list.Element, capacity),
	}
}







func (c *TransportCache[K]) Get(key K, build func() (*http.Transport, error)) (*http.Transport, error) {
	if c == nil {
		return nil, errors.New("transport cache: nil cache")
	}
	if build == nil {
		return nil, errors.New("transport cache: nil build function")
	}

	c.mu.Lock()
	locked := true
	defer func() {
		if locked {
			c.mu.Unlock()
		}
	}()
	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		transport := element.Value.(*transportCacheEntry[K]).transport
		return transport, nil
	}

	transport, errBuild := build()
	if errBuild != nil {
		return nil, errBuild
	}
	if transport == nil {
		return nil, errors.New("transport cache: build returned no transport")
	}


	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		existing := element.Value.(*transportCacheEntry[K]).transport
		locked = false
		c.mu.Unlock()
		transport.CloseIdleConnections()
		return existing, nil
	}

	entry := &transportCacheEntry[K]{key: key, transport: transport}
	attachTransportRetirementGuard(transport, &entry.retired)
	c.items[key] = c.order.PushFront(entry)
	evicted := c.evictLocked()
	locked = false
	c.mu.Unlock()

	for _, t := range evicted {
		t.CloseIdleConnections()
	}
	return transport, nil
}






func (c *TransportCache[K]) evictLocked() []*http.Transport {
	var evicted []*http.Transport
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		entry := oldest.Value.(*transportCacheEntry[K])
		entry.retired.Store(true)
		delete(c.items, entry.key)
		evicted = append(evicted, entry.transport)
	}
	return evicted
}


func (c *TransportCache[K]) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}


func (c *TransportCache[K]) Contains(key K) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.items[key]
	return ok
}



func (c *TransportCache[K]) CloseKey(key K) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	element, ok := c.items[key]
	if !ok {
		c.mu.Unlock()
		return false
	}
	c.order.Remove(element)
	delete(c.items, key)
	entry := element.Value.(*transportCacheEntry[K])
	entry.retired.Store(true)
	transport := entry.transport
	c.mu.Unlock()

	transport.CloseIdleConnections()
	return true
}



func (c *TransportCache[K]) detachMatching(predicate func(key K) bool) []*http.Transport {
	if c == nil || predicate == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var toClose []*http.Transport
	var next *list.Element
	for element := c.order.Front(); element != nil; element = next {
		next = element.Next()
		entry := element.Value.(*transportCacheEntry[K])
		if predicate(entry.key) {
			c.order.Remove(element)
			entry.retired.Store(true)
			delete(c.items, entry.key)
			toClose = append(toClose, entry.transport)
		}
	}
	return toClose
}



func (c *TransportCache[K]) CloseMatching(predicate func(key K) bool) int {
	toClose := c.detachMatching(predicate)
	for _, t := range toClose {
		t.CloseIdleConnections()
	}
	return len(toClose)
}


func (c *TransportCache[K]) Purge() {
	if c == nil {
		return
	}
	c.mu.Lock()
	var toClose []*http.Transport
	for element := c.order.Front(); element != nil; element = element.Next() {
		entry := element.Value.(*transportCacheEntry[K])
		entry.retired.Store(true)
		toClose = append(toClose, entry.transport)
	}
	c.order.Init()
	c.items = make(map[K]*list.Element, c.capacity)
	c.mu.Unlock()

	for _, t := range toClose {
		t.CloseIdleConnections()
	}
}
