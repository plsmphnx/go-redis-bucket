// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package limiter

import "math"

// WithConstantBackoff applies a constant backoff to the limiter.
func WithConstantBackoff(factor float64) Config {
	return func(c *config) {
		c.backoff = func(deny float64) float64 { return factor }
	}
}

// WithLinearBackoff applies a linear backoff to the limiter.
func WithLinearBackoff(factor float64) Config {
	return func(c *config) {
		c.backoff = func(deny float64) float64 { return factor * deny }
	}
}

// WithPowerBackoff applies a power backoff to the limiter.
func WithPowerBackoff(factor float64) Config {
	return func(c *config) {
		c.backoff = func(deny float64) float64 { return math.Pow(deny, factor) }
	}
}

// WithExponentialBackoff applies an exponential backoff to the limiter.
func WithExponentialBackoff(factor float64) Config {
	return func(c *config) {
		c.backoff = func(deny float64) float64 { return math.Pow(factor, deny) }
	}
}

// WithCustomBackoff applies a custom backoff to the limiter.
func WithCustomBackoff(backoff func(float64) float64) Config {
	return func(c *config) {
		c.backoff = backoff
	}
}
