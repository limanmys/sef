package messaging

import (
	"context"
	"fmt"
	"sef/app/entities"
	"sef/pkg/providers"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3/log"
)

// generateChatResponseOpenAI - GPT-OSS için basit implementasyon
func (s *MessagingService) generateChatResponseOpenAI(session *entities.Session, messages []providers.ChatMessage) (<-chan string, *entities.Message, error) {
	factory := &providers.ProviderFactory{}
	providerConfig := map[string]interface{}{
		"base_url": session.Chatbot.Provider.BaseURL,
	}

	if session.Chatbot.Provider.Type == "" {
		return nil, nil, fmt.Errorf("provider type is not configured")
	}

	if session.Chatbot.Provider.BaseURL == "" {
		return nil, nil, fmt.Errorf("provider base URL is not configured")
	}

	provider, err := factory.NewProvider(session.Chatbot.Provider.Type, providerConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize provider: %w", err)
	}

	options := make(map[string]interface{})
	if session.Chatbot.ModelName != "" {
		options["model"] = session.Chatbot.ModelName
	}

	toolDefinitions := s.ConvertToolsToDefinitions(session.Chatbot.Tools)
	outputCh := make(chan string)

	firstAssistant, err := s.CreateAssistantMessage(session.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	go func() {
		defer close(outputCh)

		var assistantContent strings.Builder
		thinkingStarted := false
		currentMessages := messages

		keepAliveTicker := time.NewTicker(30 * time.Second)
		defer keepAliveTicker.Stop()

		const maxIterations = 10
		iteration := 0
		toolCallCounter := make(map[string]int)

		for {
			iteration++
			if iteration > maxIterations {
				log.Warn("Max iterations reached")
				errorMsg := "Özür dilerim, çok fazla araç çağrısı yapıldı."
				outputCh <- errorMsg
				assistantContent.WriteString(errorMsg)
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			chatStream, err := provider.GenerateChatWithTools(context.Background(), currentMessages, toolDefinitions, options)
			if err != nil {
				log.Error("Failed to generate response:", err)
				errorMsg := "Özür dilerim, yanıt oluşturamadım."
				outputCh <- errorMsg
				s.UpdateAssistantMessage(firstAssistant, errorMsg)
				return
			}

			hasToolCalls := false
			var pendingToolCalls []providers.ToolCall

			for response := range chatStream {
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

				if response.Content != "" {
					outputCh <- response.Content
					assistantContent.WriteString(response.Content)
				}

				if len(response.ToolCalls) > 0 {
					hasToolCalls = true
					pendingToolCalls = append(pendingToolCalls, response.ToolCalls...)
				}

				if response.Done {
					if thinkingStarted {
						outputCh <- "</think>"
						thinkingStarted = false
						assistantContent.WriteString("</think>")
					}
					break
				}
			}

			if !hasToolCalls {
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			if thinkingStarted {
				outputCh <- "</think>"
				thinkingStarted = false
				assistantContent.WriteString("</think>")
			}

			assistantMessage := providers.ChatMessage{
				Role:    "assistant",
				Content: cleanAssistantContent(assistantContent.String()),
			}
			currentMessages = append(currentMessages, assistantMessage)

			var shouldStop bool
			currentMessages, shouldStop, _ = s.processToolCallsSimple(session, pendingToolCalls, currentMessages, outputCh, &assistantContent, toolCallCounter)

			if shouldStop {
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}
		}
	}()

	return outputCh, firstAssistant, nil
}

// processToolCallsSimple - OpenAI için basit tool call processing
func (s *MessagingService) processToolCallsSimple(session *entities.Session, toolCalls []providers.ToolCall, messages []providers.ChatMessage, outputCh chan<- string, assistantContent *strings.Builder, toolCallCounter map[string]int) ([]providers.ChatMessage, bool, string) {
	for _, toolCall := range toolCalls {
		displayName := toolCall.Function.Name
		for _, t := range session.Chatbot.Tools {
			if t.Name == toolCall.Function.Name {
				displayName = t.DisplayName
				break
			}
		}

		if displayName == "" {
			displayName = toolCall.Function.Name
		}

		toolCallCounter[toolCall.Function.Name]++
		if toolCallCounter[toolCall.Function.Name] > 2 {
			errorMsg := fmt.Sprintf("Araç '%s' limit aşıldı.", displayName)
			outputCh <- errorMsg
			assistantContent.WriteString(errorMsg)
			return messages, true, "limit_exceeded"
		}

		executingStr := fmt.Sprintf("<tool_executing>%s</tool_executing>", displayName)
		outputCh <- executingStr
		assistantContent.WriteString(executingStr)

		toolResult, err := s.ExecuteToolCall(context.Background(), toolCall)
		if err != nil {
			toolResult = fmt.Sprintf("Tool error: %v", err)
		}

		executedStr := fmt.Sprintf("<tool_executed>%s</tool_executed>", displayName)
		outputCh <- executedStr
		assistantContent.WriteString(executedStr)

		s.CreateToolMessage(session.ID, toolResult)

		toolMessage := providers.ChatMessage{
			Role:    "tool",
			Content: toolResult,
		}
		messages = append(messages, toolMessage)
	}
	return messages, false, ""
}
