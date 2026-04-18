// Package llm 封装 OpenAI 兼容 API 的调用逻辑。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/claw-works/fable/internal/schema"
)

// Client 是 LLM API 客户端。
type Client struct {
	cfg    schema.LLMConfig
	http   *http.Client
}

// New 创建一个新的 LLM 客户端。
func New(cfg schema.LLMConfig) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
	}
}

// Message 表示聊天消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest 是发送给 API 的请求体。
type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []Message         `json:"messages"`
	ResponseFormat *responseFormat    `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// chatResponse 是 API 返回的响应体。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// ChatJSON 发送聊天请求并要求返回 JSON 格式。
func (c *Client) ChatJSON(ctx context.Context, messages []Message) (string, error) {
	return c.chat(ctx, messages, &responseFormat{Type: "json_object"})
}

// Chat 发送普通聊天请求。
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	return c.chat(ctx, messages, nil)
}

func (c *Client) chat(ctx context.Context, messages []Message, rf *responseFormat) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:          c.cfg.Model,
		Messages:       messages,
		ResponseFormat: rf,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty choices in response")
	}
	return cr.Choices[0].Message.Content, nil
}
