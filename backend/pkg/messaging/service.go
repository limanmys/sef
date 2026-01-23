package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sef/app/entities"
	"sef/internal/validation"
	"sef/pkg/providers"
	"sef/pkg/rag"
	"sef/pkg/toolrunners"
	"sef/pkg/toon"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3/log"
	"gorm.io/gorm"
)

type SendMessageRequest struct {
	Content          string `json:"content" validate:"required,min=1"`
	WebSearchEnabled bool   `json:"web_search_enabled"`
}

type MessagingService struct {
	DB         *gorm.DB
	RAGService *rag.RAGService
}

type MessagingServiceInterface interface {
	ValidateAndParseSessionID(sessionIDStr string) (uint, error)
	GetSessionByIDAndUser(sessionID, userID uint) (*entities.Session, error)
	ParseSendMessageRequest(body []byte) (*SendMessageRequest, error)
	LoadSessionWithChatbotAndMessages(sessionID, userID uint) (*entities.Session, error)
	LoadSessionWithChatbotToolsAndMessages(sessionID, userID uint) (*entities.Session, error)
	SaveUserMessage(sessionID uint, content string) error
	PrepareChatMessages(session *entities.Session, userContent string) ([]providers.ChatMessage, *rag.AugmentPromptResult)
	CreateAssistantMessage(sessionID uint) (*entities.Message, error)
	CreateToolMessage(sessionID uint, content string) (*entities.Message, error)
	GenerateChatResponse(session *entities.Session, messages []providers.ChatMessage, ragResult *rag.AugmentPromptResult, webSearchEnabled bool) (<-chan string, *entities.Message, error)
	UpdateAssistantMessage(assistantMessage *entities.Message, content string)
	UpdateAssistantMessageWithCallback(assistantMessage *entities.Message, content string, callback func())
	ConvertToolsToDefinitions(tools []entities.Tool, format string) []providers.ToolDefinition
	GetWebSearchToolDefinition(format string) providers.ToolDefinition
	ExecuteToolCall(ctx context.Context, toolCall providers.ToolCall, outputFormat string) (string, error)
}

// ValidateAndParseSessionID validates and parses session ID from string
func (s *MessagingService) ValidateAndParseSessionID(sessionIDStr string) (uint, error) {
	sessionID, err := strconv.ParseUint(sessionIDStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid session ID: %w", err)
	}
	return uint(sessionID), nil
}

// GetSessionByIDAndUser retrieves a session by ID and user ID
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

// ParseSendMessageRequest parses and validates the send message request from body bytes
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

// LoadSessionWithChatbotAndMessages loads session with chatbot and messages
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

// LoadSessionWithChatbotToolsAndMessages loads session with chatbot, tools, and messages
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

