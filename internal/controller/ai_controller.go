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

package controller

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/handler"
	"github.com/apache/answer/internal/base/middleware"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/schema/mcp_tools"
	"github.com/apache/answer/internal/service/ai_conversation"
	answercommon "github.com/apache/answer/internal/service/answer_common"
	"github.com/apache/answer/internal/service/comment"
	"github.com/apache/answer/internal/service/content"
	"github.com/apache/answer/internal/service/feature_toggle"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/apache/answer/internal/service/siteinfo_common"
	tagcommonser "github.com/apache/answer/internal/service/tag_common"
	usercommon "github.com/apache/answer/internal/service/user_common"
	"github.com/apache/answer/pkg/token"
	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
	"github.com/segmentfault/pacman/errors"
	"github.com/segmentfault/pacman/i18n"
	"github.com/segmentfault/pacman/log"
)

type AIController struct {
	searchService         *content.SearchService
	siteInfoService       siteinfo_common.SiteInfoCommonService
	tagCommonService      *tagcommonser.TagCommonService
	questioncommon        *questioncommon.QuestionCommon
	commentRepo           comment.CommentRepo
	userCommon            *usercommon.UserCommon
	answerRepo            answercommon.AnswerRepo
	mcpController         *MCPController
	aiConversationService ai_conversation.AIConversationService
	featureToggleSvc      *feature_toggle.FeatureToggleService
}

// NewAIController new site info controller.
func NewAIController(
	searchService *content.SearchService,
	siteInfoService siteinfo_common.SiteInfoCommonService,
	tagCommonService *tagcommonser.TagCommonService,
	questioncommon *questioncommon.QuestionCommon,
	commentRepo comment.CommentRepo,
	userCommon *usercommon.UserCommon,
	answerRepo answercommon.AnswerRepo,
	mcpController *MCPController,
	aiConversationService ai_conversation.AIConversationService,
	featureToggleSvc *feature_toggle.FeatureToggleService,
) *AIController {
	return &AIController{
		searchService:         searchService,
		siteInfoService:       siteInfoService,
		tagCommonService:      tagCommonService,
		questioncommon:        questioncommon,
		commentRepo:           commentRepo,
		userCommon:            userCommon,
		answerRepo:            answerRepo,
		mcpController:         mcpController,
		aiConversationService: aiConversationService,
		featureToggleSvc:      featureToggleSvc,
	}
}

func (c *AIController) ensureAIChatEnabled(ctx *gin.Context) bool {
	if c.featureToggleSvc == nil {
		return true
	}
	if err := c.featureToggleSvc.EnsureEnabled(ctx, feature_toggle.FeatureAIChatbot); err != nil {
		handler.HandleResponse(ctx, err, nil)
		return false
	}
	return true
}

type ChatCompletionsRequest struct {
	Messages       []Message `validate:"required,gte=1" json:"messages"`
	ConversationID string    `json:"conversation_id"`
	UserID         string    `json:"-"`
}

