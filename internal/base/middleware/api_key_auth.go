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
	"strings"

	"github.com/apache/answer/internal/base/handler"
	"github.com/apache/answer/internal/base/reason"
	"github.com/gin-gonic/gin"
	"github.com/segmentfault/pacman/errors"
)

// AuthAPIKey middleware to authenticate API key
func (am *AuthUserMiddleware) AuthAPIKey() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token := ExtractToken(ctx)
		if len(token) == 0 {
			handler.HandleResponse(ctx, errors.Unauthorized(reason.UnauthorizedError), nil)
			ctx.Abort()
			return
		}
		// Answer mounts this middleware on the MCP routes only, and every tool
		// there reads. The SSE transport opens its stream with a GET but posts
		// every message, so judging a read by the method refused a read-only
		// key from initialize on. Matching the route template rather than the
		// request path keeps a route that later reuses this middleware, or a
		// path that merely embeds the MCP prefix, from inheriting the change.
		route := ctx.FullPath()
		isRead := ctx.Request.Method == "GET" ||
			strings.HasSuffix(route, "/mcp/sse") || strings.HasSuffix(route, "/mcp/message")
		pass, err := am.authService.AuthAPIKey(ctx, isRead, token)
		if err != nil {
			handler.HandleResponse(ctx, errors.Unauthorized(reason.UnauthorizedError), nil)
			ctx.Abort()
			return
		}
		if !pass {
			handler.HandleResponse(ctx, errors.Unauthorized(reason.UnauthorizedError), nil)
			ctx.Abort()
			return
		}
		ctx.Next()
	}
}
