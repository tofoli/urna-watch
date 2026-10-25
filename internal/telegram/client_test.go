package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendMessageSuccess(t *testing.T) {
	var got sendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot123:secret/sendMessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	}))
	defer srv.Close()
	c, err := New("123:secret", "-100123", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.SendMessage(context.Background(), "<b>oi</b>")
	if err != nil {
		t.Fatal(err)
	}
	if id != 42 {
		t.Fatalf("message id = %d", id)
	}
	if got.ChatID != "-100123" || got.Text != "<b>oi</b>" || got.ParseMode != "HTML" {
		t.Fatalf("request = %+v", got)
	}
}

func TestSendMessageRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":3}}`))
	}))
	defer srv.Close()
	c, _ := New("123:secret", "-100123", srv.URL, nil)
	_, err := c.SendMessage(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.Code != 429 || apiErr.RetryAfter != 3*time.Second || !apiErr.Transient {
		t.Fatalf("error = %+v", apiErr)
	}
}

func TestSendMessagePermanentErrorRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"bad token 123:secret"}`))
	}))
	defer srv.Close()
	c, _ := New("123:secret", "-100123", srv.URL, nil)
	_, err := c.SendMessage(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "123:secret") {
		t.Fatalf("token leaked: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Transient {
		t.Fatalf("error = %+v", apiErr)
	}
}

func TestSendMessageServerErrorIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":502,"description":"upstream"}`))
	}))
	defer srv.Close()
	c, _ := New("123:secret", "-100123", srv.URL, nil)
	_, err := c.SendMessage(context.Background(), "x")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.Transient {
		t.Fatalf("error = %+v", apiErr)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New("", "-1", "", nil); err == nil {
		t.Fatal("empty token")
	}
	if _, err := New("bad", "-1", "", nil); err == nil {
		t.Fatal("bad token")
	}
	if _, err := New("1:a", "", "", nil); err == nil {
		t.Fatal("empty chat")
	}
}

func TestSplitMessageUTF16AndEmoji(t *testing.T) {
	text := strings.Repeat("😀 linha com emoji e acentuação\n", 200)
	chunks := SplitMessage(text, 120)
	if len(chunks) < 2 {
		t.Fatal("expected chunks")
	}
	for i, chunk := range chunks {
		if UTF16Length(chunk) > 120 {
			t.Fatalf("chunk %d has %d units", i, UTF16Length(chunk))
		}
	}
	if UTF16Length(SplitMessage("curto", 120)[0]) != 5 {
		t.Fatal("short message changed")
	}
}

func TestSplitMessageLongLine(t *testing.T) {
	text := strings.Repeat("a", 250)
	chunks := SplitMessage(text, 100)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	if strings.Join(chunks, "") != text {
		t.Fatal("content changed")
	}
}