type Message struct {
	Role    string `json:"role" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type ChatCompletionsResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type StreamResponse struct {
	ChatCompletionID string         `json:"chat_completion_id"`
	Object           string         `json:"object"`
	Created          int64          `json:"created"`
	Model            string         `json:"model"`
	Choices          []StreamChoice `json:"choices"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ConversationContext struct {
	ConversationID    string
	UserID            string
	UserQuestion      string
	Messages          []*ai_conversation.ConversationMessage
	IsNewConversation bool
	Model             string
}

// GetResponseInput renders the stored conversation as Responses API input
// items. The Responses API takes a heterogeneous list: messages, function calls
// and function call outputs are all items, which is why this is []any rather
// than a message slice.
func (c *ConversationContext) GetResponseInput() []any {
	input := make([]any, len(c.Messages))
	for i, msg := range c.Messages {
		input[i] = openai.ResponseInputMessage{
			Type:    "message",
			Role:    msg.Role,
			Content: msg.Content,
		}
	}
	return input
}

// responseFunctionCall is one tool call the model asked for, carried between
// the stream reader and the tool executor.
type responseFunctionCall struct {
	CallID    string
	Name      string
	Arguments string
}

// aiReasoningEffort is the reasoning effort sent with every request, from
// ANSWER_AI_REASONING_EFFORT. Empty by default, so the field is omitted and the
// provider's own default applies.
func aiReasoningEffort() string {
	return os.Getenv("ANSWER_AI_REASONING_EFFORT")
}

// sendStreamData
func sendStreamData(w http.ResponseWriter, data StreamResponse) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *AIController) ChatCompletions(ctx *gin.Context) {
	if !c.ensureAIChatEnabled(ctx) {
		return
	}
	aiConfig, err := c.siteInfoService.GetSiteAI(context.Background())
	if err != nil {
		log.Errorf("Failed to get AI config: %v", err)
		handler.HandleResponse(ctx, errors.BadRequest("AI service configuration error"), nil)
		return
	}

	if !aiConfig.Enabled {
		handler.HandleResponse(ctx, errors.ServiceUnavailable("AI service is not enabled"), nil)
		return
	}

	aiProvider := aiConfig.GetProvider()

	req := &ChatCompletionsRequest{}
	if handler.BindAndCheck(ctx, req) {
		return
	}
	req.UserID = middleware.GetLoginUserIDFromContext(ctx)

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")
	ctx.Header("Access-Control-Allow-Origin", "*")
	ctx.Header("Access-Control-Allow-Headers", "Cache-Control")

	ctx.Status(http.StatusOK)

	w := ctx.Writer

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	chatcmplID := "chatcmpl-" + token.GenerateToken()
	created := time.Now().Unix()

	firstResponse := StreamResponse{
		ChatCompletionID: chatcmplID,
		Object:           "chat.completion.chunk",
		Created:          time.Now().Unix(),
		Model:            aiProvider.Model,
		Choices:          []StreamChoice{{Index: 0, Delta: Delta{Role: "assistant"}, FinishReason: nil}},
	}

	sendStreamData(w, firstResponse)

	conversationCtx := c.initializeConversationContext(ctx, aiProvider.Model, req)
	if conversationCtx == nil {
		log.Error("Failed to initialize conversation context")
		c.sendErrorResponse(w, chatcmplID, aiProvider.Model, "Failed to initialize conversation context")
		return
	}

	c.redirectRequestToAI(ctx, w, chatcmplID, conversationCtx)

	finishReason := "stop"
	endResponse := StreamResponse{
		ChatCompletionID: chatcmplID,
		Object:           "chat.completion.chunk",
		Created:          created,
		Model:            aiProvider.Model,
		Choices:          []StreamChoice{{Index: 0, Delta: Delta{}, FinishReason: &finishReason}},
	}

	sendStreamData(w, endResponse)

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	c.saveConversationRecord(ctx, chatcmplID, conversationCtx)
}

func (c *AIController) redirectRequestToAI(ctx *gin.Context, w http.ResponseWriter, id string, conversationCtx *ConversationContext) {
	client := c.createOpenAIClient()

	c.handleAIConversation(ctx, w, id, client, conversationCtx)
}

// createOpenAIClient
func (c *AIController) createOpenAIClient() *openai.Client {
	config := openai.DefaultConfig("")
	config.BaseURL = ""

	aiConfig, err := c.siteInfoService.GetSiteAI(context.Background())
	if err != nil {
		log.Errorf("Failed to get AI config: %v", err)
		return openai.NewClientWithConfig(config)
	}

	if !aiConfig.Enabled {
		log.Warn("AI feature is disabled")
		return openai.NewClientWithConfig(config)
	}

	aiProvider := aiConfig.GetProvider()

	config = openai.DefaultConfig(aiProvider.APIKey)
	config.BaseURL = aiProvider.APIHost
	if !strings.HasSuffix(config.BaseURL, "/v1") {
		config.BaseURL += "/v1"
	}
	return openai.NewClientWithConfig(config)
}

