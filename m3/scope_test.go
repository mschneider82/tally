// Copyright (c) 2021 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package m3

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tally "github.com/uber-go/tally/v4"
)

var commonTags = map[string]string{"env": "test"}

type doneFn func()

func newTestReporterScope(
	t *testing.T,
	addr string,
	scopePrefix string,
	scopeTags map[string]string,
) (Reporter, tally.Scope, doneFn) {
	r, err := NewReporter(Options{
		HostPorts:          []string{addr},
		Service:            "testService",
		CommonTags:         commonTags,
		IncludeHost:        includeHost,
		MaxQueueSize:       queueSize,
		MaxPacketSizeBytes: maxPacketSize,
	})
	require.NoError(t, err)

	scope, closer := tally.NewRootScope(tally.ScopeOptions{
		Prefix:         scopePrefix,
		Tags:           scopeTags,
		CachedReporter: r,
	}, shortInterval)

	return r, scope, func() {
		require.NoError(t, closer.Close())
		require.True(t, r.(*reporter).done.Load())
	}
}

// TestScope tests that scope works as expected
func TestScope(t *testing.T) {
	var wg sync.WaitGroup
	server := newFakeM3Server(t, &wg, true, Compact)
	go server.Serve()
	defer server.Close()

	tags := map[string]string{"testTag": "TestValue", "testTag2": "TestValue2"}

	_, scope, close := newTestReporterScope(t, server.Addr, "honk", tags)
	wg.Add(1)

	timer := scope.Timer("dazzle")
	timer.Start().Stop()
	close()

	wg.Wait()

	require.Equal(t, 1, len(server.Service.getBatches()))
	require.NotNil(t, server.Service.getBatches()[0])

	emittedTimers := server.Service.getBatches()[0].GetMetrics()
	require.Equal(t, internalMetrics+cardinalityMetrics+1, len(emittedTimers))
	require.Equal(t, "honk.dazzle", emittedTimers[0].GetName())
}

// TestScopeCounter tests that scope works as expected
func TestScopeCounter(t *testing.T) {
	var wg sync.WaitGroup
	server := newFakeM3Server(t, &wg, true, Compact)
	go server.Serve()
	defer server.Close()

	tags := map[string]string{"testTag": "TestValue", "testTag2": "TestValue2"}

	_, scope, close := newTestReporterScope(t, server.Addr, "honk", tags)

	wg.Add(1)
	counter := scope.Counter("foobar")
	counter.Inc(42)
	close()
	wg.Wait()

	require.Equal(t, 1, len(server.Service.getBatches()))
	require.NotNil(t, server.Service.getBatches()[0])

	emittedMetrics := server.Service.getBatches()[0].GetMetrics()
	require.Equal(t, internalMetrics+cardinalityMetrics+1, len(emittedMetrics))
	require.Equal(t, "honk.foobar", emittedMetrics[cardinalityMetrics].GetName())
}

// TestScopeGauge tests that scope works as expected
func TestScopeGauge(t *testing.T) {
	var wg sync.WaitGroup
	server := newFakeM3Server(t, &wg, true, Compact)
	go server.Serve()
	defer server.Close()

	tags := map[string]string{"testTag": "TestValue", "testTag2": "TestValue2"}

	_, scope, close := newTestReporterScope(t, server.Addr, "honk", tags)

	wg.Add(1)
	gauge := scope.Gauge("foobaz")
	gauge.Update(42)
	close()
	wg.Wait()

	require.Equal(t, 1, len(server.Service.getBatches()))
	require.NotNil(t, server.Service.getBatches()[0])

	emittedMetrics := server.Service.getBatches()[0].GetMetrics()
	require.Equal(t, internalMetrics+cardinalityMetrics+1, len(emittedMetrics))
	require.Equal(t, "honk.foobaz", emittedMetrics[cardinalityMetrics].GetName())
}

func BenchmarkScopeReportTimer(b *testing.B) {
	backend, err := NewReporter(Options{
		HostPorts:          []string{"127.0.0.1:4444"},
		Service:            "my-service",
		MaxQueueSize:       10000,
		MaxPacketSizeBytes: maxPacketSize,
	})
	if err != nil {
		b.Error(err.Error())
		return
	}

	scope, closer := tally.NewRootScope(tally.ScopeOptions{
		Prefix:         "bench",
		CachedReporter: backend,
	}, 1*time.Second)

	perEndpointScope := scope.Tagged(
		map[string]string{
			"endpointid": "health",
			"handlerid":  "health",
		},
	)
	timer := perEndpointScope.Timer("inbound.latency")
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			timer.Record(500)
		}
	})

	b.StopTimer()
	closer.Close()
	b.StartTimer()
}

func BenchmarkScopeReportHistogram(b *testing.B) {
	backend, err := NewReporter(Options{
		HostPorts:          []string{"127.0.0.1:4444"},
		Service:            "my-service",
		MaxQueueSize:       10000,
		MaxPacketSizeBytes: maxPacketSize,
		Env:                "test",
	})
	if err != nil {
		b.Error(err.Error())
		return
	}

	scope, closer := tally.NewRootScope(tally.ScopeOptions{
		Prefix:         "bench",
		CachedReporter: backend,
	}, 1*time.Second)

	perEndpointScope := scope.Tagged(
		map[string]string{
			"endpointid": "health",
			"handlerid":  "health",
		},
	)
	buckets := tally.DurationBuckets{
		0 * time.Millisecond,
		10 * time.Millisecond,
		25 * time.Millisecond,
		50 * time.Millisecond,
		75 * time.Millisecond,
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
		600 * time.Millisecond,
		800 * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
		5 * time.Second,
	}

	histogram := perEndpointScope.Histogram("inbound.latency", buckets)
	b.ReportAllocs()
	b.ResetTimer()

	bucketsLen := len(buckets)
	for i := 0; i < b.N; i++ {
		histogram.RecordDuration(buckets[i%bucketsLen])
	}

	b.StopTimer()
	closer.Close()
}
