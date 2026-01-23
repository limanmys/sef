package providers

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the LLMProvider interface for OpenAI
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAIProvider creates a new OpenAI provider instance
func NewOpenAIProvider(config map[string]interface{}) *OpenAIProvider {
	apiKey := ""
	if key, ok := config["api_key"].(string); ok {
		apiKey = key
	}

	configOpenAI := openai.DefaultConfig(apiKey)
	if baseURL, ok := config["base_url"].(string); ok && baseURL != "" {
		configOpenAI.BaseURL = baseURL
	}

	client := openai.NewClientWithConfig(configOpenAI)
	return &OpenAIProvider{
		client: client,
	}
}

// ValidateConfig validates the OpenAI provider configuration
func (o *OpenAIProvider) ValidateConfig(config map[string]interface{}) error {
	if baseURL, ok := config["base_url"].(string); ok && baseURL != "" {
		// If using custom base URL, we don't require API key (might be local OpenAI-compatible API)
		return nil
	}

	// For official OpenAI API, require API key
	if apiKey, ok := config["api_key"].(string); !ok || apiKey == "" {
		return fmt.Errorf("api_key is required for OpenAI provider")
	}
	return nil
}

// applyOptionsToRequest applies common options to an OpenAI ChatCompletionRequest
func applyOptionsToRequest(req *openai.ChatCompletionRequest, options map[string]interface{}) {
	// Temperature
	if temp, ok := options["temperature"].(float64); ok {
		req.Temperature = float32(temp)
	}
	// Top P (nucleus sampling)
	if topP, ok := options["top_p"].(float64); ok {
		req.TopP = float32(topP)
	}
}

// Generate generates a response from OpenAI with streaming support
func (o *OpenAIProvider) Generate(ctx context.Context, prompt string, options map[string]interface{}) (<-chan string, error) {
	model := "gpt-3.5-turbo" // Default model
	if m, ok := options["model"].(string); ok {
		model = m
	}

	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		Stream: true,
	}

	// Apply all model options
	applyOptionsToRequest(&req, options)

	stream, err := o.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		for {
			response, err := stream.Recv()
			if err != nil {
				// Log error if needed, but for now just break
				break
			}
			if len(response.Choices) > 0 {
				content := response.Choices[0].Delta.Content
				if content != "" {
					ch <- content
				}
			}
			if response.Choices[0].FinishReason != "" {
				break
			}
		}
		stream.Close()
	}()

	return ch, nil
}

// GenerateChat generates a response using OpenAI's chat API with proper message roles
func (o *OpenAIProvider) GenerateChat(ctx context.Context, messages []ChatMessage, options map[string]interface{}) (<-chan string, error) {
	model := "gpt-3.5-turbo" // Default model
	if m, ok := options["model"].(string); ok {
		model = m
	}

	// Convert to OpenAI chat messages
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, msg := range messages {
		var role string
		switch msg.Role {
		case "user":
			role = openai.ChatMessageRoleUser
		case "assistant":
			role = openai.ChatMessageRoleAssistant
		case "system":
			role = openai.ChatMessageRoleSystem
		default:
			role = openai.ChatMessageRoleUser
		}
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		}
	}

	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: openaiMessages,
		Stream:   true,
	}

	// Apply all model options
	applyOptionsToRequest(&req, options)

	stream, err := o.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		for {
			response, err := stream.Recv()
			if err != nil {
				break
			}
			if len(response.Choices) > 0 {
				content := response.Choices[0].Delta.Content
				if content != "" {
					ch <- content
				}
			}
			if response.Choices[0].FinishReason != "" {
				break
			}
		}
		stream.Close()
	}()

	return ch, nil
}

// GenerateChatWithTools generates a response using OpenAI's chat API with tool calling support
func (o *OpenAIProvider) GenerateChatWithTools(ctx context.Context, messages []ChatMessage, tools []ToolDefinition, options map[string]interface{}) (<-chan ChatResponse, error) {
	model := "gpt-4" // Default model for tool calls
	if m, ok := options["model"].(string); ok {
		model = m
	}

	// Convert to OpenAI chat messages
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, msg := range messages {
		var role string
		switch msg.Role {
		case "user":
			role = openai.ChatMessageRoleUser
		case "assistant":
			role = openai.ChatMessageRoleAssistant
		case "system":
			role = openai.ChatMessageRoleSystem
		case "tool":
			role = openai.ChatMessageRoleTool
		default:
			role = openai.ChatMessageRoleUser
		}
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    role,
			Content: msg.Content,
		}
	}

	// Convert tools to OpenAI format
	var openaiTools []openai.Tool
	for _, tool := range tools {
		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolType(tool.Type),
			Function: &openai.FunctionDefinition{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}

	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: openaiMessages,
		Stream:   true,
	}

	// Add tools if provided
	if len(openaiTools) > 0 {
		req.Tools = openaiTools
	}

	// Apply all model options
	applyOptionsToRequest(&req, options)

	stream, err := o.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan ChatResponse)
	go func() {
		defer close(ch)
		defer stream.Close()

		for {
			response, err := stream.Recv()
			if err != nil {
				break
			}

			if len(response.Choices) == 0 {
				continue
			}

			choice := response.Choices[0]
			chatResp := ChatResponse{}

			// Handle content
			if choice.Delta.Content != "" {
				chatResp.Content = choice.Delta.Content
			}

			// Handle tool calls
			if choice.Delta.ToolCalls != nil {
				for _, toolCall := range choice.Delta.ToolCalls {
					// Parse arguments JSON string if it's complete
					var args map[string]interface{}
					if toolCall.Function.Arguments != "" {
						// Try to parse as JSON
						if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
							// If not valid JSON yet (streaming), store raw
							args = map[string]interface{}{"raw": toolCall.Function.Arguments}
						}
					}

					chatResp.ToolCalls = append(chatResp.ToolCalls, ToolCall{
						ID:   toolCall.ID,
						Type: string(toolCall.Type),
						Function: ToolCallFunction{
							Name:      toolCall.Function.Name,
							Arguments: args,
						},
					})
				}
			}

			// Check if finished
			if choice.FinishReason != "" {
				chatResp.Done = true
			}

			// Send response if there's content or tool calls
			if chatResp.Content != "" || len(chatResp.ToolCalls) > 0 || chatResp.Done {
				ch <- chatResp
			}

			if choice.FinishReason != "" {
				break
			}
		}
	}()

	return ch, nil
}

// ListModels returns available models from OpenAI
func (o *OpenAIProvider) ListModels() ([]string, error) {
	models, err := o.client.ListModels(context.Background())
	if err != nil {
		return nil, err
	}

	var modelNames []string
	for _, model := range models.Models {
		modelNames = append(modelNames, model.ID)
	}
	return modelNames, nil
}