// getPromptByLanguage
func (c *AIController) getPromptByLanguage(language i18n.Language, question string) string {
	aiConfig, err := c.siteInfoService.GetSiteAI(context.Background())
	if err != nil {
		log.Errorf("Failed to get AI config: %v", err)
		return c.getDefaultPrompt(language, question)
	}

	var promptTemplate string

	switch language {
	case i18n.LanguageChinese:
		promptTemplate = aiConfig.PromptConfig.ZhCN
	case i18n.LanguageEnglish:
		promptTemplate = aiConfig.PromptConfig.EnUS
	default:
		promptTemplate = aiConfig.PromptConfig.EnUS
	}

	if promptTemplate == "" {
		return c.getDefaultPrompt(language, question)
	}

	return fmt.Sprintf(promptTemplate, question)
}

// getDefaultPrompt prompt
func (c *AIController) getDefaultPrompt(language i18n.Language, question string) string {
	switch language {
	case i18n.LanguageChinese:
		return fmt.Sprintf(constant.DefaultAIPromptConfigZhCN, question)
	case i18n.LanguageEnglish:
		return fmt.Sprintf(constant.DefaultAIPromptConfigEnUS, question)
	default:
		return fmt.Sprintf(constant.DefaultAIPromptConfigEnUS, question)
	}
}

// initializeConversationContext
func (c *AIController) initializeConversationContext(ctx *gin.Context, model string, req *ChatCompletionsRequest) *ConversationContext {
	if len(req.ConversationID) == 0 {
		req.ConversationID = token.GenerateToken()
	}
	conversationCtx := &ConversationContext{
		UserID:         req.UserID,
		Messages:       make([]*ai_conversation.ConversationMessage, 0),
		ConversationID: req.ConversationID,
		Model:          model,
	}

	conversationDetail, exist, err := c.aiConversationService.GetConversationDetail(ctx, &schema.AIConversationDetailReq{
		ConversationID: req.ConversationID,
		UserID:         req.UserID,
	})
	if err != nil {
		log.Errorf("Failed to get conversation detail: %v", err)
		return nil
	}
	if !exist {
		conversationCtx.UserQuestion = req.Messages[0].Content
		conversationCtx.Messages = c.buildInitialMessages(ctx, req)
		conversationCtx.IsNewConversation = true
		return conversationCtx
	}
	conversationCtx.IsNewConversation = false

	for _, record := range conversationDetail.Records {
		conversationCtx.Messages = append(conversationCtx.Messages, &ai_conversation.ConversationMessage{
			ChatCompletionID: record.ChatCompletionID,
			Role:             record.Role,
			Content:          record.Content,
		})
	}
	conversationCtx.Messages = append(conversationCtx.Messages, &ai_conversation.ConversationMessage{
		Role:    req.Messages[0].Role,
		Content: req.Messages[0].Content,
	})
	return conversationCtx
}

// buildInitialMessages
func (c *AIController) buildInitialMessages(ctx *gin.Context, req *ChatCompletionsRequest) []*ai_conversation.ConversationMessage {
	question := ""
	if len(req.Messages) == 1 {
		question = req.Messages[0].Content
	} else {
		messages := make([]*ai_conversation.ConversationMessage, len(req.Messages))
		for i, msg := range req.Messages {
			messages[i] = &ai_conversation.ConversationMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}
		}
		return messages
	}

	currentLang := handler.GetLangByCtx(ctx)

	prompt := c.getPromptByLanguage(currentLang, question)

	return []*ai_conversation.ConversationMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}}
}

// saveConversationRecord
func (c *AIController) saveConversationRecord(ctx context.Context, chatcmplID string, conversationCtx *ConversationContext) {
	if conversationCtx == nil || len(conversationCtx.Messages) == 0 {
		return
	}

	if conversationCtx.IsNewConversation {
		topic := conversationCtx.UserQuestion
		if topic == "" {
			log.Warn("No user message found for new conversation")
			return
		}

		err := c.aiConversationService.CreateConversation(ctx, conversationCtx.UserID, conversationCtx.ConversationID, topic)
		if err != nil {
			log.Errorf("Failed to create conversation: %v", err)
			return
		}
	}

	err := c.aiConversationService.SaveConversationRecords(ctx, conversationCtx.ConversationID, chatcmplID, conversationCtx.Messages)
	if err != nil {
		log.Errorf("Failed to save conversation records: %v", err)
	}
}

