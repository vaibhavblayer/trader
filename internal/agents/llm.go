// Package agents provides AI agent interfaces and implementations for trading decisions.
package agents

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// OpenAIClient implements LLMClient using OpenAI API.
type OpenAIClient struct {
	client *openai.Client
	model  string
}

// NewOpenAIClient creates a new OpenAI LLM client.
func NewOpenAIClient(apiKey string, model string) *OpenAIClient {
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  model,
	}
}

// Complete sends a prompt to the LLM and returns the response.
func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})
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
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openai completion failed: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from openai")
	}
	return resp.Choices[0].Message.Content, nil
}
