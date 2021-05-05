package provider

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/digitalocean/flipop/pkg/metacontext"
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	callsTotal  *prometheus.CounterVec
	callLatency prometheus.ObserverVec
}

func (m *metrics) withProvider(provider string) *metrics {
	l := prometheus.Labels{"provider": provider}
	return &metrics{
		callsTotal:  m.callsTotal.MustCurryWith(l),
		callLatency: m.callLatency.MustCurryWith(l),
	}
}

func initMetrics() *metrics {
	return &metrics{
		callsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "calls_total",
			Help:      `Total number of calls to providers.`,
		}, []string{
			"provider",
			"call",
			"outcome",
			"kind",
			"namespace",
			"name",
		}),
		callLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricNamespace,
			Subsystem: metricSubsystem,
			Name:      "call_duration_seconds",
			Help:      `Histogram of provider call latency.`,
			Buckets:   prometheus.DefBuckets,
		}, []string{
			"provider",
			"call",
			"kind",
			"namespace",
			"name",
		}),
	}
}

// startCall returns a callback which should be called (or ideally deferred) when the call
// completes. Ex. `done := m.startCall(); defer done(&err)`
func (m *metrics) startCall(ctx context.Context) func(*error) {
	if m == nil {
		return func(*error) {}
	}
	call := "unknown"
	var funcNames string
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		f := runtime.FuncForPC(pc)
		funcNames = f.Name() // format returns "main.funcOne.funcTwo" depending on runtimeCaller(int)
		if idx := strings.LastIndexByte(funcNames, '.'); idx >= 0 {
			call = funcNames[idx+1:]
		}
	}
	start := time.Now()
	_, namespace, name, kind := metacontext.KubeMetadataFromContext(ctx)
	return func(err *error) {
		finish := time.Now()
		var outcome string
		switch *err {
		case nil, ErrInProgress:
			outcome = "success"
		default:
			outcome = "error"
		}
		m.callsTotal.With(prometheus.Labels{
			"call":      call,
			"outcome":   outcome,
			"namespace": namespace,
			"name":      name,
			"kind":      kind,
		}).Inc()
		m.callLatency.With(prometheus.Labels{
			"call":      call,
			"namespace": namespace,
			"name":      name,
			"kind":      kind,
		}).Observe(finish.Sub(start).Seconds())
	}
}
