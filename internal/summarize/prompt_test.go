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

func TestDefaultPromptUsesInvestmentSummaryHeadings(t *testing.T) {
	for _, heading := range []string{
		"## 核心摘要",
		"## 容易被忽略但有价值的信息",
		"## 直观地可以 bullish / bearish on 什么",
		"## 隐含地可以 bullish / bearish on 什么",
		"## 可能利好/利空的股票",
	} {
		if !strings.Contains(DefaultPrompt, heading) {
			t.Fatalf("DefaultPrompt missing heading %q", heading)
		}
	}
	for _, oldHeading := range []string{"## 详细总结", "## Insights", "## 可能被忽略但有价值的点", "## 需要辩证看待的地方"} {
		if strings.Contains(DefaultPrompt, oldHeading) {
			t.Fatalf("DefaultPrompt still contains old heading %q", oldHeading)
		}
	}
}

func TestTraditionalPromptUsesTraditionalHeadings(t *testing.T) {
	prompt := VariantTraditional.Prompt()
	for _, heading := range []string{
		"## 核心摘要",
		"## 容易被忽略但有價值的資訊",
		"## 直觀地可以 bullish / bearish on 什麼",
		"## 隱含地可以 bullish / bearish on 什麼",
		"## 可能利好/利空的股票",
	} {
		if !strings.Contains(prompt, heading) {
			t.Fatalf("traditional prompt missing heading %q", heading)
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