// ConvertToolsToDefinitions converts entity tools to provider tool definitions
func (s *MessagingService) ConvertToolsToDefinitions(tools []entities.Tool, format string) []providers.ToolDefinition {
	var definitions []providers.ToolDefinition
	for _, tool := range tools {
		// Convert JSONB parameters to OpenAPI JSON Schema format
		parameters := map[string]interface{}{
			"type":       "object",
			"properties": make(map[string]interface{}),
			"required":   []string{},
		}

		if len(tool.Parameters) > 0 {
			properties := make(map[string]interface{})
			var required []string

			// Process each parameter in the array
			for _, param := range tool.Parameters {
				if paramMap, ok := param.(map[string]interface{}); ok {
					name, hasName := paramMap["name"].(string)
					paramType, hasType := paramMap["type"].(string)
					description, hasDesc := paramMap["description"].(string)
					isRequired, hasRequired := paramMap["required"].(bool)

					if hasName && hasType {
						// Create property definition
						property := map[string]interface{}{
							"type": paramType,
						}
						if hasDesc {
							property["description"] = description
						}
						properties[name] = property

						// Add to required array if marked as required
						if hasRequired && isRequired {
							required = append(required, name)
						}
					}
				}
			}

			parameters["properties"] = properties
			parameters["required"] = required
		}

		// Convert to TOON format if requested
		if format == "toon" {
			toonConverter := toon.NewConverter()
			toonStr, err := toonConverter.ConvertMapToTOON(parameters)
			if err != nil {
				log.Errorf("Error converting parameters to TOON: %v", err)
				// Fall back to JSON format on error
			} else {
				// Store TOON string representation in a special field
				parameters = map[string]interface{}{
					"type":         "toon",
					"toon_content": toonStr,
				}
			}
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

// GetWebSearchToolDefinition returns the tool definition for web search
func (s *MessagingService) GetWebSearchToolDefinition(format string) providers.ToolDefinition {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query to execute. Be specific and clear about what you're searching for.",
			},
			"num_results": map[string]interface{}{
				"type":        "number",
				"description": "Number of search results to return (default: 5, max: 10)",
				"default":     5,
			},
		},
		"required": []string{"query"},
	}

	// Convert to TOON format if requested
	if format == "toon" {
		toonConverter := toon.NewConverter()
		toonStr, err := toonConverter.ConvertMapToTOON(parameters)
		if err != nil {
			log.Errorf("Error converting web search parameters to TOON: %v", err)
			// Fall back to JSON format on error
		} else {
			// Store TOON string representation in a special field
			parameters = map[string]interface{}{
				"type":         "toon",
				"toon_content": toonStr,
			}
		}
	}

	return providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunction{
			Name:        "web_search",
			Description: "Search the web for current information, news, facts, or any topic. Use this when you need up-to-date information or when the user asks about current events, recent developments, or information you don't have in your training data.",
			Parameters:  parameters,
		},
	}
}

// ExecuteToolCall executes a tool call and returns the result
func (s *MessagingService) ExecuteToolCall(ctx context.Context, toolCall providers.ToolCall, outputFormat string) (string, error) {
	// Check if this is a web search tool call
	if toolCall.Function.Name == "web_search" {
		return s.executeWebSearchTool(ctx, toolCall, outputFormat)
	}

	// Find the tool by name (this would need to be optimized in production)
	var tool entities.Tool
	if err := s.DB.Where("name = ?", toolCall.Function.Name).First(&tool).Error; err != nil {
		return "", fmt.Errorf("tool not found: %s", toolCall.Function.Name)
	}

	// Handle arguments - they might be raw JSON string or parsed map
	var args map[string]interface{}
	if rawArgs, ok := toolCall.Function.Arguments["raw"].(string); ok {
		// Parse the raw JSON string
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	} else {
		// Already parsed
		args = toolCall.Function.Arguments
	}

	// Create tool runner
	factory := &toolrunners.ToolRunnerFactory{}
	runner, err := factory.NewToolRunner(tool.Type, tool.Config, tool.Parameters)
	if err != nil {
		return "", fmt.Errorf("failed to create tool runner: %w", err)
	}

	// Create tool call context
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

	// Execute tool with context
	result, err := runner.ExecuteWithContext(ctx, args, toolContext)
	if err != nil {
		return "", fmt.Errorf("tool execution failed: %w", err)
	}

	// Convert result based on output format
	if outputFormat == "toon" {
		log.Infof("Converting tool output to TOON format for tool: %s", toolCall.Function.Name)
		toonConverter := toon.NewConverter()
		toonStr, err := toonConverter.ConvertToTOON(result)
		if err != nil {
			log.Errorf("Error converting tool output to TOON: %v, falling back to JSON", err)
			// Fall back to JSON if TOON conversion fails
			resultJSON, jsonErr := json.Marshal(result)
			if jsonErr != nil {
				return "", fmt.Errorf("failed to marshal tool result: %w", jsonErr)
			}
			log.Infof("Tool output (JSON fallback) length: %d bytes", len(resultJSON))
			return string(resultJSON), nil
		}
		log.Infof("=== TOON Tool Output ===")
		log.Infof("Tool: %s", toolCall.Function.Name)
		log.Infof("TOON Output length: %d bytes", len(toonStr))
		// log.Infof("TOON Content:\n%s", toonStr) // Uncomment for full content logging
		log.Infof("========================")
		return toonStr, nil
	}

	// Default to JSON format
	log.Infof("Using JSON format for tool output: %s", toolCall.Function.Name)
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tool result: %w", err)
	}
	log.Infof("Tool output (JSON) length: %d bytes", len(resultJSON))

	return string(resultJSON), nil
}

