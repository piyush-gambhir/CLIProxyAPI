package cliproxy

import "time"

const (
	homeSubscriberRetryInitial = time.Second
	homeSubscriberRetryMaximum = 30 * time.Second
	homeSubscriberStablePeriod = 30 * time.Second
)



type homeSubscriberRetryBackoff struct {
	delay time.Duration
}

func (b *homeSubscriberRetryBackoff) next(healthyFor time.Duration, plannedRebalance bool, random float64) time.Duration {
	if healthyFor >= homeSubscriberStablePeriod {
		b.delay = 0
	}
	if plannedRebalance {
		return 0
	}
	if b.delay == 0 {
		b.delay = homeSubscriberRetryInitial
	} else {
		b.delay = min(b.delay*2, homeSubscriberRetryMaximum)
	}

	return b.delay/2 + time.Duration(random*float64(b.delay/2))
}
