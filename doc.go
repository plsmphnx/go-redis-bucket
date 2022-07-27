// A Redis-backed rate limiter, based on the leaky-bucket algorithm,
// implemented using a purely EVAL-based solution.
//
//	package main
//
//	import (
//		"context"
//		"net/http"
//		"strconv"
//		"time"
//
//		"github.com/go-redis/redis/v8"
//		limiter "github.com/plsmphnx/go-redis-bucket"
//	)
//
//	func main() {
//		// Create a Redis client with appropriate configuration and error handling.
//		r := redis.NewClient(&redis.Options{})
//
//		// Create a limiter that restricts calls to 10-20 per minute.
//		l, err := limiter.New(Redis{r}, limiter.Capacity{Window: time.Minute, Min: 10, Max: 20})
//		if err != nil {
//			panic(err)
//		}
//
//		// Simple handler, expects a "user" query parameter to identify callers.
//		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
//			res, err := l.Test(r.Context(), r.URL.Query().Get("user"), 1)
//			switch {
//			case err != nil:
//				w.WriteHeader(http.StatusInternalServerError)
//			case !res.Allow:
//				w.WriteHeader(http.StatusTooManyRequests)
//				w.Header().Set("Retry-After", strconv.Itoa(int(res.Wait.Seconds())))
//			default:
//				w.WriteHeader(http.StatusOK)
//			}
//		})
//		http.ListenAndServe(":80", nil)
//	}
//
//	type Redis struct{ *redis.Client }
//
//	func (r Redis) Eval(ctx context.Context, script string, keys []string, args []any) (any, error) {
//		return r.Client.Eval(ctx, script, keys, args...).Result()
//	}
//
//	func (r Redis) EvalSha(ctx context.Context, sha string, keys []string, args []any) (any, error) {
//		return r.Client.EvalSha(ctx, sha, keys, args...).Result()
//	}
package limiter