// executeWebSearchTool executes a web search tool call
func (s *MessagingService) executeWebSearchTool(ctx context.Context, toolCall providers.ToolCall, outputFormat string) (string, error) {
	// Handle arguments - they might be raw JSON string or parsed map
	var args map[string]interface{}
	if rawArgs, ok := toolCall.Function.Arguments["raw"].(string); ok {
		// Parse the raw JSON string
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	} else {
		// Already parsed
		args = toolCall.Function.Arguments
	}

	// Create web search tool runner
	runner := toolrunners.NewWebSearchToolRunner()

	// Create tool call context
	toolContext := &toolrunners.ToolCallContext{
		ToolCallID:   toolCall.ID,
		FunctionName: toolCall.Function.Name,
		ToolName:     "Web Search",
		Metadata: map[string]interface{}{
			"tool_type":        "web_search",
			"tool_description": "Search the web for current information",
		},
	}

	// Execute tool with context
	result, err := runner.ExecuteWithContext(ctx, args, toolContext)
	if err != nil {
		return "", fmt.Errorf("web search execution failed: %w", err)
	}

	// Convert result based on output format
	if outputFormat == "toon" {
		log.Info("Converting web search output to TOON format")
		toonConverter := toon.NewConverter()
		toonStr, err := toonConverter.ConvertToTOON(result)
		if err != nil {
			log.Errorf("Error converting web search output to TOON: %v, falling back to JSON", err)
			// Fall back to JSON if TOON conversion fails
			resultJSON, jsonErr := json.Marshal(result)
			if jsonErr != nil {
				return "", fmt.Errorf("failed to marshal web search result: %w", jsonErr)
			}
			log.Infof("Web search output (JSON fallback) length: %d bytes", len(resultJSON))
			return string(resultJSON), nil
		}
		log.Infof("=== TOON Web Search Output ===")
		log.Infof("TOON Output length: %d bytes", len(toonStr))
		// log.Infof("TOON Content:\n%s", toonStr) // Uncomment for full content logging
		log.Infof("===============================")
		return toonStr, nil
	}

	// Default to JSON format
	log.Info("Using JSON format for web search output")
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal web search result: %w", err)
	}
	log.Infof("Web search output (JSON) length: %d bytes", len(resultJSON))

	return string(resultJSON), nil
}

// SaveUserMessage saves the user message to database
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

// cleanAssistantContent removes internal tags from assistant content before saving
func cleanAssistantContent(content string) string {
	// Remove <think> tags and content
	re := regexp.MustCompile(`<think>.*?</think>`)
	content = re.ReplaceAllString(content, "")

	// Remove <tool_executing> tags and content
	re = regexp.MustCompile(`<tool_executing>.*?</tool_executing>`)
	content = re.ReplaceAllString(content, "")

	// Remove <tool_executed> tags and content
	re = regexp.MustCompile(`<tool_executed>.*?</tool_executed>`)
	content = re.ReplaceAllString(content, "")

	// Remove <document_used> tags and content
	re = regexp.MustCompile(`<document_used>.*?</document_used>`)
	content = re.ReplaceAllString(content, "")

	return strings.TrimSpace(content)
}

