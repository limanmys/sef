package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sef/app/entities"
	"sef/pkg/providers"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3/log"
)

// ============================================================================
// GEMMA HELPER FUNCTIONS
// ============================================================================

// extractThinkingFromContent extracts <think> tags and returns thinking + regular content
func extractThinkingFromContent(content string) (thinking string, regular string) {
	thinkRegex := regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	matches := thinkRegex.FindAllStringSubmatch(content, -1)

	var thinkingParts []string
	for _, match := range matches {
		if len(match) > 1 {
			thinkingParts = append(thinkingParts, strings.TrimSpace(match[1]))
		}
	}

	regularContent := thinkRegex.ReplaceAllString(content, "")
	return strings.Join(thinkingParts, "\n"), strings.TrimSpace(regularContent)
}

// balanceJSON attempts to balance JSON braces if they're unmatched
func balanceJSON(jsonStr string) string {
	openBraces := strings.Count(jsonStr, "{")
	closeBraces := strings.Count(jsonStr, "}")

	if openBraces > closeBraces {
		jsonStr += strings.Repeat("}", openBraces-closeBraces)
		log.Info("Balanced JSON: added", openBraces-closeBraces, "closing braces")
	}
	return jsonStr
}

// detectAndParseToolCallsFromContent analyzes content for Gemma-style tool calls
func (s *MessagingService) detectAndParseToolCallsFromContent(content string) ([]providers.ToolCall, string, string) {
	if strings.Contains(content, "<tool_call>") || strings.Contains(content, "<tool_calls>") {
		toolCalls := s.parseGemmaToolCalls(content)
		if len(toolCalls) > 0 {
			log.Info("Detected Gemma3 format tool calls in content:", len(toolCalls), "calls")
			cleanedContent := s.stripToolCallTags(content)
			return toolCalls, "gemma", cleanedContent
		}
	}
	return nil, "none", content
}

