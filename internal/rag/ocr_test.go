package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"mewcode/internal/config"
)

func newTestOcrClient(t *testing.T, baseURL string) *OcrClient {
	t.Helper()
	client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(baseURL))
	return &OcrClient{client: &client, model: "test-vision"}
}

func TestOcrExtractSendsBase64Image(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"| 季度 | 营收 |\n|---|---|\n| Q3 | 3200�?|"}}]}`)
	}))
	defer srv.Close()

	ocr := newTestOcrClient(t, srv.URL)
	text, err := ocr.Extract(context.Background(), []byte("fakejpegbytes"), "image/jpeg")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(text, "Q3") || !strings.Contains(text, "|") {
		t.Fatalf("Extract returned %q, want markdown table", text)
	}

	model, _ := gotBody["model"].(string)
	if model != "test-vision" {
		t.Fatalf("model = %v, want test-vision", model)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1", len(msgs))
	}
	msg, _ := msgs[0].(map[string]any)
	parts, _ := msg["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content parts = %d, want 2 (text + image)", len(parts))
	}
	found := false
	for _, p := range parts {
		pm, _ := p.(map[string]any)
		img, _ := pm["image_url"].(map[string]any)
		if img != nil {
			url, _ := img["url"].(string)
			if strings.HasPrefix(url, "data:image/jpeg;base64,") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no image_url with data:image/jpeg;base64 prefix in request")
	}
}

func TestOcrNoTextSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"[NO_TEXT]"}}]}`)
	}))
	defer srv.Close()

	ocr := newTestOcrClient(t, srv.URL)
	text, err := ocr.Extract(context.Background(), []byte("img"), "image/png")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if text != "" {
		t.Fatalf("Extract = %q, want empty for [NO_TEXT]", text)
	}
}

func TestOcrRetryOnServerError(t *testing.T) {
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	ocr := newTestOcrClient(t, srv.URL)
	text, err := ocr.Extract(context.Background(), []byte("img"), "image/png")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if text != "ok" {
		t.Fatalf("Extract = %q, want ok", text)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 failures + 1 success)", calls)
	}
	if ocr.failCount != 0 {
		t.Fatalf("failCount = %d, want 0 after eventual success", ocr.failCount)
	}
}

func TestOcrFailureCountedAndReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ocr := newTestOcrClient(t, srv.URL)
	_, err := ocr.Extract(context.Background(), []byte("img"), "image/png")
	if err == nil {
		t.Fatalf("want error after all retries fail")
	}
	if ocr.failCount != 1 {
		t.Fatalf("failCount = %d, want 1", ocr.failCount)
	}
	ocr.RecordSkip("a.pdf: filter unsupported")
	report := ocr.Report()
	if !strings.Contains(report, "张失败") {
		t.Fatalf("Report = %q", report)
	}
	if !strings.Contains(report, "a.pdf: filter unsupported") {
		t.Fatalf("Report missing skip reason: %q", report)
	}
}

func TestNewOcrClientConfig(t *testing.T) {
	if res := NewOcrClient(&config.ProviderConfig{}); res.Client != nil || res.Err != nil {
		t.Fatalf("empty config should yield nil client, nil err")
	}
	res := NewOcrClient(&config.ProviderConfig{
		BaseURL:     "http://localhost:1",
		APIKey:      "k",
		VisionModel: "doubao-seed-2.0-lite",
	})
	if res.Err != nil || res.Client == nil {
		t.Fatalf("valid config: err=%v client=%v", res.Err, res.Client)
	}
	if res.Client.Model() != "doubao-seed-2.0-lite" {
		t.Fatalf("Model = %s", res.Client.Model())
	}
	if !res.Client.Available() {
		t.Fatalf("Available should be true")
	}
	bad := NewOcrClient(&config.ProviderConfig{VisionModel: "m"})
	if bad.Client != nil || bad.Err == nil {
		t.Fatalf("missing api key should yield error")
	}
	var nilClient *OcrClient
	if nilClient.Available() {
		t.Fatalf("nil client should not be Available")
	}
	if nilClient.Report() != "" {
		t.Fatalf("nil client Report should be empty")
	}
}
