// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	limiter "github.com/plsmphnx/go-redis-bucket"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

// The number of actions allowed in a burst (assuming one attempt per second,
// to make the simplifying assumption that actions and time are equivalent) is
// calculated from the burst capacity and the flow returned over that duration;
// it is the solution to the equation: time = burst + (time * flow)
func calcTime(bucket limiter.Bucket, bound float64) float64 {
	flow, burst := bucket.Rate()
	// If a zero bound applies to this calculation,
	// the flow rate is not applied to the first check.
	return bound + (burst-bound)/(1.0-flow)
}

// When in constant flow, the flow value determines the number of total attempts
// per allowed action.
func calcLoop(bucket limiter.Bucket) float64 {
	flow, _ := bucket.Rate()
	return 1.0 / flow
}

// When calculating remaining capacity, consider the number of used actions,
// and the amount of this metric that would have been returned over that time.
func calcLeft(bucket limiter.Bucket, used float64, bound float64) float64 {
	flow, burst := bucket.Rate()
	// If a zero bound applies to this calculation,
	// the flow rate is not applied to the first check.
	return burst - (bound + (used-bound)*(1.0-flow))
}

func TestBasicValidation(t *testing.T) {
	ctx := context.Background()
	f := setup(ctx, t)
	defer f.Done(ctx)

	// Fails with no client.
	capacity := limiter.Capacity{Window: time.Minute, Min: 10, Max: 20}
	_, err := limiter.NewLimiter(limiter.Config{
		Buckets: []limiter.Bucket{capacity},
	})
	assert.Error(t, err)

	// Fails with no buckets.
	_, err = limiter.NewLimiter(limiter.Config{
		Redis: f,
	})
	assert.Error(t, err)

	// Fails with a negative capacity window.
	capacity = limiter.Capacity{Window: -time.Minute, Min: 10, Max: 20}
	_, err = limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{capacity},
	})
	assert.Error(t, err)

	// Fails with no buffer between min and max.
	capacity = limiter.Capacity{Window: time.Minute, Min: 10, Max: 10}
	_, err = limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{capacity},
	})
	assert.Error(t, err)

	// Fails with a zero rate metric.
	rate := limiter.Rate{Flow: 1, Burst: 0}
	_, err = limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{rate},
	})
	assert.Error(t, err)
}

func TestBasicCapacityMetrics(t *testing.T) {
	ctx := context.Background()
	f := setup(ctx, t)
	defer f.Done(ctx)

	capacity := limiter.Capacity{Window: time.Minute, Min: 10, Max: 20}
	l, err := limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{capacity},
	})
	assert.NoError(t, err)

	// Perform test twice, for burst and steady-state near capacity.
	for i := 0; i < 2; i++ {
		base := f.Now()
		var allowed int

		// Expend capacity for the duration of the window.
		for f.Now() < base+capacity.Window.Seconds() {
			res, err := l.Test(ctx, f.Key(), 1)
			assert.NoError(t, err)
			if res.Allow {
				allowed++
			}
			f.Sleep(ctx, 1)
		}

		// Capacity should be within the bounds.
		assert.GreaterOrEqual(t, float64(allowed), capacity.Min)
		assert.LessOrEqual(t, float64(allowed), capacity.Max)
	}
}