// stripToolCallTags removes tool call XML tags from content
func (s *MessagingService) stripToolCallTags(content string) string {
	re := regexp.MustCompile(`(?s)<tool_calls>.*?</tool_calls>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)
	content = re.ReplaceAllString(content, "")

	return strings.TrimSpace(content)
}

// parseGemmaToolCalls extracts tool calls from Gemma3 XML format
func (s *MessagingService) parseGemmaToolCalls(content string) []providers.ToolCall {
	var toolCalls []providers.ToolCall

	toolCallRegex := regexp.MustCompile(`<tool_call>\s*([\s\S]*?)\s*</tool_call>`)
	matches := toolCallRegex.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		log.Info("No Gemma tool call patterns found in content")
		return toolCalls
	}

	log.Info("Found", len(matches), "Gemma tool call patterns in content")

	for i, match := range matches {
		if len(match) < 2 {
			continue
		}

		jsonStr := strings.TrimSpace(match[1])
		jsonStr = balanceJSON(jsonStr)

		log.Info(fmt.Sprintf("Parsing Gemma tool call #%d, JSON length: %d chars", i+1, len(jsonStr)))

		var toolCallData map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &toolCallData); err != nil {
			log.Error("Failed to parse Gemma tool call JSON:", err)
			preview := jsonStr
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			log.Error("JSON preview:", preview)
			continue
		}

		name, nameOk := toolCallData["name"].(string)
		if !nameOk {
			log.Error("Invalid Gemma tool call - missing 'name' field:", toolCallData)
			continue
		}

		var params map[string]interface{}
		if p, ok := toolCallData["parameters"].(map[string]interface{}); ok {
			params = p
		} else if p, ok := toolCallData["arguments"].(map[string]interface{}); ok {
			params = p
		} else {
			params = make(map[string]interface{})
		}

		toolCall := providers.ToolCall{
			ID:   fmt.Sprintf("gemma_call_%d_%d", time.Now().UnixNano(), i),
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      name,
				Arguments: params,
			},
		}

		log.Info("✅ Successfully parsed Gemma tool call:", name, "with", len(params), "parameters")
		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls
}

// formatToolOutputForModel formats tool output for Gemma
func formatToolOutputForModel(result string, detectedFormat string) string {
	if detectedFormat == "gemma" {
		return fmt.Sprintf("<tool_outputs>\n<tool_output>\n%s\n</tool_output>\n</tool_outputs>", result)
	}
	return result
}

// ============================================================================
// TOOL CALL PROCESSING FOR GEMMA
// ============================================================================

func (s *MessagingService) processToolCallsGemma(session *entities.Session, toolCalls []providers.ToolCall, messages []providers.ChatMessage, outputCh chan<- string, assistantContent *strings.Builder, toolCallCounter map[string]int, detectedFormat string) ([]providers.ChatMessage, bool, string) {
	for _, toolCall := range toolCalls {
		displayName := toolCall.Function.Name
		for _, t := range session.Chatbot.Tools {
			if t.Name == toolCall.Function.Name {
				displayName = t.DisplayName
				break
			}
		}

		if displayName == "" {
			if toolCall.Function.Name != "" {
				displayName = toolCall.Function.Name
			} else {
				displayName = "Unknown Tool"
			}
		}

		toolCallCounter[toolCall.Function.Name]++
		if toolCallCounter[toolCall.Function.Name] > 2 {
			log.Warn("Tool", toolCall.Function.Name, "has been called more than 2 times, stopping execution")
			errorMsg := fmt.Sprintf("Özür dilerim, '%s' aracını kullanarak istediğiniz bilgiyi alamadım. Lütfen sorunuzu farklı bir şekilde sorun veya daha spesifik bilgi verin.", displayName)
			outputCh <- errorMsg
			assistantContent.WriteString(errorMsg)
			return messages, true, "tool_call_limit_exceeded"
		}

		log.Info("Calling tool", toolCall.Function.Name, "- attempt", toolCallCounter[toolCall.Function.Name], "of 2")

		executingStr := fmt.Sprintf("<tool_executing>%s</tool_executing>", displayName)
		outputCh <- executingStr
		assistantContent.WriteString(executingStr)

		toolResult, err := s.ExecuteToolCall(context.Background(), toolCall)
		if err != nil {
			log.Error("Tool execution failed:", err)
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

		executedStr := fmt.Sprintf("<tool_executed>%s</tool_executed>", displayName)
		outputCh <- executedStr
		assistantContent.WriteString(executedStr)

		formattedResult := formatToolOutputForModel(toolResult, detectedFormat)
		//guidanceMessage := "\n\nÖnemli: Yukarıdaki veriyi analiz et ve kullanıcıya Türkçe, özetlenmiş, anlaşılır bir şekilde sun. Ham JSON'u veya teknik çıktıyı gösterme."

		//_, err = s.CreateToolMessage(session.ID, formattedResult + guidanceMessage)
		_, err = s.CreateToolMessage(session.ID, formattedResult)
		
		if err != nil {
			log.Error("Failed to save tool message:", err)
		}

		toolMessage := providers.ChatMessage{
			Role:    "tool",
			Content: formattedResult,
		}
		messages = append(messages, toolMessage)
	}
	return messages, false, ""
}

// ============================================================================
// MAIN GEMMA CHAT RESPONSE GENERATOR
// ============================================================================

// generateChatResponseGemma generates chat response for Gemma models with enhanced features
func (s *MessagingService) generateChatResponseGemma(session *entities.Session, messages []providers.ChatMessage) (<-chan string, *entities.Message, error) {
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

	// Add additional logging for debugging
	log.Info("Chat generation parameters (Gemma mode):", map[string]interface{}{
		"session_id":     session.ID,
		"chatbot_id":     session.Chatbot.ID,
		"chatbot_name":   session.Chatbot.Name,
		"provider_type":  session.Chatbot.Provider.Type,
		"model_name":     session.Chatbot.ModelName,
		"tools_count":    len(session.Chatbot.Tools),
		"messages_count": len(messages),
	})

	// Convert tools to definitions
	toolDefinitions := s.ConvertToolsToDefinitions(session.Chatbot.Tools)

	// Create output channel
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
		detectedFormat := "none"

		// Keep-alive ticker to prevent timeouts
		keepAliveTicker := time.NewTicker(30 * time.Second)
		defer keepAliveTicker.Stop()

		// Maximum iterations to prevent infinite loops
		const maxIterations = 10
		iteration := 0

		// Track tool call counts
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

			log.Info("🔄 Tool call iteration", iteration, "of", maxIterations, "for session:", session.ID)

			// Generate chat response
			log.Info("Calling GenerateChatWithTools for session:", session.ID, "with", len(currentMessages), "messages")

			// DEBUG: Log the last few messages being sent to model
			if len(currentMessages) > 0 {
				log.Info("DEBUG - Last 3 messages sent to model:")
				startIdx := len(currentMessages) - 3
				if startIdx < 0 {
					startIdx = 0
				}
				for i := startIdx; i < len(currentMessages); i++ {
					msg := currentMessages[i]
					preview := msg.Content
					if len(preview) > 200 {
						preview = preview[:200] + "..."
					}
					log.Info(fmt.Sprintf("  Msg[%d] Role:%s Content:%s", i, msg.Role, preview))
				}
			}

			chatStream, err := provider.GenerateChatWithTools(context.Background(), currentMessages, toolDefinitions, options)
			if err != nil {
				log.Error("Failed to generate response:", err)
				// User-friendly error messages
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
				s.UpdateAssistantMessage(firstAssistant, errorMsg)
				return
			}

			log.Info("✅ GenerateChatWithTools call successful, processing stream...")

			hasToolCalls := false
			var pendingToolCalls []providers.ToolCall
			var streamContent strings.Builder
			var bufferMode bool = false

			// Process the stream
			responseCount := 0
			for response := range chatStream {
				responseCount++

				// Handle thinking tokens - both from provider and inline in content
				if response.Thinking != "" {
					if !thinkingStarted {
						outputCh <- "<think>"
						thinkingStarted = true
					}
					outputCh <- response.Thinking
					assistantContent.WriteString("<think>" + response.Thinking)
				}

				// Handle content
				if response.Content != "" {
					// Check for inline <think> tags in content (Gemma-style)
					if strings.Contains(response.Content, "<think>") {
						thinkContent, regularContent := extractThinkingFromContent(response.Content)

						if thinkContent != "" {
							if !thinkingStarted {
								outputCh <- "<think>"
								thinkingStarted = true
							}
							outputCh <- thinkContent
							assistantContent.WriteString("<think>" + thinkContent)
						}

						if strings.Contains(response.Content, "</think>") && thinkingStarted {
							outputCh <- "</think>"
							thinkingStarted = false
							assistantContent.WriteString("</think>")
						}

						if regularContent != "" {
							streamContent.WriteString(regularContent)
							assistantContent.WriteString(regularContent)

							// Early detection: Check if this chunk contains tool call tags
							if !bufferMode && (strings.Contains(regularContent, "<tool_call>") || strings.Contains(regularContent, "<tool_calls>")) {
								bufferMode = true
								log.Info("⚡ Early detected tool call tag in stream, entering buffer mode")
							}

							if !bufferMode {
								outputCh <- regularContent
							}
						}
					} else {
						// Normal content without thinking tags
						streamContent.WriteString(response.Content)
						assistantContent.WriteString(response.Content)

						// Early detection
						if !bufferMode && (strings.Contains(response.Content, "<tool_call>") || strings.Contains(response.Content, "<tool_calls>")) {
							bufferMode = true
							log.Info("⚡ Early detected tool call tag in stream, entering buffer mode")
						}

						if !bufferMode {
							outputCh <- response.Content
						}
					}
				}

				// Collect tool calls from provider (OpenAI format)
				if len(response.ToolCalls) > 0 {
					log.Info("📞 Provider returned tool calls (OpenAI format):", len(response.ToolCalls), "calls")
					hasToolCalls = true
					pendingToolCalls = append(pendingToolCalls, response.ToolCalls...)
					if detectedFormat == "none" {
						detectedFormat = "openai"
						log.Info("🔍 Auto-detected format: OpenAI (tool calls from provider)")
					}
				}

				if response.Done {
					log.Info("✅ Response marked as done after", responseCount, "responses")
					if thinkingStarted {
						outputCh <- "</think>"
						thinkingStarted = false
						assistantContent.WriteString("</think>")
					}
					break
				}
			}

			log.Info("📊 Stream processing finished. Total responses:", responseCount, "Buffer mode:", bufferMode)

			// AUTO-DETECT: Check content for tool calls if provider didn't return any
			if !hasToolCalls && streamContent.Len() > 0 {
				// DEBUG: Log content preview
				contentPreview := streamContent.String()
				if len(contentPreview) > 500 {
					contentPreview = contentPreview[:500] + "..."
				}
				log.Info("🔍 DEBUG - Model output preview (first 500 chars):", contentPreview)
				log.Info("📏 DEBUG - Full content length:", streamContent.Len(), "bytes")

				contentToolCalls, format, cleanedContent := s.detectAndParseToolCallsFromContent(streamContent.String())
				if len(contentToolCalls) > 0 {
					log.Info("🎯 Auto-detected", len(contentToolCalls), "tool calls from content. Format:", format)
					hasToolCalls = true
					pendingToolCalls = append(pendingToolCalls, contentToolCalls...)
					detectedFormat = format

					// Send cleaned content to frontend
					if bufferMode && len(cleanedContent) > 0 {
						log.Info("✨ Sending cleaned content to frontend (removed tool tags)")
						outputCh <- cleanedContent
					}

					// Update assistantContent with cleaned version
					originalLen := assistantContent.Len()
					streamLen := streamContent.Len()
					if streamLen > 0 && originalLen >= streamLen {
						prefix := assistantContent.String()[:originalLen-streamLen]
						assistantContent.Reset()
						assistantContent.WriteString(prefix)
						assistantContent.WriteString(cleanedContent)
					}
				} else if bufferMode {
					log.Info("📤 Buffer mode was active but no tool calls found, sending buffered content")
					outputCh <- streamContent.String()
				}
			}

			log.Info("📊 Tool calls status: HasToolCalls:", hasToolCalls, "DetectedFormat:", detectedFormat, "PendingCount:", len(pendingToolCalls))

			// Check for thinking-only response
			if !hasToolCalls && streamContent.Len() == 0 && assistantContent.Len() > 0 {
				log.Warn("⚠️  Model only produced thinking content, no actual response!")
				log.Warn("   This might indicate:")
				log.Warn("   1. Temperature too low")
				log.Warn("   2. System prompt needs adjustment")
				log.Warn("   3. Model stopping too early")

				fallbackMsg := "Özür dilerim, yanıt oluştururken bir sorun yaşadım. Lütfen sorunuzu tekrar sorar mısınız?"
				outputCh <- fallbackMsg
				assistantContent.WriteString(fallbackMsg)
			}

			// If no tool calls were made, we're done
			if !hasToolCalls {
				log.Info("✅ No tool calls detected, finishing response")
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			// Close thinking if open before processing tools
			if thinkingStarted {
				outputCh <- "</think>"
				thinkingStarted = false
				assistantContent.WriteString("</think>")
			}

			// Add the assistant's response to the message history
			assistantMessage := providers.ChatMessage{
				Role:    "assistant",
				Content: cleanAssistantContent(assistantContent.String()),
			}
			currentMessages = append(currentMessages, assistantMessage)

			// Process tool calls
			log.Info("🔧 Processing", len(pendingToolCalls), "tool calls...")
			var shouldStop bool
			var stopReason string
			currentMessages, shouldStop, stopReason = s.processToolCallsGemma(session, pendingToolCalls, currentMessages, outputCh, &assistantContent, toolCallCounter, detectedFormat)

			if shouldStop {
				log.Info("🛑 Stopping tool execution loop. Reason:", stopReason)
				s.UpdateAssistantMessage(firstAssistant, assistantContent.String())
				return
			}

			log.Info("🔄 Tool calls processed, continuing to next iteration...")
		}
	}()

	return outputCh, firstAssistant, nil
}
