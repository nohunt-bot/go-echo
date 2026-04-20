package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	maxRetries  = 3
	baseBackoff = 100 * time.Millisecond
	maxBackoff  = 1 * time.Second
)

// withRetry executes fn up to maxRetries+1 times with exponential backoff.
// Stops early if the error is not retryable or the context is done.
func withRetry(ctx context.Context, fn func() error) error {
	backoff := baseBackoff
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil || !isRetryable(lastErr) {
			return lastErr
		}
		if attempt == maxRetries {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
	return fmt.Errorf("redis: failed after %d retries: %w", maxRetries, lastErr)
}

// isRetryable returns true for transient network/connection errors.
// redis.Nil (key not found) and context errors are not retryable.
func isRetryable(err error) bool {
	return err != nil &&
		!errors.Is(err, goredis.Nil) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}
