package summarize

import (
	"strings"
	"testing"
)

func TestPromptHashStable(t *testing.T) {
	first := PromptHash("prompt")
	second := PromptHash("prompt")
	if first != second {
		t.Fatalf("hashes differ: %s %s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("hash length = %d", len(first))
	}
}

func TestPromptHashDiffers(t *testing.T) {
	if PromptHash("prompt one") == PromptHash("prompt two") {
		t.Fatal("different prompts produced the same hash")
	}
}

func TestDefaultPromptRequestsStructuredJSONInSimplifiedChinese(t *testing.T) {
	if !strings.Contains(DefaultPrompt, "Summary format: structured JSON") {
		t.Fatal("DefaultPrompt missing structured JSON format directive")
	}
	if !strings.Contains(DefaultPrompt, "Schema version: "+structuredSummarySchemaVersion) {
		t.Fatal("DefaultPrompt missing schema version")
	}
	if !strings.Contains(DefaultPrompt, `Language: zh-hans`) {
		t.Fatal("DefaultPrompt missing zh-hans language directive")
	}
	if !strings.Contains(DefaultPrompt, `必须精确填写 "zh-hans"`) {
		t.Fatal("DefaultPrompt missing zh-hans language requirement")
	}
	if !strings.Contains(DefaultPrompt, `必须精确填写“`+noStocksSentence(VariantSimplified)+`”`) {
		t.Fatal("DefaultPrompt missing no-stocks fallback sentence")
	}
	for _, oldHeading := range []string{"## 详细总结", "## Insights", "## 可能被忽略但有价值的点", "## 需要辩证看待的地方", "## 核心摘要", "## 可能利好/利空的股票"} {
		if strings.Contains(DefaultPrompt, oldHeading) {
			t.Fatalf("DefaultPrompt still contains Markdown heading %q", oldHeading)
		}
	}
}

func TestTraditionalPromptRequestsStructuredJSONInTraditionalChinese(t *testing.T) {
	prompt := VariantTraditional.Prompt()
	if !strings.Contains(prompt, "Summary format: structured JSON") {
		t.Fatal("traditional prompt missing structured JSON format directive")
	}
	if !strings.Contains(prompt, "Schema version: "+structuredSummarySchemaVersion) {
		t.Fatal("traditional prompt missing schema version")
	}
	if !strings.Contains(prompt, `Language: zh-hant`) {
		t.Fatal("traditional prompt missing zh-hant language directive")
	}
	if !strings.Contains(prompt, `必須精確填寫 "zh-hant"`) {
		t.Fatal("traditional prompt missing zh-hant language requirement")
	}
	if !strings.Contains(prompt, `必須精確填寫「`+noStocksSentence(VariantTraditional)+`」`) {
		t.Fatal("traditional prompt missing no-stocks fallback sentence")
	}
	for _, oldHeading := range []string{"## 詳細總結", "## Insights", "## 可能被忽略但有價值的點", "## 需要辯證看待的地方", "## 核心摘要", "## 可能利好/利空的股票"} {
		if strings.Contains(prompt, oldHeading) {
			t.Fatalf("traditional prompt still contains Markdown heading %q", oldHeading)
		}
	}
}

func TestSummaryVariantsExposeOnlyChineseLanguageButtons(t *testing.T) {
	variants := SummaryVariants()
	if len(variants) != 2 {
		t.Fatalf("variants = %#v", variants)
	}
	if variants[0] != VariantSimplified || variants[1] != VariantTraditional {
		t.Fatalf("variants = %#v", variants)
	}
	if variants[0].Label != "简中" || variants[1].Label != "繁中" {
		t.Fatalf("variant labels = %q, %q", variants[0].Label, variants[1].Label)
	}
	if variants[0].Code != "zh-hans" || variants[1].Code != "zh-hant" {
		t.Fatalf("variant codes = %q, %q", variants[0].Code, variants[1].Code)
	}
}

func TestSummaryVariantByCodeAcceptsCanonicalCodesAndAliases(t *testing.T) {
	tests := []struct {
		code string
		want SummaryVariant
		ok   bool
	}{
		{code: "zh-hans", want: VariantSimplified, ok: true},
		{code: "zh-hant", want: VariantTraditional, ok: true},
		{code: "js", want: VariantSimplified, ok: true},
		{code: "fs", want: VariantTraditional, ok: true},
		{code: "jl"},
		{code: "fl"},
		{code: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got, ok := SummaryVariantByCode(tt.code)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("SummaryVariantByCode(%q) = %#v, %v; want %#v, %v", tt.code, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSummaryVariantByPromptHashMapsActivePrompts(t *testing.T) {
	for _, variant := range []SummaryVariant{VariantSimplified, VariantTraditional} {
		got, ok := SummaryVariantByPromptHash(PromptHash(variant.Prompt()))
		if !ok || got != variant {
			t.Fatalf("SummaryVariantByPromptHash(%s) = %#v, %v; want %#v, true", variant.Code, got, ok, variant)
		}
	}
}

func TestResolvePromptUsesDefaultForEmptyPrompt(t *testing.T) {
	for _, prompt := range []string{"", " ", "\n\t"} {
		if got := ResolvePrompt(prompt); got != DefaultPrompt {
			t.Fatalf("ResolvePrompt(%q) = %q", prompt, got)
		}
	}
}

func TestResolvePromptPreservesCustomPrompt(t *testing.T) {
	prompt := "  custom prompt  "
	if got := ResolvePrompt(prompt); got != prompt {
		t.Fatalf("ResolvePrompt preserved = %q", got)
	}
}
