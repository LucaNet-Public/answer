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

package plugin

import (
	"context"
	"github.com/apache/answer/internal/entity"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
)

// Tag is the existing core tag model exposed to server plugins.
type Tag = entity.Tag

const TagStatusAvailable = entity.TagStatusAvailable

// Core binds these capabilities during application initialization.
var CurrentUserID func(*gin.Context) string
var UserIDByEmail func(context.Context, string) (string, bool, error)
var LookupTag func(context.Context, string) (*Tag, bool, error)
var FollowTag func(ctx context.Context, objectID, userID string) error

// RootRouter registers public endpoints outside the API prefix, such as discovery metadata.
type RootRouter interface{ RegisterRootRouter(*gin.RouterGroup) }

// MCPRouter adds a transport around the existing tools. The group enforces the site's MCP switch.
type MCPRouter interface {
	RegisterMCPRouter(*gin.RouterGroup, *server.MCPServer)
}

// UserAvailable checks the current account and external user-center status, without a cached session.
var UserAvailable func(context.Context, string) (bool, error)

// PublishedObject checks an existing question or answer and its parent question.
var PublishedObject func(context.Context, string) (bool, error)
