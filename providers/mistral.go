// Package providers implements LLM provider interfaces and implementations.
package providers

import (
	"encoding/json"
	"fmt"
	"github.com/teilomillet/gollm/config"
	"github.com/teilomillet/gollm/utils"
)

// MistralProvider implements the Provider interface for Mistral AI's API.
// It supports Mistral's language models and provides access to their capabilities,
// including chat completion and structured output.
type MistralProvider struct {
	apiKey       string           // API key for authentication
	model        string           // Model identifier (e.g., "mistral-large", "mistral-medium")
	extraHeaders map[string]string // Additional HTTP headers
	options      map[string]interface{} // Model-specific options
	logger       utils.Logger     // Logger instance
}

// NewMistralProvider creates a new Mistral provider instance.
// It initializes the provider with the given API key, model, and optional headers.
//
// Parameters:
//   - apiKey: Mistral API key for authentication
//   - model: The model to use (e.g., "mistral-large", "mistral-medium")
//   - extraHeaders: Additional HTTP headers for requests
//
// Returns:
//   - A configured Mistral Provider instance
func NewMistralProvider(apiKey, model string, extraHeaders map[string]string) Provider {
	if extraHeaders == nil {
		extraHeaders = make(map[string]string)
	}
	return &MistralProvider{
		apiKey:       apiKey,
		model:        model,
		extraHeaders: extraHeaders,
		options:      make(map[string]interface{}),
		logger:       utils.NewLogger(utils.LogLevelInfo),
	}
}

// SetLogger configures the logger for the Mistral provider.
// This is used for debugging and monitoring API interactions.
func (p *MistralProvider) SetLogger(logger utils.Logger) {
	p.logger = logger
}

// SetOption sets a specific option for the Mistral provider.
// Supported options include:
//   - temperature: Controls randomness (0.0 to 1.0)
//   - max_tokens: Maximum tokens in the response
//   - top_p: Nucleus sampling parameter
//   - random_seed: Random seed for deterministic sampling
func (p *MistralProvider) SetOption(key string, value interface{}) {
	p.options[key] = value
}

// SetDefaultOptions configures standard options from the global configuration.
// This includes temperature, max tokens, and sampling parameters.
func (p *MistralProvider) SetDefaultOptions(config *config.Config) {
	p.SetOption("temperature", config.Temperature)
	p.SetOption("max_tokens", config.MaxTokens)
	if config.Seed != nil {
		p.SetOption("seed", *config.Seed)
	}
}

// Name returns "mistral" as the provider identifier.
func (p *MistralProvider) Name() string {
	return "mistral"
}

// Endpoint returns the Mistral API endpoint URL.
// This is "https://api.mistral.ai/v1/chat/completions".
func (p *MistralProvider) Endpoint() string {
	return "https://api.mistral.ai/v1/chat/completions"
}

// SupportsJSONSchema indicates that Mistral supports structured output
// through its system prompts and response formatting capabilities.
func (p *MistralProvider) SupportsJSONSchema() bool {
	return true
}

// Headers returns the required HTTP headers for Mistral API requests.
// This includes:
//   - Authorization: Bearer token using the API key
//   - Content-Type: application/json
//   - Any additional headers specified via SetExtraHeaders
func (p *MistralProvider) Headers() map[string]string {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.apiKey,
	}

	for key, value := range p.extraHeaders {
		headers[key] = value
	}

	return headers
}

// PrepareRequest creates the request body for a Mistral API call.
// It handles:
//   - Message formatting
//   - System prompts
//   - Response formatting
//   - Model-specific options
//
// Parameters:
//   - prompt: The input text or conversation
//   - options: Additional parameters for the request
//
// Returns:
//   - Serialized JSON request body
//   - Any error encountered during preparation
func (p *MistralProvider) PrepareRequest(prompt string, options map[string]interface{}) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
	}

	// First, add the default options
	for k, v := range p.options {
		requestBody[k] = v
	}

	// Then, add any additional options (which may override defaults)
	for k, v := range options {
		requestBody[k] = v
	}

	return json.Marshal(requestBody)
}

// PrepareRequestWithSchema creates a request that includes structured output formatting.
// This uses Mistral's system prompts to enforce response structure.
//
// Parameters:
//   - prompt: The input text or conversation
//   - options: Additional request parameters
//   - schema: JSON schema for response validation
//
// Returns:
//   - Serialized JSON request body
//   - Any error encountered during preparation
func (p *MistralProvider) PrepareRequestWithSchema(prompt string, options map[string]interface{}, schema interface{}) ([]byte, error) {
	requestBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"response_format": map[string]interface{}{
			"type":   "json_schema",
			"schema": schema,
		},
	}

	// Add any additional options
	for k, v := range options {
		requestBody[k] = v
	}

	// Add strict option if provided
	if strict, ok := options["strict"].(bool); ok && strict {
		requestBody["response_format"].(map[string]interface{})["strict"] = true
	}

	return json.Marshal(requestBody)
}

// ParseResponse extracts the generated text from the Mistral API response.
// It handles various response formats and error cases.
//
// Parameters:
//   - body: Raw API response body
//
// Returns:
//   - Generated text content
//   - Any error encountered during parsing
func (p *MistralProvider) ParseResponse(body []byte) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("error parsing response: %w", err)
	}

	if len(response.Choices) == 0 || response.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from API")
	}

	// Combine content and tool calls
	var finalResponse string
	finalResponse += response.Choices[0].Message.Content

	for _, toolCall := range response.Choices[0].Message.ToolCalls {
		finalResponse += fmt.Sprintf("<function_call>{\"name\": \"%s\", \"arguments\": %s}</function_call>", toolCall.Function.Name, toolCall.Function.Arguments)
	}

	return finalResponse, nil
}

// HandleFunctionCalls processes structured output in the response.
// This supports Mistral's response formatting capabilities.
func (p *MistralProvider) HandleFunctionCalls(body []byte) ([]byte, error) {
	var response struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("error parsing response: %w", err)
	}

	if len(response.Choices) == 0 || len(response.Choices[0].Message.ToolCalls) == 0 {
		return nil, nil // No function call
	}

	return json.Marshal(response.Choices[0].Message.ToolCalls[0].Function)
}

// SetExtraHeaders configures additional HTTP headers for API requests.
// This allows for custom headers needed for specific features or requirements.
func (p *MistralProvider) SetExtraHeaders(extraHeaders map[string]string) {
	p.extraHeaders = extraHeaders
}