func (c *AIController) handleAIConversation(ctx *gin.Context, w http.ResponseWriter, id string, client *openai.Client, conversationCtx *ConversationContext) {
	maxRounds := 10
	input := conversationCtx.GetResponseInput()

	for round := range maxRounds {
		log.Debugf("AI conversation round: %d", round+1)

		aiReq := openai.CreateResponseRequest{
			Model:  conversationCtx.Model,
			Input:  input,
			Tools:  c.getMCPTools(),
			Stream: true,
		}
		if effort := aiReasoningEffort(); effort != "" {
			aiReq.Reasoning = &openai.ResponseReasoning{Effort: effort}
		}

		toolCalls, finished, aiResponse := c.processAIStream(ctx, w, id, conversationCtx.Model, client, aiReq)

		log.Debugf("Round %d: toolCalls=%d", round+1, len(toolCalls))
		if aiResponse != "" {
			conversationCtx.Messages = append(conversationCtx.Messages, &ai_conversation.ConversationMessage{
				Role:    "assistant",
				Content: aiResponse,
			})
		}

		if finished {
			return
		}

		if len(toolCalls) > 0 {
			input = c.executeToolCalls(ctx, toolCalls, input, aiResponse)
		} else {
			return
		}
	}

	log.Warnf("AI conversation reached maximum rounds limit: %d", maxRounds)
}

// processAIStream consumes one Responses API stream, forwarding text to the
// browser as it arrives and collecting any tool calls the model asked for. The
// events it reads are Responses events; what it writes is still Answer's own
// chat.completion.chunk shape, so the frontend is unchanged.
//
// Reasoning is deliberately not handled. Bedrock emits a reasoning output item
// with no text for GPT-5.6, with reasoning.summary set to auto or detailed
// alike, so a reasoning branch here would be unreachable code carried across
// every upstream rebase. A provider that streams reasoning_text.delta or
// reasoning_summary_text.delta would need it added back.
func (c *AIController) processAIStream(
	_ *gin.Context, w http.ResponseWriter, id, model string, client *openai.Client, aiReq openai.CreateResponseRequest) (
	[]responseFunctionCall, bool, string) {
	stream, err := client.CreateResponseStream(context.Background(), aiReq)
	if err != nil {
		log.Errorf("Failed to create stream: %v", err)
		c.sendErrorResponse(w, id, model, "Failed to create AI stream")
		return nil, true, ""
	}
	defer func() {
		_ = stream.Close()
	}()

	var toolCalls []responseFunctionCall
	var accumulatedContent strings.Builder
	completed := false

	for {
		event, err := stream.Recv()
		if err != nil {
			// The stream signals its end with io.EOF. Comparing the message
			// text instead would miss a wrapped error and treat a clean end of
			// stream as a failure. The stdlib import is aliased because this
			// file already imports pacman/errors as errors.
			if stderrors.Is(err, io.EOF) {
				log.Info("Stream finished")
				break
			}
			log.Errorf("Stream error: %v", err)
			break
		}

		switch event.Type {
		case openai.ResponseStreamEventOutputTextDelta:
			if event.Delta == "" {
				continue
			}
			accumulatedContent.WriteString(event.Delta)
			sendStreamData(w, StreamResponse{
				ChatCompletionID: id,
				Object:           "chat.completion.chunk",
				Created:          time.Now().Unix(),
				Model:            model,
				Choices: []StreamChoice{
					{Index: 0, Delta: Delta{Content: event.Delta}, FinishReason: nil},
				},
			})

		// A completed function_call item carries the whole call, so the
		// argument deltas that preceded it need no accumulation of their own.
		case openai.ResponseStreamEventOutputItemDone:
			if event.Item == nil || event.Item.Type != "function_call" {
				continue
			}
			toolCalls = append(toolCalls, responseFunctionCall{
				CallID:    event.Item.CallID,
				Name:      event.Item.Name,
				Arguments: event.Item.Arguments,
			})

		case openai.ResponseStreamEventCompleted:
			completed = true

		case openai.ResponseStreamEventFailed, openai.ResponseStreamEventError:
			message := event.Message
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
			}
			if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
				message = event.Response.Error.Message
			}
			log.Errorf("AI stream reported failure: %s", message)
			c.sendErrorResponse(w, id, model, "AI request failed")
			return nil, true, accumulatedContent.String()
		}
	}

	// Tool calls mean another round; anything else ends the conversation. A
	// stream that ends without completing is treated as finished so a truncated
	// response cannot spin the round loop.
	if len(toolCalls) > 0 {
		return toolCalls, false, accumulatedContent.String()
	}
	_ = completed
	return nil, true, accumulatedContent.String()
}

