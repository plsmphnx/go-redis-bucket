// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package limiter

import (
	"context"
	_ "embed"
	"strings"
)

type (
	// Eval represents a Redis client supporting EVAL.
	Eval interface {
		Eval(ctx context.Context, script string, keys []string, args []any) (any, error)
	}

	// EvalSha represents a Redis client supporting EVALSHA.
	EvalSha interface {
		EvalSha(ctx context.Context, sha string, keys []string, args []any) (any, error)
	}
)

//go:embed script/bucket.min.lua
var script string

//go:embed script/bucket.min.lua.sha1
var sha1 string

func exec(ctx context.Context, eval Eval, keys []string, args []any) (any, error) {
	if evalsha, ok := eval.(EvalSha); ok {
		res, err := evalsha.EvalSha(ctx, sha1, keys, args)
		if err == nil || !strings.Contains(err.Error(), "NOSCRIPT") {
			return res, err
		}
	}
	return eval.Eval(ctx, script, keys, args)
}
