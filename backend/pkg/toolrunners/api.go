package toolrunners

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3/log"
	"github.com/itchyny/gojq"
)

// APIToolRunner implements the ToolRunner interface for API calls
type APIToolRunner struct {
	config     map[string]interface{}
	parameters []interface{}
}

// NewAPIToolRunner creates a new API tool runner
func NewAPIToolRunner(config map[string]interface{}, parameters interface{}) *APIToolRunner {
	var params []interface{}
	if parameters != nil {
		if p, ok := parameters.([]interface{}); ok {
			params = p
		}
	}
	return &APIToolRunner{
		config:     config,
		parameters: params,
	}
}

// Execute runs the API tool with given parameters (backward compatibility)
func (r *APIToolRunner) Execute(ctx context.Context, parameters map[string]interface{}) (interface{}, error) {
	return r.ExecuteWithContext(ctx, parameters, nil)
}

// ExecuteWithContext runs the API tool with given parameters and additional context
func (r *APIToolRunner) ExecuteWithContext(ctx context.Context, parameters map[string]interface{}, toolContext *ToolCallContext) (interface{}, error) {
	// Extract configuration
	method, ok := r.config["method"].(string)
	if !ok {
		method = "GET"
	}

	urlTemplate, ok := r.config["url"].(string)
	if !ok {
		return nil, fmt.Errorf("url is required in tool configuration")
	}

	// Process URL parameters (replace {PARAM_NAME} placeholders)
	url := r.processURLParameters(urlTemplate, parameters)

	headers, _ := r.config["headers"].(map[string]interface{})
	timeout := 30 * time.Second
	if timeoutVal, ok := r.config["timeout"].(float64); ok {
		timeout = time.Duration(timeoutVal) * time.Second
	}

	insecureSkipVerify := false
	if val, ok := r.config["insecure_skip_verify"].(bool); ok {
		insecureSkipVerify = val
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify,
		},
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	var reqBody io.Reader
	if method == "POST" || method == "PUT" || method == "PATCH" {
		if bodyStr, ok := parameters["body"].(string); ok && bodyStr != "" {
			reqBody = strings.NewReader(bodyStr)
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		if strVal, ok := value.(string); ok {
			req.Header.Set(key, strVal)
		}
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON response
	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		// If not JSON, return as string
		result = string(body)
	}

	// Apply jq filtering if query is provided
	if jqQuery, ok := r.config["jq_query"].(string); ok && jqQuery != "" {
		filteredResult, err := r.ApplyJqFilter(result, jqQuery, parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to apply jq filter: %w", err)
		}
		result = filteredResult
	}

	// Build tool call details
	toolCallDetails := map[string]interface{}{
		"tool_type":   "api",
		"method":      method,
		"url":         url,
		"parameters":  parameters,
		"executed_at": time.Now().UTC().Format(time.RFC3339),
	}

	// Add tool context information if provided
	if toolContext != nil {
		if toolContext.ToolCallID != "" {
			toolCallDetails["tool_call_id"] = toolContext.ToolCallID
		}
		if toolContext.FunctionName != "" {
			toolCallDetails["function_name"] = toolContext.FunctionName
		}
		if toolContext.ToolName != "" {
			toolCallDetails["tool_name"] = toolContext.ToolName
		}
		if toolContext.Metadata != nil {
			toolCallDetails["metadata"] = toolContext.Metadata
		}
	}

	// Return detailed result with tool call information
	return map[string]interface{}{
		"tool_call_details": toolCallDetails,
		"status_code":       resp.StatusCode,
		"body":              result,
	}, nil
}

// processURLParameters replaces {PARAM_NAME} placeholders in URL with parameter values
func (r *APIToolRunner) processURLParameters(urlStr string, parameters map[string]interface{}) string {
	// Find all placeholders in the URL using regex
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(urlStr, -1)

	processedURL := urlStr
	for _, match := range matches {
		if len(match) == 2 {
			paramName := match[1]
			if paramValue, exists := parameters[paramName]; exists {
				// Convert parameter value to string
				var strValue string
				switch v := paramValue.(type) {
				case string:
					strValue = v
				case int, int32, int64:
					strValue = fmt.Sprintf("%d", v)
				case float64:
					strValue = fmt.Sprintf("%g", v)
				case bool:
					strValue = fmt.Sprintf("%t", v)
				default:
					// For complex types, convert to JSON string
					if jsonBytes, err := json.Marshal(v); err == nil {
						strValue = string(jsonBytes)
					} else {
						strValue = fmt.Sprintf("%v", v)
					}
				}
				// URL encode the parameter value
				processedURL = strings.ReplaceAll(processedURL, "{"+paramName+"}", url.QueryEscape(strValue))
			}
		}
	}

	return processedURL
}

// extractURLParameters extracts parameter names and their types from URL placeholders
func (r *APIToolRunner) extractURLParameters(url string) map[string]string {
	params := make(map[string]string)
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(url, -1)

	for _, match := range matches {
		if len(match) == 2 {
			paramName := match[1]
			// Default to string type for URL parameters
			params[paramName] = "string"
		}
	}

	return params
}

// ValidateParameters validates the input parameters against the tool's schema
func (r *APIToolRunner) ValidateParameters(parameters map[string]interface{}) error {
	if parameters == nil {
		return fmt.Errorf("parameters cannot be nil")
	}

	schema := r.GetParameterSchema()
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for paramName, paramDef := range properties {
			if paramDefMap, ok := paramDef.(map[string]interface{}); ok {
				if err := r.validateParameterType(paramName, parameters[paramName], paramDefMap); err != nil {
					return err
				}
			}
		}
	}

	// Check required parameters
	if required, ok := schema["required"].([]interface{}); ok {
		for _, reqParam := range required {
			if reqParamStr, ok := reqParam.(string); ok {
				if _, exists := parameters[reqParamStr]; !exists {
					return fmt.Errorf("parameter '%s' is required", reqParamStr)
				}
			}
		}
	}

	return nil
}

