package answercmd

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/apache/answer/internal/base/middleware"
	"github.com/getsentry/sentry-go"
	"github.com/segmentfault/pacman/log"
)

// Lucanet: errors and request traces go to Lucanet's Sentry. Terraform sets
// SENTRY_DSN, SENTRY_ENVIRONMENT, SENTRY_RELEASE and SENTRY_TRACES_SAMPLE_RATE;
// without a DSN, as in a local run, nothing is started and the middleware in
// internal/base/middleware/sentry.go passes every request straight through.
// See docs/sentry.md in the Lucanet repository.

// sentryFlushTimeout bounds how long a stopping server waits for reports.
const sentryFlushTimeout = 2 * time.Second

// startSentry starts the SDK and returns what flushes it on the way out.
func startSentry() (flush func()) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return func() {}
	}
	rate := sentryTracesSampleRate(os.Getenv("SENTRY_TRACES_SAMPLE_RATE"))
	err := sentry.Init(sentry.ClientOptions{
		Dsn:           dsn,
		Environment:   os.Getenv("SENTRY_ENVIRONMENT"),
		Release:       os.Getenv("SENTRY_RELEASE"),
		EnableTracing: rate > 0,
		TracesSampler: sentryTracesSampler(rate),
		// Only the transaction: an error on the same request is still
		// reported, so a forged user agent hides nothing but a trace.
		BeforeSendTransaction: dropLoadBalancerHealthChecks,
		DataCollection: middleware.SentryDataCollection(),
	})
	if err != nil {
		// A bad DSN must not stop the site; it is reported where someone
		// reading the logs will see why nothing reaches Sentry.
		log.Errorf("sentry not started: %v", err)
		return func() {}
	}
	return func() { sentry.Flush(sentryFlushTimeout) }
}

// reportPanic reports a panic that is stopping the server, then lets it
// continue: the process still exits as it would have, and the flush deferred
// before this one still runs on the way out. Deferred directly, as recover
// only works there.
func reportPanic() {
	if recovered := recover(); recovered != nil {
		sentry.CurrentHub().Recover(recovered)
		panic(recovered)
	}
}

// sentryTracesSampler traces a request at rate, except one that continues a
// trace the browser started: that keeps the browser's decision. Supplying a
// sampler replaces the SDK's own inheritance, and re-sampling would leave a
// sampled browser span with no server side, or trace a request the browser
// did not keep.
func sentryTracesSampler(rate float64) sentry.TracesSampler {
	return func(ctx sentry.SamplingContext) float64 {
		switch ctx.ParentSampled {
		case sentry.SampledTrue:
			return 1
		case sentry.SampledFalse:
			return 0
		}
		return rate
	}
}

// dropLoadBalancerHealthChecks drops the trace of a load balancer health
// check. The target group polls "/" from every subnet every few seconds,
// which would be most of the trace volume and none of the interest; matching
// the poller rather than the path keeps a reader's request to "/" traced.
func dropLoadBalancerHealthChecks(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event.Request != nil && strings.HasPrefix(event.Request.Headers["User-Agent"], "ELB-HealthChecker/") {
		return nil
	}
	return event
}

// sentryTracesSampleRate reads the share of requests to trace, between 0 and
// 1. Anything unreadable traces nothing rather than everything.
func sentryTracesSampleRate(value string) float64 {
	rate, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(rate) || rate < 0 {
		return 0
	}
	if rate > 1 {
		return 1
	}
	return rate
}
