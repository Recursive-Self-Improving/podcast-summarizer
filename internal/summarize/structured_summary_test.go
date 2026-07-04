package summarize

import (
	"strings"
	"testing"
)

func validStructuredSummary(variant SummaryVariant) StructuredSummary {
	return StructuredSummary{
		Language:               variant.Code,
		CoreSummary:            "嘉宾 A 谈论了半导体周期。",
		OverlookedInformation:  "嘉宾提到一句库存数据。",
		ExplicitBullishBearish: "明确看好设备厂。",
		ImplicitBullishBearish: "暗含看好上游材料。",
		Stocks:                 noStocksSentence(variant),
	}
}

func TestStructuredSummarySchemaRejectsAdditionalProperties(t *testing.T) {
	schema := structuredSummarySchema(VariantSimplified)
	if got, ok := schema["additionalProperties"]; !ok || got != false {
		t.Fatalf("additionalProperties = %#v, want false", got)
	}
}

func TestStructuredSummarySchemaRequiresAllContentFields(t *testing.T) {
	schema := structuredSummarySchema(VariantSimplified)
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required = %#v, want []string", schema["required"])
	}
	want := []string{
		"language",
		"core_summary",
		"overlooked_information",
		"explicit_bullish_bearish",
		"implicit_bullish_bearish",
		"stocks",
	}
	if len(required) != len(want) {
		t.Fatalf("required = %#v, want %#v", required, want)
	}
	for i, name := range want {
		if required[i] != name {
			t.Fatalf("required[%d] = %q, want %q", i, required[i], name)
		}
	}
}

func TestStructuredSummarySchemaLanguageEnumMatchesVariant(t *testing.T) {
	for _, variant := range []SummaryVariant{VariantSimplified, VariantTraditional} {
		schema := structuredSummarySchema(variant)
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("properties = %#v", schema["properties"])
		}
		lang, ok := props["language"].(map[string]any)
		if !ok {
			t.Fatalf("language property = %#v", props["language"])
		}
		enum, ok := lang["enum"].([]string)
		if !ok || len(enum) != 1 || enum[0] != variant.Code {
			t.Fatalf("language enum for %s = %#v, want [%q]", variant.Code, enum, variant.Code)
		}
	}
}

func TestStructuredSummarySchemaStringPropertiesUseStrictSupportedKeywords(t *testing.T) {
	schema := structuredSummarySchema(VariantSimplified)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	for _, name := range []string{"core_summary", "overlooked_information", "explicit_bullish_bearish", "implicit_bullish_bearish", "stocks"} {
		property, ok := props[name].(map[string]any)
		if !ok {
			t.Fatalf("property %s = %#v", name, props[name])
		}
		if _, ok := property["minLength"]; ok {
			t.Fatalf("property %s includes unsupported strict schema keyword minLength: %#v", name, property)
		}
	}
}

func TestParseStructuredSummaryAcceptsFencedJSON(t *testing.T) {
	body := `{"language":"zh-hans","core_summary":"c","overlooked_information":"o","explicit_bullish_bearish":"e","implicit_bullish_bearish":"i","stocks":"s"}`
	for _, fence := range []string{
		"```json\n" + body + "\n```",
		"```JSON\n" + body + "\n```",
		"```\n" + body + "\n```",
		"  ```json\n" + body + "\n```  ",
		body,
	} {
		t.Run(fence[:6], func(t *testing.T) {
			got, err := parseStructuredSummary(fence)
			if err != nil {
				t.Fatalf("parseStructuredSummary returned error: %v", err)
			}
			if got.CoreSummary != "c" || got.Stocks != "s" {
				t.Fatalf("parsed summary = %#v", got)
			}
		})
	}
}

