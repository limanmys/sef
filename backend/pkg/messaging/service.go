package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sef/app/entities"
	"sef/internal/validation"
	"sef/pkg/providers"
	"sef/pkg/toolrunners"
	"strconv"

	"github.com/gofiber/fiber/v3/log"
	"gorm.io/gorm"
)

type SendMessageRequest struct {
	Content string `json:"content" validate:"required,min=1"`
}

type MessagingService struct {
	DB *gorm.DB
}

type MessagingServiceInterface interface {
	ValidateAndParseSessionID(sessionIDStr string) (uint, error)
	GetSessionByIDAndUser(sessionID, userID uint) (*entities.Session, error)
	ParseSendMessageRequest(body []byte) (*SendMessageRequest, error)
	LoadSessionWithChatbotAndMessages(sessionID, userID uint) (*entities.Session, error)
	LoadSessionWithChatbotToolsAndMessages(sessionID, userID uint) (*entities.Session, error)
	SaveUserMessage(sessionID uint, content string) error
	PrepareChatMessages(session *entities.Session, userContent string) []providers.ChatMessage
	CreateAssistantMessage(sessionID uint) (*entities.Message, error)
	CreateToolMessage(sessionID uint, content string) (*entities.Message, error)
	GenerateChatResponse(session *entities.Session, messages []providers.ChatMessage) (<-chan string, *entities.Message, error)
	UpdateAssistantMessage(assistantMessage *entities.Message, content string)
	UpdateAssistantMessageWithCallback(assistantMessage *entities.Message, content string, callback func())
	ConvertToolsToDefinitions(tools []entities.Tool) []providers.ToolDefinition
	ExecuteToolCall(ctx context.Context, toolCall providers.ToolCall) (string, error)
}

func (s *MessagingService) ValidateAndParseSessionID(sessionIDStr string) (uint, error) {
	sessionID, err := strconv.ParseUint(sessionIDStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid session ID: %w", err)
	}
	return uint(sessionID), nil
}

func (s *MessagingService) GetSessionByIDAndUser(sessionID, userID uint) (*entities.Session, error) {
	var session entities.Session
	if err := s.DB.
		Where("id = ? AND user_id = ?", sessionID, userID).
		First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("chat session not found")
		}
		log.Error("Failed to fetch chat session:", err)
		return nil, fmt.Errorf("failed to fetch chat session: %w", err)
	}
	return &session, nil
}

func (s *MessagingService) ParseSendMessageRequest(body []byte) (*SendMessageRequest, error) {
	var req SendMessageRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	if err := validation.Validate(req); err != nil {
		return nil, fmt.Errorf("validation failed: %v", err)
	}

	return &req, nil
}

func (s *MessagingService) LoadSessionWithChatbotAndMessages(sessionID, userID uint) (*entities.Session, error) {
	var session entities.Session
	if err := s.DB.
		Where("id = ? AND user_id = ?", sessionID, userID).
		Preload("Chatbot").
		Preload("Chatbot.Provider").
		Preload("Messages").
		First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("chat session not found")
		}
		log.Error("Failed to fetch chat session:", err)
		return nil, fmt.Errorf("failed to fetch chat session: %w", err)
	}
	return &session, nil
}

func (s *MessagingService) LoadSessionWithChatbotToolsAndMessages(sessionID, userID uint) (*entities.Session, error) {
	var session entities.Session
	if err := s.DB.
		Where("id = ? AND user_id = ?", sessionID, userID).
		Preload("Chatbot").
		Preload("Chatbot.Provider").
		Preload("Chatbot.Tools").
		Preload("Messages").
		First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("chat session not found")
		}
		log.Error("Failed to fetch chat session:", err)
		return nil, fmt.Errorf("failed to fetch chat session: %w", err)
	}
	return &session, nil
}