// PrepareChatMessages prepares the messages array for the chat API
func (s *MessagingService) PrepareChatMessages(session *entities.Session, userContent string) ([]providers.ChatMessage, *rag.AugmentPromptResult) {
	var messages []providers.ChatMessage
	var ragResult *rag.AugmentPromptResult

	// Add system message if system prompt exists
	if session.Chatbot.SystemPrompt != "" {
		messages = append(messages, providers.ChatMessage{
			Role:    "system",
			Content: session.Chatbot.SystemPrompt,
		})
	}

	// Add current chat session messages
	for _, msg := range session.Messages {
		messages = append(messages, providers.ChatMessage{
			Role:    msg.Role,
			Content: cleanAssistantContent(msg.Content),
		})
	}

	// Check if RAG is available and augment the user message if needed
	augmentedContent := userContent
	if s.RAGService != nil {
		// Check if chatbot has documents
		isAvailable, err := s.RAGService.IsRAGAvailable(session.Chatbot.ID)
		if err != nil {
			log.Warn("Failed to check RAG availability:", err)
		} else if isAvailable {
			// Augment the prompt with RAG context
			// Using 7 chunks provides good context while staying within token limits
			result, err := s.RAGService.AugmentPrompt(context.Background(), userContent, session.Chatbot.ID, 0)
			if err != nil {
				log.Warn("Failed to augment prompt with RAG:", err)
			} else if result != nil {
				augmentedContent = result.AugmentedPrompt
				ragResult = result
				log.Info("RAG augmented prompt with", len(result.DocumentsUsed), "documents")
			}
		}
	}

	// Add current user message (possibly augmented with RAG context)
	messages = append(messages, providers.ChatMessage{
		Role:    "user",
		Content: augmentedContent,
	})

	return messages, ragResult
}

// CreateAssistantMessage creates an empty assistant message record
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

// CreateToolMessage creates a tool message record
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

// processToolCalls handles the execution of tool calls and returns updated messages
// Returns: updated messages, shouldStop flag, stop reason
func (s *MessagingService) processToolCalls(session *entities.Session, toolCalls []providers.ToolCall, messages []providers.ChatMessage, outputCh chan<- string, assistantContent *strings.Builder, toolCallCounter map[string]int, outputFormat string) ([]providers.ChatMessage, bool, string) {
	for _, toolCall := range toolCalls {
		displayName := toolCall.Function.Name
		// Extract tool display name from session.Chatbot.Tools
		for _, t := range session.Chatbot.Tools {
			if t.Name == toolCall.Function.Name {
				displayName = t.DisplayName
				break
			}
		}

		// Ensure we have a valid display name, fallback to function name if empty
		if displayName == "" {
			if toolCall.Function.Name != "" {
				displayName = toolCall.Function.Name
			} else {
				displayName = "Unknown Tool"
			}
		}

		if displayName == "web_search" {
			displayName = "Web Search"
		}

		// Check if this tool has been called too many times
		toolCallCounter[toolCall.Function.Name]++
		if toolCallCounter[toolCall.Function.Name] > 2 {
			log.Warn("Tool", toolCall.Function.Name, "has been called more than 2 times, stopping execution")
			errorMsg := fmt.Sprintf("Özür dilerim, '%s' aracını kullanarak istediğiniz bilgiyi alamadım. Lütfen sorunuzu farklı bir şekilde sorun veya daha spesifik bilgi verin.", displayName)
			outputCh <- errorMsg
			assistantContent.WriteString(errorMsg)
			return messages, true, "tool_call_limit_exceeded"
		}

		log.Info("Calling tool", toolCall.Function.Name, "- attempt", toolCallCounter[toolCall.Function.Name], "of 2")

		// Send tool executing indicator
		executingStr := fmt.Sprintf("<tool_executing>%s</tool_executing>", displayName)
		outputCh <- executingStr
		assistantContent.WriteString(executingStr)

		toolResult, err := s.ExecuteToolCall(context.Background(), toolCall, outputFormat)
		if err != nil {
			log.Error("Tool execution failed:", err)
			// Provide more user-friendly tool error messages
			if strings.Contains(err.Error(), "not found") {
				toolResult = fmt.Sprintf("The tool '%s' is not available or has been removed.", displayName)
			} else if strings.Contains(err.Error(), "timeout") {
				toolResult = fmt.Sprintf("The tool '%s' took too long to respond. Please try again.", displayName)
			} else if strings.Contains(err.Error(), "arguments") {
				toolResult = fmt.Sprintf("There was an issue with the parameters provided to '%s'. Please try rephrasing your request.", displayName)
			} else {
				toolResult = fmt.Sprintf("Tool '%s' encountered an error: %v", displayName, err)
			}
		}

		// Send tool executed indicator
		executedStr := fmt.Sprintf("<tool_executed>%s</tool_executed>", displayName)
		outputCh <- executedStr
		assistantContent.WriteString(executedStr)

		// Save tool message
		_, err = s.CreateToolMessage(session.ID, toolResult)
		if err != nil {
			log.Error("Failed to save tool message:", err)
		}

		// Add to messages for followup
		toolMessage := providers.ChatMessage{
			Role:    "tool",
			Content: toolResult,
		}
		messages = append(messages, toolMessage)
	}
	return messages, false, ""
}

