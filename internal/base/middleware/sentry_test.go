/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/apache/answer/internal/base/handler"
	"github.com/apache/answer/internal/entity"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
)

// Lucanet: tests for ReportToSentry. Events are caught in BeforeSend and
// dropped there, so nothing leaves the process.

type sentEvents struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (s *sentEvents) all() []*sentry.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*sentry.Event(nil), s.events...)
}

func startTestSentry(t *testing.T) *sentEvents {
	t.Helper()
	sent := &sentEvents{}
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "https://public@sentry.invalid/1",
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			sent.mu.Lock()
			sent.events = append(sent.events, event)
			sent.mu.Unlock()
			return nil
		},
		DataCollection: SentryDataCollection(),
	})
	if err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })
	return sent
}

func signedIn(userID string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Set(ctxUUIDKey, &entity.UserCacheInfo{UserID: userID})
		ctx.Next()
	}
}

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Recovery("/api"))
	r.Use(ReportToSentry()...)
	return r
}

func serve(r *gin.Engine, path string) *httptest.ResponseRecorder {
	return serveAs(r, path, "")
}

func serveAs(r *gin.Engine, path, userAgent string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	r.ServeHTTP(w, req)
	return w
}

// A panic reaches Sentry with its request and the signed-in user, and
// Recovery still answers it.
func TestReportToSentry_PanicCarriesRequestAndUser(t *testing.T) {
	sent := startTestSentry(t)
	r := testRouter()
	r.GET("/api/panic", signedIn("42"), func(ctx *gin.Context) {
		panic("test panic")
	})

	w := serve(r, "/api/panic")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected Recovery's 500, got %d", w.Code)
	}
	events := sent.all()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].User.ID != "42" {
		t.Errorf("expected user 42, got %q", events[0].User.ID)
	}
	if events[0].User.Email != "" || events[0].User.IPAddress != "" {
		t.Errorf("expected the user's ID only, got %+v", events[0].User)
	}
	if events[0].Request == nil || events[0].Request.URL == "" {
		t.Errorf("expected the request on the event, got %+v", events[0].Request)
	}
}

// An error HandleResponse turns into a 500 is reported; one it does not is
// not.
func TestReportToSentry_InternalErrorIsReported(t *testing.T) {
	sent := startTestSentry(t)
	r := testRouter()
	r.GET("/api/fails", signedIn("7"), func(ctx *gin.Context) {
		handler.HandleResponse(ctx, errors.New("database unreachable"), nil)
	})
	r.GET("/api/ok", func(ctx *gin.Context) {
		handler.HandleResponse(ctx, nil, "fine")
	})

	if w := serve(r, "/api/fails"); w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	serve(r, "/api/ok")

	events := sent.all()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if len(events[0].Exception) == 0 || events[0].Exception[0].Value != "database unreachable" {
		t.Errorf("expected the handler's error, got %+v", events[0].Exception)
	}
	if events[0].User.ID != "7" {
		t.Errorf("expected user 7, got %q", events[0].User.ID)
	}
}

// The load balancer's polls keep their scope: an error on one is reported
// like any other. Only their transactions are dropped, in cmd/sentry.go.
func TestReportToSentry_ReportsErrorsOnLoadBalancerHealthChecks(t *testing.T) {
	sent := startTestSentry(t)
	r := testRouter()
	r.GET("/", func(ctx *gin.Context) {
		handler.HandleResponse(ctx, errors.New("index failed"), nil)
	})

	serveAs(r, "/", "ELB-HealthChecker/2.0")
	if events := sent.all(); len(events) != 1 {
		t.Fatalf("expected the health check's error to be reported, got %d events", len(events))
	}
}

// A real transaction carries the poller's user agent under the key
// cmd/sentry.go's dropLoadBalancerHealthChecks reads.
func TestReportToSentry_TransactionCarriesUserAgent(t *testing.T) {
	var transactions []*sentry.Event
	var mu sync.Mutex
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://public@sentry.invalid/1",
		EnableTracing:    true,
		TracesSampleRate: 1,
		DataCollection:   SentryDataCollection(),
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			mu.Lock()
			transactions = append(transactions, event)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })
	r := testRouter()
	r.GET("/", func(ctx *gin.Context) { ctx.String(http.StatusOK, "index") })

	serveAs(r, "/", "ELB-HealthChecker/2.0")

	mu.Lock()
	defer mu.Unlock()
	if len(transactions) != 1 || transactions[0].Request == nil {
		t.Fatalf("expected one transaction with a request, got %d", len(transactions))
	}
	if got := transactions[0].Request.Headers["User-Agent"]; got != "ELB-HealthChecker/2.0" {
		t.Errorf("expected the poller's user agent under User-Agent, got %q", got)
	}
}

// The session token travels in Authorization, which the SDK's default header
// deny list must redact; Answer leaves headers at that default.
func TestReportToSentry_AuthorizationIsRedacted(t *testing.T) {
	sent := startTestSentry(t)
	r := testRouter()
	r.GET("/api/panic", func(ctx *gin.Context) {
		panic("test panic")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/panic", nil)
	req.Header.Set("Authorization", "session-token-value")
	r.ServeHTTP(w, req)

	events := sent.all()
	if len(events) != 1 || events[0].Request == nil {
		t.Fatalf("expected one event with a request, got %d", len(events))
	}
	// Present and redacted, not merely absent: an empty header map would also
	// keep the token out, and would mean headers had stopped being collected.
	if got := events[0].Request.Headers["Authorization"]; got != "[Filtered]" {
		t.Errorf("expected Authorization to be [Filtered], got %q", got)
	}
	for name, value := range events[0].Request.Headers {
		if strings.Contains(value, "session-token-value") {
			t.Errorf("header %s carries the session token: %q", name, value)
		}
	}
}

// Without a DSN nothing is added, so a local run behaves exactly as upstream.
func TestReportToSentry_NoClientAddsNothing(t *testing.T) {
	sentry.CurrentHub().BindClient(nil)
	if handlers := ReportToSentry(); handlers != nil {
		t.Errorf("expected no middleware without a client, got %d", len(handlers))
	}
}