func (s *MessagingService) ConvertToolsToDefinitions(tools []entities.Tool) []providers.ToolDefinition {
	var definitions []providers.ToolDefinition
	for _, tool := range tools {
		parameters := map[string]interface{}{
			"type":       "object",
			"properties": make(map[string]interface{}),
			"required":   []string{},
		}

		if len(tool.Parameters) > 0 {
			properties := make(map[string]interface{})
			var required []string

			for _, param := range tool.Parameters {
				if paramMap, ok := param.(map[string]interface{}); ok {
					name, hasName := paramMap["name"].(string)
					paramType, hasType := paramMap["type"].(string)
					description, hasDesc := paramMap["description"].(string)
					isRequired, hasRequired := paramMap["required"].(bool)

					if hasName && hasType {
						property := map[string]interface{}{
							"type": paramType,
						}
						if hasDesc {
							property["description"] = description
						}
						properties[name] = property

						if hasRequired && isRequired {
							required = append(required, name)
						}
					}
				}
			}

			parameters["properties"] = properties
			parameters["required"] = required
		}

		definition := providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func (s *MessagingService) ExecuteToolCall(ctx context.Context, toolCall providers.ToolCall) (string, error) {
	var tool entities.Tool
	if err := s.DB.Where("name = ?", toolCall.Function.Name).First(&tool).Error; err != nil {
		return "", fmt.Errorf("tool not found: %s", toolCall.Function.Name)
	}

	var args map[string]interface{}
	if rawArgs, ok := toolCall.Function.Arguments["raw"].(string); ok {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	} else {
		args = toolCall.Function.Arguments
	}

	factory := &toolrunners.ToolRunnerFactory{}
	runner, err := factory.NewToolRunner(tool.Type, tool.Config, tool.Parameters)
	if err != nil {
		return "", fmt.Errorf("failed to create tool runner: %w", err)
	}

	toolContext := &toolrunners.ToolCallContext{
		ToolCallID:   toolCall.ID,
		FunctionName: toolCall.Function.Name,
		ToolName:     tool.Name,
		Metadata: map[string]interface{}{
			"tool_type":        tool.Type,
			"tool_id":          tool.ID,
			"tool_description": tool.Description,
		},
	}

	result, err := runner.ExecuteWithContext(ctx, args, toolContext)
	if err != nil {
		return "", fmt.Errorf("tool execution failed: %w", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool result: %w", err)
	}

	return string(resultJSON), nil
}

func (s *MessagingService) SaveUserMessage(sessionID uint, content string) error {
	userMessage := entities.Message{
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
	}

	if err := s.DB.Create(&userMessage).Error; err != nil {
		log.Error("Failed to save user message:", err)
		return fmt.Errorf("failed to save message: %w", err)
	}

	return nil
}

func (s *MessagingService) PrepareChatMessages(session *entities.Session, userContent string) []providers.ChatMessage {
	var messages []providers.ChatMessage

	if session.Chatbot.SystemPrompt != "" {
		messages = append(messages, providers.ChatMessage{
			Role:    "system",
			Content: session.Chatbot.SystemPrompt,
		})
	}

	for _, msg := range session.Messages {
		messages = append(messages, providers.ChatMessage{
			Role:    msg.Role,
			Content: cleanAssistantContent(msg.Content),
		})
	}

	messages = append(messages, providers.ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	return messages
}

func (s *MessagingService) CreateAssistantMessage(sessionID uint) (*entities.Message, error) {
	assistantMessage := entities.Message{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   "",
	}

	if err := s.DB.Create(&assistantMessage).Error; err != nil {
		log.Error("Failed to create assistant message:", err)
		return nil, fmt.Errorf("failed to create message record: %w", err)
	}

	return &assistantMessage, nil
}

func (s *MessagingService) CreateToolMessage(sessionID uint, content string) (*entities.Message, error) {
	toolMessage := entities.Message{
		SessionID: sessionID,
		Role:      "tool",
		Content:   content,
	}

	if err := s.DB.Create(&toolMessage).Error; err != nil {
		log.Error("Failed to create tool message:", err)
		return nil, fmt.Errorf("failed to create tool message record: %w", err)
	}

	return &toolMessage, nil
}

func (s *MessagingService) UpdateAssistantMessage(assistantMessage *entities.Message, content string) {
	if assistantMessage == nil {
		log.Error("Attempted to update nil assistant message")
		return
	}
	assistantMessage.Content = content
	if err := s.DB.Save(&assistantMessage).Error; err != nil {
		log.Error("Failed to update assistant message:", err)
	}
}

func (s *MessagingService) UpdateAssistantMessageWithCallback(assistantMessage *entities.Message, content string, callback func()) {
	s.UpdateAssistantMessage(assistantMessage, content)
	if callback != nil {
		callback()
	}
}

// GenerateChatResponse - MAIN ROUTER
func (s *MessagingService) GenerateChatResponse(session *entities.Session, messages []providers.ChatMessage) (<-chan string, *entities.Message, error) {
	isGemma := isGemmaModel(session.Chatbot.ModelName)

	if isGemma {
		log.Info("🔍 Using Gemma response generation for:", session.Chatbot.ModelName)
		return s.generateChatResponseGemma(session, messages)
	} else {
		log.Info("🔍 Using OpenAI response generation for:", session.Chatbot.ModelName)
		return s.generateChatResponseOpenAI(session, messages)
	}
}