func TestBasicRateMetrics(t *testing.T) {
	ctx := context.Background()
	f := setup(ctx, t)
	defer f.Done(ctx)

	rate := limiter.Rate{Burst: 9, Flow: 1.0 / 2.0}
	l, err := limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{rate},
	})
	assert.NoError(t, err)

	// Perform test twice to ensure full drain.
	for i := 0; i < 2; i++ {
		base := f.Now()
		free := rate.Burst - 1

		// Expect initial burst to be allowed.
		time := calcTime(rate, 1)
		for f.Now() < base+time {
			res, err := l.Test(ctx, f.Key(), 1)
			assert.NoError(t, err)
			assert.Equal(t, res, limiter.Result{Allow: true, Free: free})

			f.Sleep(ctx, 1)
			free += rate.Flow - 1
		}

		// Expect steady-state of flow rate near capacity.
		loop := calcLoop(rate)
		for f.Now() < base+time+loop*4 {
			var allowed int
			for n := 0.0; n < loop; n++ {
				res, err := l.Test(ctx, f.Key(), 1)
				assert.NoError(t, err)
				if res.Allow {
					allowed++
				}
				f.Sleep(ctx, 1)
			}
			assert.Equal(t, allowed, 1)
		}

		// Once the flow would return the full burst capacity,
		// behavior should be reset to baseline.
		f.Sleep(ctx, rate.Burst/rate.Flow)
	}
}

func TestMultipleRates(t *testing.T) {
	ctx := context.Background()
	f := setup(ctx, t)
	defer f.Done(ctx)

	slow := limiter.Rate{Burst: 18.0, Flow: 1.0 / 4.0}
	fast := limiter.Rate{Burst: 9.0, Flow: 1.0 / 2.0}
	l, err := limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{slow, fast},
		Backoff: limiter.Exponential(2.0),
	})
	assert.NoError(t, err)

	base := f.Now()
	free := fast.Burst - 1.0

	// Expect initial burst to be allowed.
	timeFast := calcTime(fast, 1)
	for f.Now() < base+timeFast {
		res, err := l.Test(ctx, f.Key(), 1)
		assert.NoError(t, err)
		assert.Equal(t, res, limiter.Result{Allow: true, Free: free})

		f.Sleep(ctx, 1)
		free += fast.Flow - 1
	}

	// Expect fast flow rate until slow burst is consumed.
	loopFast := calcLoop(fast)
	timeSlow := calcTime(limiter.Rate{
		Burst: calcLeft(slow, timeFast, 1),
		Flow:  slow.Flow / fast.Flow,
	}, 0) * loopFast
	for f.Now() < base+timeFast+timeSlow {
		var allowed int
		for n := 0.0; n < loopFast; n++ {
			res, err := l.Test(ctx, f.Key(), 1)
			assert.NoError(t, err)
			if res.Allow {
				allowed++
			}
			f.Sleep(ctx, 1)
		}
		assert.Equal(t, allowed, 1)
	}

	// Expect slow flow rate afterwards.
	loopSlow := calcLoop(slow)
	var wait time.Duration
	for f.Now() < base+timeFast+timeSlow+loopSlow*4 {
		var allowed int
		for n := 0.0; n < loopSlow; n++ {
			res, err := l.Test(ctx, f.Key(), 1)
			assert.NoError(t, err)
			if res.Allow {
				allowed++
			} else if wait > 0 {
				assert.Equal(t, res.Wait, 2*wait)
			}
			wait = res.Wait
			f.Sleep(ctx, 1)
		}
		assert.Equal(t, allowed, 1)
	}

	f.Done(ctx)
}

func TestSubsecondDeltas(t *testing.T) {
	ctx := context.Background()
	f := setup(ctx, t)
	defer f.Done(ctx)

	capacity := limiter.Capacity{Window: time.Second, Min: 4, Max: 5}
	l, err := limiter.NewLimiter(limiter.Config{
		Redis:   f,
		Buckets: []limiter.Bucket{capacity},
	})
	assert.NoError(t, err)

	base := f.Now()
	var allowed int

	// Expend capacity for the duration of the window.
	for f.Now() < base+capacity.Window.Seconds() {
		res, err := l.Test(ctx, f.Key(), 1)
		assert.NoError(t, err)
		if res.Allow {
			allowed++
		}
		f.Sleep(ctx, 0.125)
	}

	// Capacity should be within the bounds.
	assert.GreaterOrEqual(t, float64(allowed), capacity.Min)
	assert.LessOrEqual(t, float64(allowed), capacity.Max)
}

