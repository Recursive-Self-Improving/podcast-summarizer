package summarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIResponsesBuildsRequestAndParsesText(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_123",
			"object":"response",
			"created_at":0,
			"model":"test-model",
			"output":[{"id":"msg_123","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"final summary","annotations":[]}]}]
		}`))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	text, err := summarizer.Summarize(context.Background(), "prompt", "transcript")
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if text != "final summary" {
		t.Fatalf("text = %q", text)
	}
	if gotPath != "/responses" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("body = %#v", gotBody)
	}
	input, ok := gotBody["input"].(string)
	if !ok || !containsAll(input, "Prompt:\nprompt", "Transcript:\ntranscript") {
		t.Fatalf("input = %#v", gotBody["input"])
	}
}

func TestOpenAIResponsesRejectsEmptyOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_123","object":"response","created_at":0,"model":"test-model","output":[]}`))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	if _, err := summarizer.Summarize(context.Background(), "prompt", "transcript"); err == nil {
		t.Fatal("expected empty output error")
	}
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