func TestParseStructuredSummaryRejectsUnknownFields(t *testing.T) {
	body := `{"language":"zh-hans","core_summary":"c","overlooked_information":"o","explicit_bullish_bearish":"e","implicit_bullish_bearish":"i","stocks":"s","extra":"boom"}`
	if _, err := parseStructuredSummary(body); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestParseStructuredSummaryRejectsTrailingData(t *testing.T) {
	body := `{"language":"zh-hans","core_summary":"c","overlooked_information":"o","explicit_bullish_bearish":"e","implicit_bullish_bearish":"i","stocks":"s"}{}`
	if _, err := parseStructuredSummary(body); err == nil {
		t.Fatal("expected trailing data to be rejected")
	}
}

func TestParseStructuredSummaryRejectsMalformedJSON(t *testing.T) {
	if _, err := parseStructuredSummary("{not json"); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}

func TestValidateStructuredSummaryRejectsWrongLanguage(t *testing.T) {
	summary := validStructuredSummary(VariantSimplified)
	summary.Language = "zh-hant"
	if err := validateStructuredSummary(summary, VariantSimplified); err == nil {
		t.Fatal("expected wrong language to be rejected")
	}
}

func TestValidateStructuredSummaryRejectsWhitespaceOnlyFields(t *testing.T) {
	fields := map[string]func(s StructuredSummary) StructuredSummary{
		"core_summary":             func(s StructuredSummary) StructuredSummary { s.CoreSummary = "   \n\t"; return s },
		"overlooked_information":   func(s StructuredSummary) StructuredSummary { s.OverlookedInformation = "  "; return s },
		"explicit_bullish_bearish": func(s StructuredSummary) StructuredSummary { s.ExplicitBullishBearish = "\n"; return s },
		"implicit_bullish_bearish": func(s StructuredSummary) StructuredSummary { s.ImplicitBullishBearish = "\t"; return s },
		"stocks":                   func(s StructuredSummary) StructuredSummary { s.Stocks = " "; return s },
	}
	for name, mutate := range fields {
		t.Run(name, func(t *testing.T) {
			summary := mutate(validStructuredSummary(VariantSimplified))
			err := validateStructuredSummary(summary, VariantSimplified)
			if err == nil {
				t.Fatalf("expected whitespace-only %s to be rejected", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("error = %v, want field name %q", err, name)
			}
		})
	}
}

func TestValidateStructuredSummaryAcceptsValidSummary(t *testing.T) {
	for _, variant := range []SummaryVariant{VariantSimplified, VariantTraditional} {
		if err := validateStructuredSummary(validStructuredSummary(variant), variant); err != nil {
			t.Fatalf("validate for %s returned error: %v", variant.Code, err)
		}
	}
}

func TestRenderStructuredSummaryMarkdownSimplifiedHeadings(t *testing.T) {
	markdown, err := renderStructuredSummaryMarkdown(validStructuredSummary(VariantSimplified), VariantSimplified)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	for _, heading := range []string{
		"## 核心摘要",
		"## 容易被忽略但有价值的信息",
		"## 直观地可以 bullish / bearish on 什么",
		"## 隐含地可以 bullish / bearish on 什么",
		"## 可能利好/利空的股票",
	} {
		if !strings.Contains(markdown, heading) {
			t.Fatalf("rendered markdown missing heading %q\n%s", heading, markdown)
		}
	}
}

func TestRenderStructuredSummaryMarkdownTraditionalHeadings(t *testing.T) {
	markdown, err := renderStructuredSummaryMarkdown(validStructuredSummary(VariantTraditional), VariantTraditional)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	for _, heading := range []string{
		"## 核心摘要",
		"## 容易被忽略但有價值的資訊",
		"## 直觀地可以 bullish / bearish on 什麼",
		"## 隱含地可以 bullish / bearish on 什麼",
		"## 可能利好/利空的股票",
	} {
		if !strings.Contains(markdown, heading) {
			t.Fatalf("rendered markdown missing heading %q\n%s", heading, markdown)
		}
	}
}

func TestRenderStructuredSummaryMarkdownRejectsInvalid(t *testing.T) {
	summary := validStructuredSummary(VariantSimplified)
	summary.Language = "zh-hant"
	if _, err := renderStructuredSummaryMarkdown(summary, VariantSimplified); err == nil {
		t.Fatal("expected render to reject wrong language")
	}
}

func TestRenderStructuredSummaryMarkdownTrimsBodyWhitespace(t *testing.T) {
	summary := validStructuredSummary(VariantSimplified)
	summary.CoreSummary = "  padded core  "
	markdown, err := renderStructuredSummaryMarkdown(summary, VariantSimplified)
	if err != nil {
		t.Fatalf("render returned error: %v", err)
	}
	if strings.Contains(markdown, "## 核心摘要\n\n  padded core") {
		t.Fatalf("rendered markdown did not trim body whitespace\n%s", markdown)
	}
	if !strings.Contains(markdown, "## 核心摘要\n\npadded core") {
		t.Fatalf("rendered markdown missing trimmed body\n%s", markdown)
	}
}
