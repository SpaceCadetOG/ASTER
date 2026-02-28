package ratelimit

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/time/rate"
)

// Limiter wraps golang.org/x/time/rate with convenience methods.
type Limiter struct {
	l *rate.Limiter
}

// New creates a Limiter allowing reqPerSec requests/second with a burst of burst.
func New(reqPerSec int, burst int) *Limiter {
	return &Limiter{l: rate.NewLimiter(rate.Limit(reqPerSec), burst)}
}

// Wait blocks until a token is available or ctx is cancelled.
func (lim *Limiter) Wait(ctx context.Context) error {
	return lim.l.Wait(ctx)
}

// WaitTimeout blocks up to d before returning an error.
func (lim *Limiter) WaitTimeout(d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	if err := lim.l.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}
	return nil
}
