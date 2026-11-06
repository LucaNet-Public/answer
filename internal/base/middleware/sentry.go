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
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// Lucanet: ReportToSentry gives every request its own Sentry scope, carrying
// the request and the signed-in user's ID, and a transaction when the request
// is sampled. It belongs inside Recovery: the SDK reports a panic with the
// request attached and re-panics, and Recovery still writes the response. The
// 500s Answer returns without panicking are reported by HandleResponse, from
// the same scope. Without a DSN, nothing is added. See cmd/sentry.go.
func ReportToSentry() []gin.HandlerFunc {
	if sentry.CurrentHub().Client() == nil {
		return nil
	}
	return []gin.HandlerFunc{
		sentrygin.New(sentrygin.Options{Repanic: true}),
		reportSentryUser,
	}
}

// SentryDataCollection is what Answer's events may carry, shared by
// cmd/sentry.go and this package's tests so the tests run with exactly the
// options the server does. Request bodies are questions, answers and AI chat
// prompts, and cookies carry the session, so neither is collected. Headers
// are left at the SDK's default, which keeps them but redacts the values of
// sensitive ones such as Authorization. An event names the signed-in user by
// ID, which reportSentryUser sets explicitly -- never an email address.
func SentryDataCollection() *sentry.DataCollection {
	return &sentry.DataCollection{
		UserInfo:   sentry.Set(false),
		Cookies:    &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		HTTPBodies: []sentry.BodyType{},
	}
}

// reportSentryUser names the signed-in user on the request's events. The auth
// middleware runs later, on the route groups, so the ID is read when an event
// is sent rather than now. Only the ID: an address or a name is not needed to
// find the account.
func reportSentryUser(ctx *gin.Context) {
	if hub := sentrygin.GetHubFromContext(ctx); hub != nil {
		hub.Scope().AddEventProcessor(func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			if userID := GetLoginUserIDFromContext(ctx); userID != "" {
				event.User.ID = userID
			}
			return event
		})
	}
	ctx.Next()
}