// executeToolCalls runs each requested tool and appends the call and its result
// to the input, which is what carries them into the next round.
func (c *AIController) executeToolCalls(ctx *gin.Context, toolCalls []responseFunctionCall, input []any, assistantContent string) []any {
	if assistantContent != "" {
		input = append(input, openai.ResponseInputMessage{
			Type:    "message",
			Role:    openai.ChatMessageRoleAssistant,
			Content: assistantContent,
		})
	}

	for _, toolCall := range toolCalls {
		if toolCall.CallID == "" || toolCall.Name == "" {
			log.Errorf("Invalid tool call: missing required fields. CallID: %s, Name: %s", toolCall.CallID, toolCall.Name)
			continue
		}

		arguments := toolCall.Arguments
		if arguments == "" {
			arguments = "{}"
		}

		// The call is echoed back before its output: the Responses API pairs
		// them by call_id, and an output without its call is rejected.
		input = append(input, openai.ResponseOutputItem{
			Type:      "function_call",
			CallID:    toolCall.CallID,
			Name:      toolCall.Name,
			Arguments: arguments,
		})

		var args map[string]any
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			log.Errorf("Failed to parse tool arguments for %s: %v", toolCall.Name, err)
			input = append(input, openai.ResponseFunctionCallOutput{
				Type:   "function_call_output",
				CallID: toolCall.CallID,
				Output: fmt.Sprintf("Error parsing tool arguments: %v", err),
			})
			continue
		}

		result, err := c.callMCPTool(ctx, toolCall.Name, args)
		if err != nil {
			log.Errorf("Failed to call MCP tool %s: %v", toolCall.Name, err)
			result = fmt.Sprintf("Error calling tool %s: %v", toolCall.Name, err)
		}

		input = append(input, openai.ResponseFunctionCallOutput{
			Type:   "function_call_output",
			CallID: toolCall.CallID,
			Output: result,
		})
	}

	return input
}

// pluginToolOwner returns the enabled AITool plugin that lists toolName, or
// nil when no plugin does. Plugins serve their tool lists from a cache, so
// this is cheap enough to call per tool call.
func (c *AIController) pluginToolOwner(ctx context.Context, toolName string) plugin.AITool {
	var owner plugin.AITool
	_ = plugin.CallAITool(func(t plugin.AITool) error {
		if owner != nil {
			return nil
		}
		for _, tool := range t.Tools(ctx) {
			if tool.Name == toolName {
				owner = t
				break
			}
		}
		return nil
	})
	return owner
}

