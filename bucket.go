// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package limiter

import "time"

type (
	// Bucket represents a single set of rate-limiting parameters that can be
	// described with leaky-bucket flow (per second) and burst values.
	Bucket interface {
		Rate() (flow float64, burst float64)
	}

	// Rate describes a bucket using raw flow (per second) and burst values.
	Rate struct {
		// Flow is the rate at which capacity becomes available, per second. In
		// a fully-stressed system, calls will be limited to exactly this rate.
		Flow float64

		// Burst is the amount of leeway in capacity the system can support.
		// This is the amount of capacity that can be utilized before rate-
		// limiting is applied. It must be at least equal to the highest cost
		// which will be tested.
		Burst float64
	}

	// Capacity describes a bucket using a minimum and maximum over a window.
	Capacity struct {
		// Window is the time window over which these limits are considered.
		Window time.Duration

		// Min is the minimum capacity that is guaranteed over this time window,
		// assuming a perfectly uniform call pattern.
		Min float64

		// Max is the maximum capacity that the system can handle over this
		// time window. This value is absolute; callers will be limited to
		// enforce this. It must be sufficiently greater than the minimum
		// capacity to cover the highest cost which will be tested.
		Max float64
	}
)

// Rate returns the flow and burst parameters for a Rate bucket.
func (r Rate) Rate() (float64, float64) {
	return r.Flow, r.Burst
}

// Rate returns the flow and burst parameters for a Capacity bucket.
func (c Capacity) Rate() (float64, float64) {
	return c.Min / c.Window.Seconds(), c.Max - c.Min
}

// WithAdditionalBucket adds an additional rate-limiting bucket to the limiter.
func WithAdditionalBucket(bucket Bucket) Config {
	return func(c *config) {
		flow, burst := bucket.Rate()
		c.rates = append(c.rates, Rate{flow, burst})
	}
}
