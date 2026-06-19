// Package llm provides a minimal, dependency-free HTTP client for
// OpenAI-compatible chat completion APIs (OpenAI, Anthropic via proxy,
// Azure OpenAI, local Ollama, etc.).
//
// It is intentionally thin: one function, one request shape, one response
// shape. The workflow engine needs exactly "send messages, get text back"
// — anything fancier belongs in the daemon's CLI agent layer.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Provider identifies which LLM service to call.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderOllama    Provider = "ollama"
)

// defaultEndpoints maps providers to their default API base URLs.
var defaultEndpoints = map[Provider]string{
	ProviderOpenAI:    "https://api.openai.com/v1",
	ProviderAnthropic: "https://api.anthropic.com/v1",
	ProviderOllama:    "http://localhost:11434/v1",
}

// Request is the input to a chat completion call.
type Request struct {
	Provider     Provider // which API shape to use
	Model        string   // e.g. "gpt-4o", "claude-sonnet-4-20250514"
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Temperature  float64
	APIKey       string // bearer token
	BaseURL      string // override default endpoint
}

// Response is the output of a chat completion call.
type Response struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// Complete sends a chat completion request and returns the assistant response.
// It speaks the OpenAI chat completions wire format for all providers except
// Anthropic (which gets its native messages API format).
func Complete(ctx context.Context, req Request) (Response, error) {
	if req.Provider == ProviderAnthropic {
		return completeAnthropic(ctx, req)
	}
	return completeOpenAI(ctx, req)
}

// ── OpenAI-compatible path ──

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func completeOpenAI(ctx context.Context, req Request) (Response, error) {
	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = defaultEndpoints[req.Provider]
	}
	if baseURL == "" {
		baseURL = defaultEndpoints[ProviderOpenAI]
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"

	messages := make([]openAIMessage, 0, 2)
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.UserPrompt})

	body := openAIRequest{
		Model:    req.Model,
		Messages: messages,
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}

	respBody, err := doPost(ctx, endpoint, req.APIKey, "", body)
	if err != nil {
		return Response{}, err
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return Response{}, fmt.Errorf("llm: parse openai response: %w", err)
	}
	if len(oaiResp.Choices) == 0 {
		return Response{}, fmt.Errorf("llm: openai returned 0 choices")
	}

	return Response{
		Content:      oaiResp.Choices[0].Message.Content,
		Model:        oaiResp.Model,
		InputTokens:  oaiResp.Usage.PromptTokens,
		OutputTokens: oaiResp.Usage.CompletionTokens,
	}, nil
}

// ── Anthropic native path ──

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int                `json:"max_tokens"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func completeAnthropic(ctx context.Context, req Request) (Response, error) {
	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = defaultEndpoints[ProviderAnthropic]
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/messages"

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	body := anthropicRequest{
		Model:     req.Model,
		System:    req.SystemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: req.UserPrompt}},
		MaxTokens: maxTokens,
	}

	respBody, err := doPost(ctx, endpoint, req.APIKey, "2023-06-01", body)
	if err != nil {
		return Response{}, err
	}

	var aResp anthropicResponse
	if err := json.Unmarshal(respBody, &aResp); err != nil {
		return Response{}, fmt.Errorf("llm: parse anthropic response: %w", err)
	}
	if len(aResp.Content) == 0 {
		return Response{}, fmt.Errorf("llm: anthropic returned 0 content blocks")
	}

	return Response{
		Content:      aResp.Content[0].Text,
		Model:        aResp.Model,
		InputTokens:  aResp.Usage.InputTokens,
		OutputTokens: aResp.Usage.OutputTokens,
	}, nil
}

// ── HTTP transport ──

var httpClient = &http.Client{Timeout: 120 * time.Second}

func doPost(ctx context.Context, url, apiKey, anthropicVersion string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if apiKey != "" {
		if anthropicVersion != "" {
			// Anthropic uses x-api-key header
			httpReq.Header.Set("x-api-key", apiKey)
			httpReq.Header.Set("anthropic-version", anthropicVersion)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("llm: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm: API returned %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	return respBody, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// DetectProvider infers the LLM provider from a model name string.
func DetectProvider(model string) Provider {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return ProviderAnthropic
	case strings.Contains(m, "llama"), strings.Contains(m, "mistral"), strings.Contains(m, "qwen"):
		return ProviderOllama
	default:
		return ProviderOpenAI
	}
}

// ExtractAPIKey reads the LLM API key from agent custom_env.
// Tries provider-specific keys first, then generic OPENAI_API_KEY.
func ExtractAPIKey(customEnv map[string]string, provider Provider) string {
	switch provider {
	case ProviderAnthropic:
		if k := customEnv["ANTHROPIC_API_KEY"]; k != "" {
			return k
		}
	case ProviderOpenAI:
		if k := customEnv["OPENAI_API_KEY"]; k != "" {
			return k
		}
	}
	// Fallback: try generic keys
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "LLM_API_KEY"} {
		if k := customEnv[key]; k != "" {
			return k
		}
	}
	return ""
}

// ExtractBaseURL reads a custom API base URL from agent custom_env.
func ExtractBaseURL(customEnv map[string]string, provider Provider) string {
	switch provider {
	case ProviderAnthropic:
		return customEnv["ANTHROPIC_BASE_URL"]
	case ProviderOpenAI:
		return customEnv["OPENAI_BASE_URL"]
	case ProviderOllama:
		if u := customEnv["OLLAMA_BASE_URL"]; u != "" {
			return u
		}
		return customEnv["OLLAMA_HOST"]
	}
	return ""
}
