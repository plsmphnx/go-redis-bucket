// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"
)

type (
	// Config provides configuration values for creating a new rate-limiter.
	Config struct {
		// Redis is the Redis client for this rate-limiter.
		Redis Eval

		// Prefix is a string that will be added to the beginning of all keys.
		Prefix string

		// Backoff is the backoff scaling function (default 2x linear).
		Backoff Backoff

		// Buckets are the rate-limiting metrics.
		Buckets []Bucket
	}

	// Limiter provides a single rate-limiter instance.
	Limiter struct {
		args    []any
		redis   Eval
		prefix  string
		backoff Backoff
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

// NewLimiter creates a new rate-limiter instance.
func NewLimiter(c Config) (*Limiter, error) {
	if c.Redis == nil {
		return nil, errors.New("the limiter must have a redis client")
	}

	if len(c.Buckets) == 0 {
		return nil, errors.New("the limiter must have provided rates")
	}

	if c.Backoff == nil {
		c.Backoff = Linear(2)
	}

	// Resolve all bucket metrics.
	rates := make([]struct{ flow, burst float64 }, len(c.Buckets))
	for i, b := range c.Buckets {
		rates[i].flow, rates[i].burst = b.Rate()
		if rates[i].flow <= 0 || rates[i].burst <= 0 {
			return nil, errors.New("all rate parameters must be positive")
		}
	}

	// Sort rates by the slowest to fastest flow for consistency, or by burst
	// if flow is the same (to make them easier to filter out later).
	sort.Slice(rates, func(i int, j int) bool {
		if rates[i].flow != rates[j].flow {
			return rates[i].flow < rates[j].flow
		}
		return rates[i].burst < rates[j].burst
	})

	args := []any{rates[0].flow, rates[0].burst}
	for _, r := range rates[1:] {
		// Any limit that is strictly larger than another is superfluous,
		// as the smaller limit will always be more restrictive.
		if r.burst < args[len(args)-1].(float64) {
			args = append(args, r.flow, r.burst)
		}
	}

	return &Limiter{
		args:    args,
		redis:   c.Redis,
		prefix:  c.Prefix,
		backoff: c.Backoff,
	}, nil
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
		wait := (cost / flow) * l.backoff.Backoff(value/cost)
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
	err = errors.New("invalid type returned from eval")
	return
}
