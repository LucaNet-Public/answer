package answercmd

import (
	"math"
	"testing"

	"github.com/getsentry/sentry-go"
)

// Lucanet: an unreadable sample rate traces nothing rather than everything.
func TestSentryTracesSampleRate(t *testing.T) {
	cases := map[string]float64{
		"":     0,
		"x":    0,
		"NaN":  0,
		"-1":   0,
		"0.1":  0.1,
		"1":    1,
		"2":    1,
		"+Inf": 1,
	}
	for value, want := range cases {
		if got := sentryTracesSampleRate(value); got != want || math.IsNaN(got) {
			t.Errorf("sentryTracesSampleRate(%q) = %v, want %v", value, got, want)
		}
	}
}

// Lucanet: a trace the browser propagated keeps its decision; anything else is
// sampled at the configured rate.
func TestSentryTracesSampler(t *testing.T) {
	sample := sentryTracesSampler(0.1)
	cases := map[sentry.Sampled]float64{
		sentry.SampledTrue:      1,
		sentry.SampledFalse:     0,
		sentry.SampledUndefined: 0.1,
	}
	for parent, want := range cases {
		if got := sample(sentry.SamplingContext{Span: &sentry.Span{}, ParentSampled: parent}); got != want {
			t.Errorf("parent %v: got %v, want %v", parent, got, want)
		}
	}
}

// Lucanet: a health check's transaction is dropped, a reader's is kept.
func TestDropLoadBalancerHealthChecks(t *testing.T) {
	poll := &sentry.Event{Request: &sentry.Request{Headers: map[string]string{"User-Agent": "ELB-HealthChecker/2.0"}}}
	if dropLoadBalancerHealthChecks(poll, nil) != nil {
		t.Error("expected the health check's transaction to be dropped")
	}
	reader := &sentry.Event{Request: &sentry.Request{Headers: map[string]string{"User-Agent": "Mozilla/5.0"}}}
	if dropLoadBalancerHealthChecks(reader, nil) == nil {
		t.Error("expected a reader's transaction to be kept")
	}
	if dropLoadBalancerHealthChecks(&sentry.Event{}, nil) == nil {
		t.Error("expected a transaction without a request to be kept")
	}
}
