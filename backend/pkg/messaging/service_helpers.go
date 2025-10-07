package messaging

import (
	"regexp"
	"strings"
)

// isGemmaModel checks if model is Gemma variant
func isGemmaModel(modelName string) bool {
	if modelName == "" {
		return true
	}
	modelLower := strings.ToLower(modelName)

	// GPT-OSS check
	if strings.Contains(modelLower, "gpt-oss") {
		return false
	}
	if strings.Contains(modelLower, "gpt-") && !strings.Contains(modelLower, "gemma") {
		return false
	}
	if strings.Contains(modelLower, "openai") {
		return false
	}

	// Gemma check
	if strings.Contains(modelLower, "gemma") {
		return true
	}

	// Your custom models
	if strings.Contains(modelLower, "kubernetes-ai") || strings.Contains(modelLower, "kube-ai") {
		return true
	}

	return true
}

// cleanAssistantContent removes internal tags
func cleanAssistantContent(content string) string {
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_executing>.*?</tool_executing>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_executed>.*?</tool_executed>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_calls>.*?</tool_calls>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_outputs>.*?</tool_outputs>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile(`(?s)<tool_output>.*?</tool_output>`)
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile("(?s)```tool_outputs.*?```")
	content = re.ReplaceAllString(content, "")

	re = regexp.MustCompile("(?s)```json.*?```")
	content = re.ReplaceAllString(content, "")

	content = strings.ReplaceAll(content, "<start_of_turn>user", "")
	content = strings.ReplaceAll(content, "<start_of_turn>model", "")
	content = strings.ReplaceAll(content, "<end_of_turn>", "")

	return strings.TrimSpace(content)
}
