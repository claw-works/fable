// Package llm 封装 LLM API 调用，支持 OpenAI 和 Bedrock Converse。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/claw-works/fable/internal/schema"
)

// Message 表示聊天消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client 是 LLM API 客户端，根据 provider 分发请求。
type Client struct {
	cfg  schema.LLMConfig
	http *http.Client
}

// New 创建一个新的 LLM 客户端。
func New(cfg schema.LLMConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// ChatJSON 发送聊天请求并要求返回 JSON 格式。
func (c *Client) ChatJSON(ctx context.Context, messages []Message) (string, error) {
	var raw string
	var err error
	if c.cfg.Provider == "bedrock" {
		raw, err = c.bedrockChat(ctx, messages, true)
	} else {
		raw, err = c.openaiChat(ctx, messages, &responseFormat{Type: "json_object"})
	}
	if err != nil {
		return "", err
	}
	return stripMarkdownJSON(raw), nil
}

// Chat 发送普通聊天请求。
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c.cfg.Provider == "bedrock" {
		return c.bedrockChat(ctx, messages, false)
	}
	return c.openaiChat(ctx, messages, nil)
}

// ── OpenAI ──

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) openaiChat(ctx context.Context, messages []Message, rf *responseFormat) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: c.cfg.Model, Messages: messages, ResponseFormat: rf,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := c.cfg.BaseURL + "/chat/completions"
	log.Printf("[llm:openai] → POST %s model=%s msgs=%d", url, c.cfg.Model, len(messages))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
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
		log.Printf("[llm:openai] ✗ HTTP %d: %s", resp.StatusCode, truncate(respBody, 200))
		return "", fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("empty choices in response")
	}
	content := cr.Choices[0].Message.Content
	log.Printf("[llm:openai] ✓ %d chars", len(content))
	return content, nil
}

// ── Bedrock Converse ──

// Bedrock Converse API 请求/响应结构

type bedrockContentBlock struct {
	Text string `json:"text"`
}

type bedrockMessage struct {
	Role    string                `json:"role"`
	Content []bedrockContentBlock `json:"content"`
}

type bedrockRequest struct {
	Messages        []bedrockMessage       `json:"messages"`
	System          []bedrockContentBlock  `json:"system,omitempty"`
	InferenceConfig *bedrockInferenceConfig `json:"inferenceConfig,omitempty"`
}

type bedrockInferenceConfig struct {
	MaxTokens int `json:"maxTokens"`
}

type bedrockResponse struct {
	Output struct {
		Message struct {
			Role    string                `json:"role"`
			Content []bedrockContentBlock `json:"content"`
		} `json:"message"`
	} `json:"output"`
}

func (c *Client) bedrockChat(ctx context.Context, messages []Message, wantJSON bool) (string, error) {
	// 分离 system 消息和对话消息
	var system []bedrockContentBlock
	var convMsgs []bedrockMessage

	for _, m := range messages {
		if m.Role == "system" {
			text := m.Content
			if wantJSON {
				text += "\n\n请务必以纯 JSON 格式回复，不要包含任何其他文本。"
			}
			system = append(system, bedrockContentBlock{Text: text})
		} else {
			convMsgs = append(convMsgs, bedrockMessage{
				Role:    m.Role,
				Content: []bedrockContentBlock{{Text: m.Content}},
			})
		}
	}

	reqBody := bedrockRequest{
		Messages:        convMsgs,
		System:          system,
		InferenceConfig: &bedrockInferenceConfig{MaxTokens: 1024},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/model/%s/converse", strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.Model)
	log.Printf("[llm:bedrock] → POST %s model=%s msgs=%d", url, c.cfg.Model, len(convMsgs))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
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
		log.Printf("[llm:bedrock] ✗ HTTP %d: %s", resp.StatusCode, truncate(respBody, 200))
		return "", fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var br bedrockResponse
	if err := json.Unmarshal(respBody, &br); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if len(br.Output.Message.Content) == 0 {
		return "", fmt.Errorf("empty content in bedrock response")
	}

	content := br.Output.Message.Content[0].Text
	log.Printf("[llm:bedrock] ✓ %d chars", len(content))
	return content, nil
}

// stripMarkdownJSON 去掉 LLM 返回中的 ```json ... ``` 包裹，并提取第一个 JSON 对象。
func stripMarkdownJSON(s string) string {
	s = strings.TrimSpace(s)
	// 去掉 markdown 代码块
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	// 提取第一个 {...} 块，正确跳过字符串内的花括号
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	inStr := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inStr {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
