// Modified Go translation for navmesh-go; see PROVENANCE.md.
// Copyright (c) 2009 Mikko Mononen memon@inside.org
//
// This software is provided 'as-is', without any express or implied
// warranty.  In no event will the authors be held liable for any damages
// arising from the use of this software.
//
// Permission is granted to anyone to use this software for any purpose,
// including commercial applications, and to alter it and redistribute it
// freely, subject to the following restrictions:
//
// 1. The origin of this software must not be misrepresented; you must not
// claim that you wrote the original software. If you use this software
// in a product, an acknowledgment in the product documentation would be
// appreciated but is not required.
// 2. Altered source versions must be plainly marked as such, and must not be
// misrepresented as being the original software.
// 3. This notice may not be removed or altered from any source distribution.
//

package recast

import (
	"context"
	"sync"
	"time"
)

type metricsContextKeyType struct{}

var metricsContextKey = metricsContextKeyType{}

// PerfMetrics accumulates timings; use one instance per build to avoid overlapping labels.
type PerfMetrics struct {
	mu         sync.Mutex
	timers     map[TimerLabel]time.Duration
	timerStart map[TimerLabel]time.Time
}

// NewPerfMetrics returns an empty timing collector.
func NewPerfMetrics() *PerfMetrics {
	return &PerfMetrics{
		timers:     make(map[TimerLabel]time.Duration),
		timerStart: make(map[TimerLabel]time.Time),
	}
}

// WithMetrics associates a timing collector with a build context.
func WithMetrics(ctx context.Context, metrics *PerfMetrics) context.Context {
	return context.WithValue(ctx, metricsContextKey, metrics)
}

// GetMetrics obtains the collector associated with a context.
func GetMetrics(ctx context.Context) (*PerfMetrics, bool) {
	m, ok := ctx.Value(metricsContextKey).(*PerfMetrics)
	return m, ok
}

// ScopedTimer starts a timer and returns its completion function.
func ScopedTimer(ctx context.Context, label TimerLabel) func() {
	metrics, ok := ctx.Value(metricsContextKey).(*PerfMetrics)
	if !ok || metrics == nil {
		return func() {}
	}

	start := time.Now()

	return func() {
		cost := time.Since(start)

		metrics.mu.Lock()
		metrics.timers[label] += cost
		metrics.mu.Unlock()
	}
}

// StartTimer starts the named stage timer.
func StartTimer(ctx context.Context, label TimerLabel) {
	metrics, ok := ctx.Value(metricsContextKey).(*PerfMetrics)
	if !ok || metrics == nil {
		return
	}

	metrics.mu.Lock()
	metrics.timerStart[label] = time.Now()
	metrics.mu.Unlock()
}

// StopTimer records elapsed time for a previously started stage timer.
func StopTimer(ctx context.Context, label TimerLabel) {
	metrics, ok := ctx.Value(metricsContextKey).(*PerfMetrics)
	if !ok || metrics == nil {
		return
	}

	metrics.mu.Lock()
	if start, ok := metrics.timerStart[label]; ok {
		metrics.timers[label] += time.Since(start)
		delete(metrics.timerStart, label)
	}
	metrics.mu.Unlock()
}

// GetAccumulatedTime reads a stage duration under the collector lock.
func GetAccumulatedTime(ctx context.Context, label TimerLabel) (time.Duration, bool) {
	metrics, ok := ctx.Value(metricsContextKey).(*PerfMetrics)
	if !ok || metrics == nil {
		return 0, false
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()

	d, ok := metrics.timers[label]
	return d, ok
}
