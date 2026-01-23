package chatbots

import (
	"sef/app/entities"
	"sef/internal/paginator"
	"sef/internal/search"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Controller struct {
	DB *gorm.DB
}

func (h *Controller) Index(c fiber.Ctx) error {
	var items []*entities.Chatbot
	db := h.DB.Model(&entities.Chatbot{}).Preload(clause.Associations)

	if c.Query("search") != "" {
		search.Search(c.Query("search"), db)
	}

	page, err := paginator.New(db, c).Paginate(&items)
	if err != nil {
		return err
	}

	return c.JSON(page)
}

func (h *Controller) Show(c fiber.Ctx) error {
	var item *entities.Chatbot
	if err := h.DB.Preload(clause.Associations).First(&item, c.Params("id")).Error; err != nil {
		return err
	}

	return c.JSON(item)
}

func (h *Controller) Create(c fiber.Ctx) error {
	var payload struct {
		Name              string   `json:"name"`
		Description       string   `json:"description"`
		ProviderID        uint     `json:"provider_id"`
		SystemPrompt      string   `json:"system_prompt"`
		ModelName         string   `json:"model_name"`
		PromptSuggestions []string `json:"prompt_suggestions"`
		ToolIDs           []uint   `json:"tool_ids"`
		DocumentIDs       []uint   `json:"document_ids"`
		// Model Parameters
		Temperature *float64 `json:"temperature"`
		TopP        *float64 `json:"top_p"`
		TopK        *int     `json:"top_k"`
	}
	if err := c.Bind().JSON(&payload); err != nil {
		return err
	}

	// Create chatbot entity
	chatbot := &entities.Chatbot{
		Name:              payload.Name,
		Description:       payload.Description,
		ProviderID:        payload.ProviderID,
		SystemPrompt:      payload.SystemPrompt,
		ModelName:         payload.ModelName,
		PromptSuggestions: payload.PromptSuggestions,
		// Model Parameters
		Temperature: payload.Temperature,
		TopP:        payload.TopP,
		TopK:        payload.TopK,
	}

	if err := h.DB.Create(chatbot).Error; err != nil {
		return err
	}

	// Associate tools if provided
	if len(payload.ToolIDs) > 0 {
		var tools []entities.Tool
		for _, id := range payload.ToolIDs {
			tools = append(tools, entities.Tool{Base: entities.Base{ID: id}})
		}
		if err := h.DB.Model(chatbot).Association("Tools").Replace(tools); err != nil {
			return err
		}
	}

	// Associate documents if provided
	if len(payload.DocumentIDs) > 0 {
		var documents []entities.Document
		for _, id := range payload.DocumentIDs {
			documents = append(documents, entities.Document{Base: entities.Base{ID: id}})
		}
		if err := h.DB.Model(chatbot).Association("Documents").Replace(documents); err != nil {
			return err
		}
	}

	// Return the created chatbot with associations
	if err := h.DB.Preload("Tools").Preload("Provider").Preload("Documents").First(chatbot, chatbot.ID).Error; err != nil {
		return err
	}

	return c.JSON(chatbot)
}

func (h *Controller) Update(c fiber.Ctx) error {
	var payload map[string]interface{}
	if err := c.Bind().JSON(&payload); err != nil {
		return err
	}

	var chatbot *entities.Chatbot
	if err := h.DB.First(&chatbot, c.Params("id")).Error; err != nil {
		return err
	}

	// Extract tool IDs if provided
	var toolIDs []uint
	if toolIDsInterface, ok := payload["tool_ids"]; ok && toolIDsInterface != nil {
		if ids, ok := toolIDsInterface.([]interface{}); ok {
			for _, id := range ids {
				if idFloat, ok := id.(float64); ok {
					toolIDs = append(toolIDs, uint(idFloat))
				}
			}
		}
	}

	// Extract document IDs if provided
	var documentIDs []uint
	if docIDsInterface, ok := payload["document_ids"]; ok && docIDsInterface != nil {
		if ids, ok := docIDsInterface.([]interface{}); ok {
			for _, id := range ids {
				if idFloat, ok := id.(float64); ok {
					documentIDs = append(documentIDs, uint(idFloat))
				}
			}
		}
	}

	// Remove tool_ids and document_ids from payload before updating
	delete(payload, "tool_ids")
	delete(payload, "document_ids")

	if err := h.DB.
		Model(&entities.Chatbot{}).
		Where("id = ?", c.Params("id")).
		Updates(&payload).Error; err != nil {
		return err
	}

	// Associate tools if provided
	if len(toolIDs) > 0 {
		var tools []entities.Tool
		for _, id := range toolIDs {
			tools = append(tools, entities.Tool{Base: entities.Base{ID: id}})
		}
		if err := h.DB.Model(chatbot).Association("Tools").Replace(tools); err != nil {
			return err
		}
	}

	// Associate documents if provided
	if len(documentIDs) > 0 {
		var documents []entities.Document
		for _, id := range documentIDs {
			documents = append(documents, entities.Document{Base: entities.Base{ID: id}})
		}
		if err := h.DB.Model(chatbot).Association("Documents").Replace(documents); err != nil {
			return err
		}
	}

	// Return the updated chatbot with associations
	if err := h.DB.Preload("Tools").Preload("Provider").Preload("Documents").First(chatbot, c.Params("id")).Error; err != nil {
		return err
	}

	return c.JSON(chatbot)
}

func (h *Controller) Delete(c fiber.Ctx) error {
	if err := h.DB.Delete(&entities.Chatbot{}, c.Params("id")).Error; err != nil {
		return err
	}

	return c.JSON(fiber.Map{"message": "Chatbot deleted successfully"})
}
