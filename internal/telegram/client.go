// Package telegram is a minimal Telegram Bot API client for sendMessage.
// The bot token is never logged or included in errors.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	DefaultBaseURL        = "https://api.telegram.org"
	MaxMessageLength      = 4096
	DefaultRequestTimeout = 20 * time.Second
)

// APIError is a sanitized Telegram error.
type APIError struct {
	Code        int
	Description string
	RetryAfter  time.Duration
	Transient   bool
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("Telegram HTTP %d: %s (retry-after %s)", e.Code, e.Description, e.RetryAfter)
	}
	return fmt.Sprintf("Telegram HTTP %d: %s", e.Code, e.Description)
}

// RetryAfterValue lets the notification worker honor Telegram throttling.
func (e *APIError) RetryAfterValue() time.Duration { return e.RetryAfter }

// Client sends messages to one chat.
type Client struct {
	Token   string
	ChatID  string
	BaseURL string
	HTTP    *http.Client
	now     func() time.Time
}

// New validates and builds a client. It does not call the network.
func New(token, chatID, baseURL string, hc *http.Client) (*Client, error) {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN não configurado")
	}
	if !strings.Contains(token, ":") {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN com formato inválido")
	}
	if chatID == "" {
		return nil, fmt.Errorf("TELEGRAM_CHAT_ID não configurado")
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("TELEGRAM_API_BASE inválida")
	}
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	return &Client{Token: token, ChatID: chatID, BaseURL: strings.TrimRight(baseURL, "/"), HTTP: hc, now: time.Now}, nil
}

type sendRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type messageResult struct {
	MessageID int `json:"message_id"`
}

// SendMessage sends one already-split HTML message. It returns the Telegram
// message id on success.
func (c *Client) SendMessage(ctx context.Context, text string) (int, error) {
	if strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("mensagem vazia")
	}
	if utf16Len(text) > MaxMessageLength {
		return 0, fmt.Errorf("mensagem com %d unidades UTF-16 excede o limite de %d", utf16Len(text), MaxMessageLength)
	}
	payload, err := json.Marshal(sendRequest{
		ChatID:                c.ChatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return 0, err
	}
	endpoint := c.BaseURL + "/bot" + c.Token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("montar requisição Telegram: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "urna-watch/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("falha de rede ao enviar ao Telegram: %s", c.redact(err.Error()))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var env apiEnvelope
	_ = json.Unmarshal(body, &env)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !env.OK {
		code := env.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		desc := strings.TrimSpace(env.Description)
		if desc == "" {
			desc = strings.TrimSpace(string(body))
			if desc == "" {
				desc = http.StatusText(resp.StatusCode)
			}
		}
		ra := time.Duration(env.Parameters.RetryAfter) * time.Second
		if ra == 0 {
			if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
				ra = time.Duration(secs) * time.Second
			}
		}
		return 0, &APIError{
			Code:        code,
			Description: c.redact(desc),
			RetryAfter:  ra,
			Transient:   code == http.StatusTooManyRequests || code >= 500,
		}
	}
	var result messageResult
	_ = json.Unmarshal(env.Result, &result)
	return result.MessageID, nil
}

func (c *Client) redact(s string) string {
	if c.Token != "" {
		s = strings.ReplaceAll(s, c.Token, "[redacted]")
	}
	return s
}

// UTF16Length is the unit used by Telegram's 4096-character limit.
func UTF16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

func utf16Len(s string) int { return UTF16Length(s) }

// SplitMessage splits text into chunks that fit Telegram's UTF-16 limit,
// preferring line boundaries. It does not add extra prefixes, so callers can
// keep the content byte-for-byte unless a single line is too long.
func SplitMessage(text string, limit int) []string {
	if limit <= 0 || limit > MaxMessageLength {
		limit = MaxMessageLength
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if utf16Len(text) <= limit {
		return []string{text}
	}
	var chunks []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if utf16Len(line) > limit {
			flush()
			parts := splitLongLine(line, limit)
			chunks = append(chunks, parts...)
			continue
		}
		candidate := line
		if current.Len() > 0 {
			candidate = "\n" + line
		}
		if utf16Len(current.String())+utf16Len(candidate) > limit {
			flush()
			current.WriteString(line)
			continue
		}
		current.WriteString(candidate)
	}
	flush()
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func splitLongLine(line string, limit int) []string {
	var parts []string
	var b strings.Builder
	for _, r := range line {
		if utf16Len(b.String())+utf16Len(string(r)) > limit {
			if b.Len() > 0 {
				parts = append(parts, b.String())
				b.Reset()
			}
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}
