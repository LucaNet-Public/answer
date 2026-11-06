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

package router

import (
	"time"

	"github.com/apache/answer/internal/schema/mcp_tools"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/server"
)

func (a *AnswerAPIRouter) RegisterMCPRouter(r *gin.RouterGroup) {
	s := server.NewMCPServer("Answer Enterprise MCP Server", "1.0.0")

	s.AddTool(mcp_tools.NewQuestionsTool(), a.mcpController.MCPQuestionsHandler())
	s.AddTool(mcp_tools.NewAnswersTool(), a.mcpController.MCPAnswersHandler())
	s.AddTool(mcp_tools.NewCommentsTool(), a.mcpController.MCPCommentsHandler())
	s.AddTool(mcp_tools.NewTagsTool(), a.mcpController.MCPTagsHandler())
	s.AddTool(mcp_tools.NewTagDetailTool(), a.mcpController.MCPTagDetailsHandler())
	s.AddTool(mcp_tools.NewUserTool(), a.mcpController.MCPUserDetailsHandler())
	// Upstream defines semantic_search and its handler for this server but never
	// registers them, so a client could reach only get_questions, which returns
	// the five newest matches. This is the retrieval the assistant itself uses.
	s.AddTool(mcp_tools.NewSemanticSearchTool(), a.mcpController.MCPSemanticSearchHandler())

	sseServer := server.NewSSEServer(s,
		server.WithSSEEndpoint("/answer/api/v1/mcp/see"),
		server.WithMessageEndpoint("/answer/api/v1/mcp/message"),
		// The stream is silent between messages, and CloudFront cuts a response
		// that sends nothing for 60 seconds. A ping every 30 seconds keeps it
		// open; the client answers each one with a POST, which the WAF counts.
		server.WithKeepAlive(true),
		server.WithKeepAliveInterval(30 * time.Second),
	)
	r.GET("/mcp/sse", gin.WrapH(sseServer.SSEHandler()))
	r.POST("/mcp/message", gin.WrapH(sseServer.MessageHandler()))
}
