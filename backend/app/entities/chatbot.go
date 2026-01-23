package entities

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

type StringArray []string

func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = []string{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(bytes, a)
}

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	return json.Marshal(a)
}

type Chatbot struct {
	Base
	Name              string      `json:"name" gorm:"not null;size:255"`
	Description       string      `json:"description" gorm:"type:text"`
	ProviderID        uint        `json:"provider_id" gorm:"not null"`
	Provider          Provider    `json:"provider,omitempty" gorm:"foreignKey:ProviderID"`
	SystemPrompt      string      `json:"system_prompt" gorm:"type:text"`
	ModelName         string      `json:"model_name" gorm:"not null"`
	WebSearchEnabled  bool        `json:"web_search_enabled" gorm:"default:false"`
	ToolFormat        string      `json:"tool_format" gorm:"default:'json';size:10"`
	OutputFormat      string      `json:"output_format" gorm:"default:'json';size:10"`
	PromptSuggestions StringArray `json:"prompt_suggestions" gorm:"type:json"`
	// Model Parameters
	Temperature *float64 `json:"temperature" gorm:"type:decimal(3,2)"`
	TopP        *float64 `json:"top_p" gorm:"type:decimal(3,2)"`
	TopK        *int     `json:"top_k"`
	// Relationships
	Sessions  []Session  `json:"sessions,omitempty" gorm:"foreignKey:ChatbotID"`
	Tools     []Tool     `json:"tools,omitempty" gorm:"many2many:chatbot_tools;"`
	Documents []Document `json:"documents,omitempty" gorm:"many2many:chatbot_documents;"`
}

// GetPromptSuggestions returns the prompt suggestions, or default ones if none are set
func (c *Chatbot) GetPromptSuggestions() []string {
	if len(c.PromptSuggestions) > 0 {
		return c.PromptSuggestions
	}

	// Default suggestions
	return []string{
		"What can you help me with?",
		"Explain this concept to me",
		"How do I get started?",
	}
}
