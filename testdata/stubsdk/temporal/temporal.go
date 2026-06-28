// Package temporal is a minimal stub of go.temporal.io/sdk/temporal for
// hermetic analyzer tests. It is NOT the real SDK — only the package path and
// the signatures the analyzer keys on matter.
package temporal

import "time"

// RetryPolicy mirrors the real type's shape closely enough to compile.
type RetryPolicy struct {
	InitialInterval    time.Duration
	BackoffCoefficient float64
	MaximumInterval    time.Duration
	MaximumAttempts    int32
}
