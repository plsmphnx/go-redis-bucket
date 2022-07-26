// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import "math"

type (
	// Backoff represents a scaling backoff function for repeated denials.
	Backoff interface {
		Backoff(float64) float64
	}

	// Constant represents a constant backoff factor.
	Constant float64

	// Linear represents a linear backoff factor.
	Linear float64

	// Power represents a power backoff factor.
	Power float64

	// Exponential represents an exponential backoff factor.
	Exponential float64
)

// Backoff computes a constant backoff value.
func (c Constant) Backoff(value float64) float64 {
	return float64(c)
}

// Backoff computes a linear backoff value.
func (l Linear) Backoff(value float64) float64 {
	return float64(l) * value
}

// Backoff computes a power backoff value.
func (p Power) Backoff(value float64) float64 {
	return math.Pow(value, float64(p))
}

// Backoff computes an exponential backoff value.
func (e Exponential) Backoff(value float64) float64 {
	return math.Pow(float64(e), value)
}
