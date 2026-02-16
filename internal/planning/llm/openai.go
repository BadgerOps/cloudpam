package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAIProvider implements the Provider interface using the OpenAI-compatible
// chat completions API. Works with OpenAI, Ollama, vLLM, Azure, and any
// endpoint that speaks the same protocol.
type OpenAIProvider struct {
	cfg    Config
	client *http.Client
}

// NewOpenAIProvider creates a provider for an OpenAI-compatible API.
func NewOpenAIProvider(cfg Config) *OpenAIProvider {
	return &OpenAIProvider{
		cfg:    cfg,
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string     { return "openai" }
func (p *OpenAIProvider) Available() bool   { return p.cfg.APIKey != "" }

func (p *OpenAIProvider) baseURL() string {
	if p.cfg.Endpoint != "" {
		return strings.TrimRight(p.cfg.Endpoint, "/")
	}
	return "https://api.openai.com/v1"
}

// openaiRequest is the request body for the chat completions API.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int64           `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse is the response body for non-streaming completions.
type openaiResponse struct {
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// openaiStreamChunk is a single SSE chunk from the streaming API.
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message, opts Options) (*Response, error) {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	temp := opts.Temperature
	if temp == 0 {
		temp = p.cfg.Temperature
	}

	oaiMsgs := make([]openaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = openaiMessage(m)
	}

	body := openaiRequest{
		Model:       p.cfg.Model,
		Messages:    oaiMsgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      false,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("api error: %s", oaiResp.Error.Message)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &Response{
		Content:      oaiResp.Choices[0].Message.Content,
		FinishReason: oaiResp.Choices[0].FinishReason,
		PromptTokens: oaiResp.Usage.PromptTokens,
		OutputTokens: oaiResp.Usage.CompletionTokens,
	}, nil
}

func (p *OpenAIProvider) StreamComplete(ctx context.Context, messages []Message, opts Options) (<-chan StreamEvent, error) {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	temp := opts.Temperature
	if temp == 0 {
		temp = p.cfg.Temperature
	}

	oaiMsgs := make([]openaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = openaiMessage(m)
	}

	body := openaiRequest{
		Model:       p.cfg.Model,
		Messages:    oaiMsgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      true,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()
		p.readSSEStream(resp.Body, ch)
	}()

	return ch, nil
}

func (p *OpenAIProvider) readSSEStream(r io.Reader, ch chan<- StreamEvent) {
	buf := make([]byte, 4096)
	var lineBuf strings.Builder

	for {
		n, err := r.Read(buf)
		if n > 0 {
			lineBuf.Write(buf[:n])
			for {
				text := lineBuf.String()
				idx := strings.Index(text, "\n")
				if idx == -1 {
					break
				}
				line := text[:idx]
				lineBuf.Reset()
				lineBuf.WriteString(text[idx+1:])

				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					ch <- StreamEvent{Done: true}
					return
				}

				var chunk openaiStreamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue
				}
				if len(chunk.Choices) == 0 {
					continue
				}

				evt := StreamEvent{
					Delta: chunk.Choices[0].Delta.Content,
				}
				if chunk.Choices[0].FinishReason != nil {
					evt.FinishReason = *chunk.Choices[0].FinishReason
					evt.Done = true
				}
				ch <- evt
			}
		}
		if err != nil {
			ch <- StreamEvent{Done: true}
			return
		}
	}
}
