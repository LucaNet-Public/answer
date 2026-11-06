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
	"errors"

	"github.com/mark3labs/mcp-go/mcp"
)

// ErrAIToolResult marks an error that is the tool's answer rather than a
// failure to run it: the tool was reached, ran, and reported that what was
// asked for does not exist or was not valid. An AITool wraps it with %w so
// the assistant can tell the two apart — a result is logged below error
// level, and the reader is told the source had nothing rather than that it
// could not be reached.
var ErrAIToolResult = errors.New("the tool returned an error result")

// AITool offers tools to the AI assistant beside Answer's built-in MCP tools.
// Every tool a plugin lists is offered to the model; when the model calls one
// by name, the plugin that listed it runs the call and returns text for the
// model to read. A failure is returned as an error, which the assistant hands
// to the model as the tool's result, so a broken tool degrades one answer
// rather than failing the conversation.
type AITool interface {
	Base

	// Tools lists the tools currently offered. It is called at the start of
	// every model round and again to route each call, so implementations
	// should serve a cached list and never block on a slow source. Names must
	// not collide with Answer's own tools; a plugin-specific prefix is the
	// usual way to ensure that.
	Tools(ctx context.Context) []mcp.Tool

	// CallTool runs one tool that Tools listed and returns its text output.
	CallTool(ctx context.Context, name string, arguments map[string]any) (string, error)
}

// AIToolProgress is implemented by an AITool plugin that can tell the reader
// what a call is doing while the assistant works. Both strings are shown to
// the person asking, in the interface language the plugin chooses, and should
// be one short line. A plugin without this interface gets generic wording.
type AIToolProgress interface {
	// DescribeToolCall renders one call as a line for the reader, for
	// example "Searching the portal for “…”…".
	DescribeToolCall(name string, arguments map[string]any) string

	// DescribeToolFailure is the line shown when the call failed; the model
	// has already been told the detail as the tool result. err is what
	// CallTool returned, so a result the tool itself flagged (an
	// ErrAIToolResult) can be worded as "nothing found" rather than as an
	// outage.
	DescribeToolFailure(name string, err error) string
}

var (
	// CallAITool calls every enabled AITool plugin.
	CallAITool,
	registerAITool = MakePlugin[AITool](false)
)
