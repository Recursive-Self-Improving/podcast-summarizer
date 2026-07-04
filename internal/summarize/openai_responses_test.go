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

func TestOpenAIResponsesBuiltInPromptSendsStrictJSONSchemaAndReturnsCanonicalMarkdown(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_struct",
			"object":"response",
			"created_at":0,
			"model":"test-model",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"language\":\"zh-hans\",\"core_summary\":\"嘉宾谈论半导体。\",\"overlooked_information\":\"一句库存数据。\",\"explicit_bullish_bearish\":\"看好设备。\",\"implicit_bullish_bearish\":\"暗含看好材料。\",\"stocks\":\"本期未提及具体标的\"}","annotations":[]}]}]
		}`))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	markdown, err := summarizer.Summarize(context.Background(), VariantSimplified.Prompt(), "transcript")
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if !strings.Contains(markdown, "## 核心摘要") || !strings.Contains(markdown, "嘉宾谈论半导体") {
		t.Fatalf("expected canonical Markdown with rendered sections, got %q", markdown)
	}
	if !strings.Contains(markdown, "## 可能利好/利空的股票") || !strings.Contains(markdown, "本期未提及具体标的") {
		t.Fatalf("expected stocks section rendered from JSON, got %q", markdown)
	}

	text, ok := gotBody["text"].(map[string]any)
	if !ok {
		t.Fatalf("request missing text.format, body = %#v", gotBody)
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("request missing text.format, text = %#v", text)
	}
	if format["type"] != "json_schema" {
		t.Fatalf("text.format.type = %#v, want json_schema", format["type"])
	}
	if format["name"] != "podcast_investment_summary" {
		t.Fatalf("text.format.name = %#v, want podcast_investment_summary", format["name"])
	}
	if format["strict"] != true {
		t.Fatalf("text.format.strict = %#v, want true", format["strict"])
	}
	schema, ok := format["schema"].(map[string]any)
	if !ok {
		t.Fatalf("text.format.schema = %#v, want map", format["schema"])
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("schema.additionalProperties = %#v, want false", schema["additionalProperties"])
	}
}

func TestOpenAIResponsesBuiltInTraditionalPromptReturnsTraditionalMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_struct",
			"object":"response",
			"created_at":0,
			"model":"test-model",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"{\"language\":\"zh-hant\",\"core_summary\":\"嘉賓談論半導體。\",\"overlooked_information\":\"一句庫存數據。\",\"explicit_bullish_bearish\":\"看好設備。\",\"implicit_bullish_bearish\":\"暗含看好材料。\",\"stocks\":\"本期未提及具體標的\"}","annotations":[]}]}]
		}`))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	markdown, err := summarizer.Summarize(context.Background(), VariantTraditional.Prompt(), "transcript")
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if !strings.Contains(markdown, "## 容易被忽略但有價值的資訊") {
		t.Fatalf("expected traditional heading, got %q", markdown)
	}
	if strings.Contains(markdown, "容易被忽略但有价值的信息") {
		t.Fatalf("traditional markdown leaked simplified heading, got %q", markdown)
	}
}

func TestOpenAIResponsesBuiltInPromptAcceptsFencedJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"resp_struct\",\"object\":\"response\",\"created_at\":0,\"model\":\"test-model\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"```json\\n{\\\"language\\\":\\\"zh-hans\\\",\\\"core_summary\\\":\\\"c\\\",\\\"overlooked_information\\\":\\\"o\\\",\\\"explicit_bullish_bearish\\\":\\\"e\\\",\\\"implicit_bullish_bearish\\\":\\\"i\\\",\\\"stocks\\\":\\\"本期未提及具体标的\\\"}\\n```\",\"annotations\":[]}]}]}"))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	markdown, err := summarizer.Summarize(context.Background(), VariantSimplified.Prompt(), "transcript")
	if err != nil {
		t.Fatalf("Summarize returned error for fenced JSON: %v", err)
	}
	if !strings.Contains(markdown, "## 核心摘要") {
		t.Fatalf("expected fenced JSON parsed into canonical markdown, got %q", markdown)
	}
}

func TestOpenAIResponsesCustomPromptSendsNoTextFormatAndReturnsRawSummary(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_legacy",
			"object":"response",
			"created_at":0,
			"model":"test-model",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"raw final summary with ## heading","annotations":[]}]}]
		}`))
	}))
	defer server.Close()

	summarizer := NewOpenAIResponses(server.URL, "test-key", "test-model")
	text, err := summarizer.Summarize(context.Background(), "custom prompt body", "transcript")
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if text != "raw final summary with ## heading" {
		t.Fatalf("legacy text = %q, want raw final summary", text)
	}
	if _, ok := gotBody["text"]; ok {
		t.Fatalf("custom prompt request must not set text field, body = %#v", gotBody)
	}
	input, ok := gotBody["input"].(string)
	if !ok || !strings.Contains(input, "Prompt:\ncustom prompt body") {
		t.Fatalf("legacy input = %#v", gotBody["input"])
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
