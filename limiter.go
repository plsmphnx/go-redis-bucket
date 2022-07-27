// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package limiter

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"
)

type (
	// Config provides configuration values for creating a new rate-limiter.
	Config func(*config)

	config struct {
		rates   []Rate
		prefix  string
		backoff func(float64) float64
	}

	// Limiter provides a single rate-limiter instance.
	Limiter struct {
		args    []any
		redis   Eval
		prefix  string
		backoff func(float64) float64
	}

	// Result provides the result of a rate-limiting test.
	Result struct {
		// Allow indicates whether the request should be allowed.
		Allow bool

		// Free indicates the remaining capacity before calls will be rejected.
		Free float64

		// Wait indicates how long the caller should wait before trying again.
		Wait time.Duration
	}
)

// WithPrefix adds the given string to the beginning of all keys.
func WithPrefix(prefix string) Config {
	return func(c *config) { c.prefix = prefix }
}

// New creates a new rate-limiter instance.
func New(redis Eval, bucket Bucket, configs ...Config) (*Limiter, error) {
	if redis == nil {
		return nil, errors.New("limiter: must have a redis client")
	}

	c := &config{}
	WithLinearBackoff(2)(c)
	WithAdditionalBucket(bucket)(c)
	for _, cfg := range configs {
		cfg(c)
	}

	// Sort rates by the slowest to fastest flow for consistency, or by burst
	// if flow is the same (to make them easier to filter out later).
	sort.Slice(c.rates, func(i int, j int) bool {
		if c.rates[i].Flow != c.rates[j].Flow {
			return c.rates[i].Flow < c.rates[j].Flow
		}
		return c.rates[i].Burst < c.rates[j].Burst
	})

	// Turn the rate parameters into appropriate arguments for the Lua script.
	args := []any{c.rates[0].Flow, c.rates[0].Burst}
	for _, r := range c.rates[1:] {
		// Any limit that is strictly larger than another is superfluous,
		// as the smaller limit will always be more restrictive.
		if r.Burst < args[len(args)-1].(float64) {
			args = append(args, r.Flow, r.Burst)
		}
	}

	for _, arg := range args {
		if arg.(float64) <= 0 {
			return nil, errors.New("limiter: rate parameters must be positive")
		}
	}

	return &Limiter{args, redis, c.prefix, c.backoff}, nil
}

// Test whether the given action should be allowed according to the rate limits.
func (l *Limiter) Test(ctx context.Context, key string, cost float64) (Result, error) {
	keys := []string{l.prefix + key}

	args := make([]any, len(l.args)+1)
	args[0] = cost
	copy(args[1:], l.args)

	raw, err := exec(ctx, l.redis, keys, args)
	if err != nil {
		return Result{}, err
	}

	allow, value, index, err := validate(raw)
	if err != nil {
		return Result{}, err
	}

	if allow == 1 {
		return Result{Allow: true, Free: value}, nil
	} else {
		flow := args[2*index-1].(float64)
		wait := (cost / flow) * l.backoff(value/cost)
		return Result{Allow: false, Wait: time.Duration(wait * float64(time.Second))}, nil
	}
}

func validate(raw any) (allow int64, value float64, index int64, err error) {
	if res, ok := raw.([]any); ok && len(res) == 3 {
		if allow, ok = res[0].(int64); ok {
			if val, ok := res[1].(string); ok {
				if value, err = strconv.ParseFloat(val, 64); err == nil {
					if index, ok = res[2].(int64); ok {
						return
					}
				}
			}
		}
	}
	err = errors.New("limiter: invalid type returned from eval")
	return
}
