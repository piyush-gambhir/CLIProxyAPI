package usage

import (
	"context"
	"sync"
	"sync/atomic"
)

type streamDeliveryKey struct{}

type streamDelivery struct {
	supported atomic.Int32
	once      sync.Once
	done      chan struct{}
	err       error
}



func WithStreamDelivery(ctx context.Context) (context.Context, func(error)) {
	delivery := &streamDelivery{done: make(chan struct{})}
	return context.WithValue(ctx, streamDeliveryKey{}, delivery), func(err error) {
		delivery.once.Do(func() {
			delivery.err = err
			close(delivery.done)
		})
	}
}




func WithoutStreamDelivery(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, streamDeliveryKey{}, struct{}{})
}


func StreamDeliveryTracked(ctx context.Context) bool {
	_, ok := ctx.Value(streamDeliveryKey{}).(*streamDelivery)
	return ok
}



func WaitStreamDelivery(ctx context.Context) (error, bool) {
	delivery, ok := ctx.Value(streamDeliveryKey{}).(*streamDelivery)
	if !ok {
		return nil, false
	}
	<-delivery.done
	return delivery.err, true
}



func SupportStreamDelivery(ctx context.Context) func() {
	delivery, ok := ctx.Value(streamDeliveryKey{}).(*streamDelivery)
	if !ok {
		return func() {}
	}
	delivery.supported.Add(1)
	var once sync.Once
	return func() { once.Do(func() { delivery.supported.Add(-1) }) }
}


func StreamDeliverySupported(ctx context.Context) bool {
	delivery, ok := ctx.Value(streamDeliveryKey{}).(*streamDelivery)
	return ok && delivery.supported.Load() > 0
}