// GenerateChatResponse generates the chat response stream with infinite tool call chain support
func (s *MessagingService) GenerateChatResponse(session *entities.Session, messages []providers.ChatMessage, ragResult *rag.AugmentPromptResult, webSearchEnabled bool) (<-chan string, *entities.Message, error) {
	// Create provider instance
	factory := &providers.ProviderFactory{}
	providerConfig := map[string]interface{}{
		"base_url": session.Chatbot.Provider.BaseURL,
	}

	// Validate provider configuration
	if session.Chatbot.Provider.Type == "" {
		log.Error("Provider type is empty for chatbot:", session.Chatbot.Name)
		return nil, nil, fmt.Errorf("provider type is not configured for chatbot: %s", session.Chatbot.Name)
	}

	if session.Chatbot.Provider.BaseURL == "" {
		log.Error("Provider base URL is empty for chatbot:", session.Chatbot.Name)
		return nil, nil, fmt.Errorf("provider base URL is not configured for chatbot: %s", session.Chatbot.Name)
	}

	log.Info("Creating provider with config:", map[string]interface{}{
		"type":         session.Chatbot.Provider.Type,
		"base_url":     session.Chatbot.Provider.BaseURL,
		"chatbot_id":   session.Chatbot.ID,
		"chatbot_name": session.Chatbot.Name,
	})

	provider, err := factory.NewProvider(session.Chatbot.Provider.Type, providerConfig)
	if err != nil {
		log.Error("Failed to create provider:", err, "Provider type:", session.Chatbot.Provider.Type, "Config:", providerConfig)
		return nil, nil, fmt.Errorf("failed to initialize provider: %w", err)
	}

	log.Info("Provider created successfully for chatbot:", session.Chatbot.Name)

	// Prepare options
	options := make(map[string]interface{})
	if session.Chatbot.ModelName != "" {
		options["model"] = session.Chatbot.ModelName
		log.Info("Using model from chatbot:", session.Chatbot.ModelName, "for chatbot:", session.Chatbot.Name)
	} else {
		log.Info("Using default model for chatbot:", session.Chatbot.Name)
	}

	// Add model parameters from chatbot settings
	if session.Chatbot.Temperature != nil {
		options["temperature"] = *session.Chatbot.Temperature
	}
	if session.Chatbot.TopP != nil {
		options["top_p"] = *session.Chatbot.TopP
	}
	if session.Chatbot.TopK != nil {
		options["top_k"] = *session.Chatbot.TopK
	}

	// Add additional logging for debugging
	log.Info("Chat generation parameters:", map[string]interface{}{
		"session_id":     session.ID,
		"chatbot_id":     session.Chatbot.ID,
		"chatbot_name":   session.Chatbot.Name,
		"provider_type":  session.Chatbot.Provider.Type,
		"model_name":     session.Chatbot.ModelName,
		"tools_count":    len(session.Chatbot.Tools),
		"messages_count": len(messages),
	})

	// Get tool format from chatbot settings, default to "json"
	toolFormat := session.Chatbot.ToolFormat
	if toolFormat == "" {
		toolFormat = "json"
	}

	// Get output format from chatbot settings, default to "json"
	outputFormat := session.Chatbot.OutputFormat
	if outputFormat == "" {
		outputFormat = "json"
	}
	log.Infof("Tool output format for session %d: %s", session.ID, outputFormat)

	// Convert tools to definitions
	toolDefinitions := s.ConvertToolsToDefinitions(session.Chatbot.Tools, toolFormat)

	// Add web search tool if enabled for this message and chatbot supports it
	if webSearchEnabled && session.Chatbot.WebSearchEnabled {
		webSearchTool := s.GetWebSearchToolDefinition(toolFormat)
		toolDefinitions = append(toolDefinitions, webSearchTool)
		log.Info("Web search tool enabled for this message in session:", session.ID)
	}

	// Log tool definitions for debugging
	if toolFormat == "toon" && len(toolDefinitions) > 0 {
		log.Info("=== TOON Format Tool Definitions ===")
		for i, toolDef := range toolDefinitions {
			log.Infof("Tool %d: %s", i+1, toolDef.Function.Name)
			/*if toonContent, ok := toolDef.Function.Parameters["toon_content"].(string); ok {
				log.Infof("TOON Content:\n%s", toonContent)
			}*/ // Uncomment for full content logging
		}
		log.Info("====================================")
	} // Create output channel
	outputCh := make(chan string)

	// Create first assistant message synchronously
	firstAssistant, err := s.CreateAssistantMessage(session.ID)
	if err != nil {
		log.Error("Failed to create assistant message:", err)
		return nil, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	go func() {
		defer close(outputCh)

		var assistantContent strings.Builder
		thinkingStarted := false
		currentMessages := messages

		// Stream document used indicators if RAG was used
		if ragResult != nil && len(ragResult.DocumentsUsed) > 0 {
			for _, doc := range ragResult.DocumentsUsed {
				docTag := fmt.Sprintf("<document_used>%s (Skor: %.2f)</document_used>", doc.Title, doc.Score)
				outputCh <- docTag
				// Note: We stream this to frontend but DON'T add to assistantContent
				// so it won't be saved to DB (cleanAssistantContent will remove it anyway)
			}
		}

		// Keep-alive ticker to prevent timeouts
		keepAliveTicker := time.NewTicker(30 * time.Second)
		defer keepAliveTicker.Stop()

		// Maximum iterations to prevent infinite loops
		const maxIterations = 10
		iteration := 0

		// Track tool call counts - each tool can be called at most 2 times
		toolCallCounter := make(map[string]int)

		// Continuous loop to handle infinite tool call chains
		for {
			iteration++
			if iteration > maxIterations {
				log.Warn("Maximum tool call iterations reached for session:", session.ID)
				errorMsg := "Özür dilerim, çok fazla araç çağrısı yapıldı. Lütfen sorunuzu daha basit bir şekilde sorun."
				outputCh <- errorMsg
				assistantContent.WriteString(errorMsg)
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			log.Info("Tool call iteration", iteration, "of", maxIterations, "for session:", session.ID)

			// Generate chat response
			log.Info("Calling GenerateChatWithTools for session:", session.ID, "with", len(currentMessages), "messages")
			chatStream, err := provider.GenerateChatWithTools(context.Background(), currentMessages, toolDefinitions, options)
			if err != nil {
				log.Error("Failed to generate response:", err)
				// Kullanıcı dostu hata mesajı gönder
				errorMsg := "Özür dilerim, şu anda yanıt oluşturmakta zorlanıyorum. "
				if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "timeout") {
					errorMsg += "AI servisi ile bağlantı sorunu yaşanıyor gibi görünüyor. Lütfen bir süre sonra tekrar deneyin."
				} else if strings.Contains(err.Error(), "authentication") || strings.Contains(err.Error(), "auth") {
					errorMsg += "AI servisi ile kimlik doğrulama sorunu yaşanıyor. Lütfen bir yönetici ile iletişime geçin."
				} else if strings.Contains(err.Error(), "model") || strings.Contains(err.Error(), "does not support") {
					errorMsg += "Seçilen AI modeli kullanılamıyor veya araçları desteklemiyor. Lütfen farklı bir chatbot deneyin veya bir yönetici ile iletişime geçin."
				} else {
					errorMsg += fmt.Sprintf("Hata detayları: %v", err)
				}

				log.Info("Sending error message to client:", errorMsg)
				outputCh <- errorMsg

				// Assistant mesajını hata içeriği ile güncelle
				s.UpdateAssistantMessage(firstAssistant, errorMsg)
				return
			}

			log.Info("GenerateChatWithTools call successful, processing stream...")

			hasToolCalls := false
			var pendingToolCalls []providers.ToolCall

			// Process the stream
			responseCount := 0
			for response := range chatStream {
				responseCount++

				// Handle thinking tokens
				if response.Thinking != "" {
					if !thinkingStarted {
						outputCh <- "<think>"
						thinkingStarted = true
					}
					outputCh <- response.Thinking
					assistantContent.WriteString("<think>" + response.Thinking)
				} else if thinkingStarted {
					outputCh <- "</think>"
					thinkingStarted = false
					assistantContent.WriteString("</think>")
				}

				// Handle content
				if response.Content != "" {
					outputCh <- response.Content
					assistantContent.WriteString(response.Content)
				}

				// Collect tool calls
				if len(response.ToolCalls) > 0 {
					log.Info("Agent tool calls: ", response.ToolCalls)
					hasToolCalls = true
					pendingToolCalls = append(pendingToolCalls, response.ToolCalls...)
				}

				// If response is done, process any collected tool calls
				if response.Done {
					log.Info("Response marked as done after", responseCount, "responses")
					if thinkingStarted {
						outputCh <- "</think>"
						thinkingStarted = false
						assistantContent.WriteString("</think>")
					}
					break
				}
			}

			log.Info("Stream processing finished. Total responses:", responseCount, "HasToolCalls:", hasToolCalls)

			// If no tool calls were made, we're done
			if !hasToolCalls {
				// Update the assistant message with full content
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			// Close thinking if open before processing tools
			if thinkingStarted {
				outputCh <- "</think>"
				thinkingStarted = false
				assistantContent.WriteString("</think>")
			}

			// IMPORTANT: Add the assistant's response to the message history
			// This helps the LLM understand what it has already said and prevents re-calling tools
			assistantMessage := providers.ChatMessage{
				Role:    "assistant",
				Content: cleanAssistantContent(assistantContent.String()),
			}
			currentMessages = append(currentMessages, assistantMessage)

			// Process tool calls and update messages for next iteration
			var shouldStop bool
			var stopReason string
			currentMessages, shouldStop, stopReason = s.processToolCalls(session, pendingToolCalls, currentMessages, outputCh, &assistantContent, toolCallCounter, outputFormat)

			// If we should stop (e.g., tool call limit exceeded), save message and exit
			if shouldStop {
				log.Info("Stopping tool execution loop. Reason:", stopReason)
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}
		}
	}()

	return outputCh, firstAssistant, nil
}

// UpdateAssistantMessage updates the assistant message with the full response
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

// UpdateAssistantMessageWithCallback updates the assistant message and executes a callback
func (s *MessagingService) UpdateAssistantMessageWithCallback(assistantMessage *entities.Message, content string, callback func()) {
	s.UpdateAssistantMessage(assistantMessage, content)
	if callback != nil {
		callback()
	}
}
