// Package agents provides AI agent interfaces and implementations for trading decisions.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// OpenAIClient implements LLMClient using OpenAI API.
type OpenAIClient struct {
	client          *openai.Client
	model           string
	reasoningEffort string // "low", "medium", "high", or "" for default
}

// NewOpenAIClient creates a new OpenAI LLM client.
func NewOpenAIClient(apiKey string, model string) *OpenAIClient {
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
	}
}

// NewOpenAIClientWithReasoning creates a new OpenAI LLM client with reasoning effort.
func NewOpenAIClientWithReasoning(apiKey, model, reasoningEffort string) *OpenAIClient {
	return &OpenAIClient{
		client:          openai.NewClient(apiKey),
		model:           model,
		reasoningEffort: reasoningEffort,
	}
}

// Complete sends a prompt to the LLM and returns the response.
func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	}
	applyJSONResponseFormat(&req, prompt)
	if c.reasoningEffort != "" {
		req.ReasoningEffort = c.reasoningEffort
	}
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from openai")
	}
	return resp.Choices[0].Message.Content, nil
}

// CompleteWithSystem sends a prompt with system message to the LLM.
func (c *OpenAIClient) CompleteWithSystem(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
	}
	applyJSONResponseFormat(&req, systemPrompt, userPrompt)
	if c.reasoningEffort != "" {
		req.ReasoningEffort = c.reasoningEffort
	}
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("openai completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from openai")
	}
	return resp.Choices[0].Message.Content, nil
}

// ToolCallResult represents the result of a tool call execution.
type ToolCallResult struct {
	ToolCallID string
	Result     string
}

// ToolCallLog represents a single tool call in the chain of thought.
type ToolCallLog struct {
	ToolName  string
	Arguments string
	Result    string
}

// ChainOfThought captures the AI's reasoning process.
type ChainOfThought struct {
	ToolCalls []ToolCallLog
	Response  string
}

// ToolExecutorInterface defines the interface for tool executors.
// Both ToolExecutor (live) and BacktestToolExecutor implement this.
type ToolExecutorInterface interface {
	ExecuteTool(ctx context.Context, toolName string, args json.RawMessage) (string, error)
}

// CompleteWithTools sends a prompt with tools and handles tool calls.
// It returns the final response after executing any tool calls.
func (c *OpenAIClient) CompleteWithTools(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor ToolExecutorInterface) (string, error) {
	cot, err := c.CompleteWithToolsVerbose(ctx, systemPrompt, userPrompt, tools, executor)
	if err != nil {
		return "", err
	}
	return cot.Response, nil
}

// CompleteWithToolsVerbose sends a prompt with tools and returns the full chain of thought.
func (c *OpenAIClient) CompleteWithToolsVerbose(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor ToolExecutorInterface) (*ChainOfThought, error) {
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}

	cot := &ChainOfThought{
		ToolCalls: make([]ToolCallLog, 0),
	}

	// Allow up to 8 rounds of tool calls (increased for mini models that may call more tools)
	for i := 0; i < 8; i++ {
		req := openai.ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
			Tools:    tools,
		}
		applyJSONResponseFormat(&req, systemPrompt, userPrompt)
		if c.reasoningEffort != "" {
			req.ReasoningEffort = c.reasoningEffort
		}
		resp, err := c.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("openai completion failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, fmt.Errorf("no response from openai")
		}

		choice := resp.Choices[0]

		// If no tool calls, return the content
		if len(choice.Message.ToolCalls) == 0 {
			cot.Response = choice.Message.Content
			return cot, nil
		}

		// Add assistant message with tool calls
		messages = append(messages, choice.Message)

		// Execute each tool call
		for _, toolCall := range choice.Message.ToolCalls {
			result, err := executor.ExecuteTool(ctx, toolCall.Function.Name, json.RawMessage(toolCall.Function.Arguments))
			if err != nil {
				result = fmt.Sprintf("Error executing tool %s: %v", toolCall.Function.Name, err)
			}

			// Log the tool call
			cot.ToolCalls = append(cot.ToolCalls, ToolCallLog{
				ToolName:  toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
				Result:    result,
			})

			// Add tool result message
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("exceeded maximum tool call iterations")
}

// GetClient returns the underlying OpenAI client for advanced usage.
func (c *OpenAIClient) GetClient() *openai.Client {
	return c.client
}

// GetModel returns the model name.
func (c *OpenAIClient) GetModel() string {
	return c.model
}

// GetReasoningEffort returns the reasoning effort setting.
func (c *OpenAIClient) GetReasoningEffort() string {
	return c.reasoningEffort
}

// SetReasoningEffort updates the reasoning effort setting.
func (c *OpenAIClient) SetReasoningEffort(effort string) {
	c.reasoningEffort = effort
}

func applyJSONResponseFormat(req *openai.ChatCompletionRequest, prompts ...string) {
	for _, prompt := range prompts {
		normalized := strings.ToLower(prompt)
		if strings.Contains(normalized, "json") {
			req.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			}
			return
		}
	}
}