type superfluousRateTester struct{ *testing.T }

func (t superfluousRateTester) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
	assert.Equal(t, args, []any{1.0, 0.1, 4.0, 0.2, 2.0, 0.4, 1.0})
	return []any{int64(1), "1", int64(1)}, nil
}

func TestSuperfluousRates(t *testing.T) {
	l, err := limiter.NewLimiter(limiter.Config{
		Redis: superfluousRateTester{t},
		Buckets: []limiter.Bucket{
			limiter.Rate{Burst: 4, Flow: 0.1}, // 1 - Valid
			limiter.Rate{Burst: 3, Flow: 0.2}, // 2 - Strictly larger than 3
			limiter.Rate{Burst: 2, Flow: 0.2}, // 3 - Valid
			limiter.Rate{Burst: 2, Flow: 0.3}, // 4 - Strictly larger than 3
			limiter.Rate{Burst: 1, Flow: 0.4}, // 5 - Valid
		},
	})
	assert.NoError(t, err)

	_, err = l.Test(context.Background(), "key", 1)
	assert.NoError(t, err)
}

type errorPassingTester struct{ *testing.T }

func (t errorPassingTester) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
	assert.Fail(t, "Should not reach EVAL")
	return nil, nil
}

func (t errorPassingTester) EvalSha(ctx context.Context, sha string, keys []string, args []any) (any, error) {
	return nil, t
}

func (t errorPassingTester) Error() string {
	return "unknown error"
}

func TestErrorPassing(t *testing.T) {
	error := errorPassingTester{t}
	l, err := limiter.NewLimiter(limiter.Config{
		Redis:   error,
		Buckets: []limiter.Bucket{limiter.Rate{Burst: 4, Flow: 0.1}},
	})
	assert.NoError(t, err)

	_, err = l.Test(context.Background(), "key", 1)
	assert.ErrorIs(t, err, error)
}

func TestScaling(t *testing.T) {
	factor := 2.0
	denied := 3.0
	for _, test := range []struct {
		backoff limiter.Backoff
		result  float64
	}{
		{limiter.Constant(factor), 2.0},
		{limiter.Linear(factor), 6.0},
		{limiter.Power(factor), 9.0},
		{limiter.Exponential(factor), 8.0},
	} {
		assert.Equal(t, test.backoff.Backoff(denied), test.result)
	}
}

// Test framework, which also serves as the Redis limiter.Client implementation.
type framework struct {
	redis   *redis.Client
	seconds float64
	time    string
	key     string
}

func setup(ctx context.Context, t *testing.T) *framework {
	f := &framework{
		redis:   redis.NewClient(&redis.Options{}),
		seconds: 1,
		time:    "redis-bucket-test:time:" + t.Name(),
		key:     "redis-bucket-test:key:" + t.Name(),
	}
	f.redis.LPush(ctx, f.time, 0, 1)
	return f
}

func (f *framework) Key() string {
	return f.key
}

func (f *framework) Now() float64 {
	return f.seconds
}

func (f *framework) Sleep(ctx context.Context, s float64) {
	f.seconds += s
	full, part := math.Modf(f.seconds)
	f.redis.LPush(ctx, f.time, int(math.Floor(part*1e6)), int(full))
}

func (f *framework) Done(ctx context.Context) {
	f.redis.Del(ctx, f.key, f.time)
}

func (f *framework) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
	// Patch the script, using a list to perform a mock of the time function.
	script = strings.Replace(script, "'time'", "'lrange','"+f.time+"',0,1", -1)
	return f.redis.Eval(ctx, script, keys, args...).Result()
}

func (f *framework) EvalSha(ctx context.Context, sha string, keys []string, args []any) (any, error) {
	// This will always fail since the hash will not match the patched script,
	// but it validates the fallback path.
	return f.redis.EvalSha(ctx, sha, keys, args...).Result()
}