// validateParameterType validates a single parameter against its type definition
func (r *APIToolRunner) validateParameterType(paramName string, value interface{}, paramDef map[string]interface{}) error {
	paramType, ok := paramDef["type"].(string)
	if !ok {
		// If no type specified, skip validation
		return nil
	}

	if value == nil {
		// Check if parameter is required by looking at the schema's required array
		// This is handled in ValidateParameters, so we can skip here
		return nil
	}

	switch paramType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("parameter '%s' must be a string", paramName)
		}
	case "number":
		switch value.(type) {
		case float64, int, int32, int64:
			// Valid number types
		default:
			return fmt.Errorf("parameter '%s' must be a number", paramName)
		}
	case "integer":
		switch v := value.(type) {
		case int, int32, int64:
			// Valid integer types
		case float64:
			// Check if it's a whole number
			if v != float64(int64(v)) {
				return fmt.Errorf("parameter '%s' must be an integer", paramName)
			}
		default:
			return fmt.Errorf("parameter '%s' must be an integer", paramName)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("parameter '%s' must be a boolean", paramName)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("parameter '%s' must be an object", paramName)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("parameter '%s' must be an array", paramName)
		}
	default:
		// Unknown type, allow it
		return nil
	}

	return nil
}

// GetParameterSchema returns the JSON schema for tool parameters
func (r *APIToolRunner) GetParameterSchema() map[string]interface{} {
	properties := make(map[string]interface{})
	required := make([]interface{}, 0)

	// Add parameters from the parameters array
	for _, param := range r.parameters {
		if paramMap, ok := param.(map[string]interface{}); ok {
			name, nameOk := paramMap["name"].(string)
			paramType, typeOk := paramMap["type"].(string)
			description, descOk := paramMap["description"].(string)
			req, reqOk := paramMap["required"].(bool)

			if nameOk && typeOk {
				prop := map[string]interface{}{
					"type": paramType,
				}
				if descOk && description != "" {
					prop["description"] = description
				}
				properties[name] = prop

				if reqOk && req {
					required = append(required, name)
				}
			}
		}
	}

	// Add URL parameters extracted from URL placeholders
	if urlStr, ok := r.config["url"].(string); ok {
		urlParams := r.extractURLParameters(urlStr)
		for paramName, paramType := range urlParams {
			if _, exists := properties[paramName]; !exists {
				properties[paramName] = map[string]interface{}{
					"type":        paramType,
					"description": fmt.Sprintf("URL parameter %s", paramName),
				}
				required = append(required, paramName)
			}
		}
	}

	if len(properties) > 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": properties,
			"required":   required,
		}
	}

	// Fallback to default schema if no parameters defined
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Request body (JSON string for POST/PUT/PATCH)",
				"format":      "json",
			},
		},
	}
}

// GetConfigSchema returns the JSON schema for tool configuration
func (r *APIToolRunner) GetConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The API endpoint URL to call",
				"format":      "uri",
			},
			"method": map[string]interface{}{
				"type":        "string",
				"description": "HTTP method for the request",
				"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
				"default":     "GET",
			},
			"headers": map[string]interface{}{
				"type":        "object",
				"description": "HTTP headers as key-value pairs",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
			"timeout": map[string]interface{}{
				"type":        "integer",
				"description": "Request timeout in seconds",
				"minimum":     1,
				"maximum":     300,
				"default":     30,
			},
			"jq_query": map[string]interface{}{
				"type":        "string",
				"description": "Optional jq query to filter/transform the JSON response",
				"examples":    []string{".", ".data", ".[] | select(.status == \"active\")", "{results: .items, count: (.items | length)}"},
			},
		},
		"required": []string{"url"},
	}
}

// ApplyJqFilter applies a jq query to filter/transform JSON data
func (r *APIToolRunner) ApplyJqFilter(input interface{}, query string, params map[string]interface{}) (interface{}, error) {
	log.Info("Applying jq filter with query:", query, "and params:", params)

	// Replace parameter placeholders in the query
	processedQuery := query
	for key, value := range params {
		placeholder := "$" + key
		// Convert value to JSON string for replacement
		valueStr := fmt.Sprintf("%v", value)
		if str, ok := value.(string); ok {
			// If it's a string, wrap in quotes
			valueStr = fmt.Sprintf(`"%s"`, str)
		}
		processedQuery = strings.ReplaceAll(processedQuery, placeholder, valueStr)
	}

	// Compile the jq query
	compiledQuery, err := gojq.Parse(processedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse jq query: %w", err)
	}

	// Create iterator for the query
	iter := compiledQuery.Run(input)

	// Collect results
	var results []interface{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("jq query execution error: %w", err)
		}
		results = append(results, v)
	}

	// Return single result or array based on count
	if len(results) == 1 {
		return results[0], nil
	}
	return results, nil
}