// callPluginTool routes a tool the controller does not know to the AITool
// plugin that listed it. The plugin's error is returned as it is: the caller
// hands it to the model as the tool result.
func (c *AIController) callPluginTool(ctx context.Context, toolName string, arguments map[string]any) (string, error) {
	owner := c.pluginToolOwner(ctx, toolName)
	if owner == nil {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
	return owner.CallTool(ctx, toolName, arguments)
}

// sendErrorResponse send error response in stream
func (c *AIController) sendErrorResponse(w http.ResponseWriter, id, model, errorMsg string) {
	errorResponse := StreamResponse{
		ChatCompletionID: id,
		Object:           "chat.completion.chunk",
		Created:          time.Now().Unix(),
		Model:            model,
		Choices: []StreamChoice{
			{
				Index: 0,
				Delta: Delta{
					Content: fmt.Sprintf("Error: %s", errorMsg),
				},
				FinishReason: nil,
			},
		},
	}
	sendStreamData(w, errorResponse)
}

// getMCPTools
func (c *AIController) getMCPTools() []openai.ResponseTool {
	openaiTools := make([]openai.ResponseTool, 0)
	for _, mcpTool := range mcp_tools.MCPToolsList {
		openaiTool := c.convertMCPToolToOpenAI(mcpTool)
		openaiTools = append(openaiTools, openaiTool)
	}

	// Tools served by AITool plugins ride along beside Answer's own. A plugin
	// that lists nothing, because it is unconfigured or its source cannot be
	// reached, adds nothing, and the model then works from Answer's content.
	_ = plugin.CallAITool(func(t plugin.AITool) error {
		for _, tool := range t.Tools(context.Background()) {
			openaiTools = append(openaiTools, c.convertMCPToolToOpenAI(tool))
		}
		return nil
	})

	return openaiTools
}

// convertMCPToolToOpenAI
func (c *AIController) convertMCPToolToOpenAI(mcpTool mcp.Tool) openai.ResponseTool {
	properties := make(map[string]any)
	required := make([]string, 0)

	maps.Copy(properties, mcpTool.InputSchema.Properties)

	required = append(required, mcpTool.InputSchema.Required...)

	parameters := map[string]any{
		"type":       "object",
		"properties": properties,
	}

	if len(required) > 0 {
		parameters["required"] = required
	}

	// Responses tools are flat: name, description and parameters sit on the
	// tool itself rather than under a function object.
	return openai.NewResponseFunctionTool(openai.FunctionDefinition{
		Name:        mcpTool.Name,
		Description: mcpTool.Description,
		Parameters:  parameters,
	})
}

// callMCPTool
func (c *AIController) callMCPTool(ctx context.Context, toolName string, arguments map[string]any) (string, error) {
	request := mcp.CallToolRequest{
		Request: mcp.Request{},
		Params: struct {
			Name      string    `json:"name"`
			Arguments any       `json:"arguments,omitempty"`
			Meta      *mcp.Meta `json:"_meta,omitempty"`
		}{
			Name:      toolName,
			Arguments: arguments,
		},
	}

	var result *mcp.CallToolResult
	var err error

	log.Debugf("Calling MCP tool: %s with arguments: %v", toolName, arguments)

	switch toolName {
	case "get_questions":
		result, err = c.mcpController.MCPQuestionsHandler()(ctx, request)
	case "get_answers_by_question_id":
		result, err = c.mcpController.MCPAnswersHandler()(ctx, request)
	case "get_comments":
		result, err = c.mcpController.MCPCommentsHandler()(ctx, request)
	case "get_tags":
		result, err = c.mcpController.MCPTagsHandler()(ctx, request)
	case "get_tag_detail":
		result, err = c.mcpController.MCPTagDetailsHandler()(ctx, request)
	case "get_user":
		result, err = c.mcpController.MCPUserDetailsHandler()(ctx, request)
	case "semantic_search":
		result, err = c.mcpController.MCPSemanticSearchHandler()(ctx, request)
	default:
		return c.callPluginTool(ctx, toolName, arguments)
	}

	if err != nil {
		return "", err
	}

	data, _ := json.Marshal(result)
	log.Debugf("MCP tool %s called successfully, result: %v", toolName, string(data))

	if result != nil && len(result.Content) > 0 {
		if textContent, ok := result.Content[0].(mcp.TextContent); ok {
			return textContent.Text, nil
		}
	}

	return "No result found", nil
}
