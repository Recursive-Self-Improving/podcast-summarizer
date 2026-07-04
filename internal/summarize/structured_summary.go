package summarize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const structuredSummarySchemaVersion = "structured-summary-v1"

type StructuredSummary struct {
	Language               string `json:"language"`
	CoreSummary            string `json:"core_summary"`
	OverlookedInformation  string `json:"overlooked_information"`
	ExplicitBullishBearish string `json:"explicit_bullish_bearish"`
	ImplicitBullishBearish string `json:"implicit_bullish_bearish"`
	Stocks                 string `json:"stocks"`
}

func structuredSummarySchema(variant SummaryVariant) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"language",
			"core_summary",
			"overlooked_information",
			"explicit_bullish_bearish",
			"implicit_bullish_bearish",
			"stocks",
		},
		"properties": map[string]any{
			"language": map[string]any{
				"type":        "string",
				"enum":        []string{variant.Code},
				"description": "Must exactly match the requested output language code.",
			},
			"core_summary": map[string]any{
				"type":        "string",
				"description": "核心摘要. Write this value in " + variant.Code + ". Include 5-20 key points and identify the guest near the beginning. Internal Markdown is allowed; do not include section headings.",
			},
			"overlooked_information": map[string]any{
				"type":        "string",
				"description": "容易被忽略但有价值的信息 / 容易被忽略但有價值的資訊. Write this value in " + variant.Code + ". Internal Markdown is allowed; do not include section headings.",
			},
			"explicit_bullish_bearish": map[string]any{
				"type":        "string",
				"description": "直观地可以 bullish / bearish on 什么 / 直觀地可以 bullish / bearish on 什麼. Write this value in " + variant.Code + ". Internal Markdown is allowed; do not include section headings.",
			},
			"implicit_bullish_bearish": map[string]any{
				"type":        "string",
				"description": "隐含地可以 bullish / bearish on 什么 / 隱含地可以 bullish / bearish on 什麼. Write this value in " + variant.Code + ". Internal Markdown is allowed; do not include section headings.",
			},
			"stocks": map[string]any{
				"type":        "string",
				"description": "可能利好/利空的股票. Write this value in " + variant.Code + ". If no directly related stock/company/ticker is discussed, set this value exactly to " + noStocksSentence(variant) + ". Internal Markdown is allowed; do not include section headings.",
			},
		},
	}
}

func parseStructuredSummary(text string) (StructuredSummary, error) {
	text = stripJSONFence(strings.TrimSpace(text))
	var summary StructuredSummary
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return StructuredSummary{}, fmt.Errorf("decode structured summary JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return StructuredSummary{}, fmt.Errorf("decode structured summary JSON: trailing data")
	}
	return summary, nil
}

func stripJSONFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	firstNewline := strings.IndexByte(trimmed, '\n')
	if firstNewline < 0 {
		return trimmed
	}
	opening := strings.TrimSpace(trimmed[:firstNewline])
	if opening != "```" && opening != "```json" && opening != "```JSON" {
		return trimmed
	}
	bodyAndClose := trimmed[firstNewline+1:]
	lastFence := strings.LastIndex(bodyAndClose, "```")
	if lastFence < 0 {
		return trimmed
	}
	if strings.TrimSpace(bodyAndClose[lastFence+len("```"):]) != "" {
		return trimmed
	}
	return strings.TrimSpace(bodyAndClose[:lastFence])
}

func validateStructuredSummary(summary StructuredSummary, variant SummaryVariant) error {
	if strings.TrimSpace(summary.Language) != variant.Code {
		return fmt.Errorf("structured summary language = %q, want %q", summary.Language, variant.Code)
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "core_summary", value: summary.CoreSummary},
		{name: "overlooked_information", value: summary.OverlookedInformation},
		{name: "explicit_bullish_bearish", value: summary.ExplicitBullishBearish},
		{name: "implicit_bullish_bearish", value: summary.ImplicitBullishBearish},
		{name: "stocks", value: summary.Stocks},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("structured summary %s is empty", field.name)
		}
	}
	return nil
}

func renderStructuredSummaryMarkdown(summary StructuredSummary, variant SummaryVariant) (string, error) {
	if err := validateStructuredSummary(summary, variant); err != nil {
		return "", err
	}
	sections := []struct {
		title string
		body  string
	}{
		{title: sectionTitleCoreSummary(variant), body: summary.CoreSummary},
		{title: sectionTitleOverlookedInformation(variant), body: summary.OverlookedInformation},
		{title: sectionTitleExplicitBullishBearish(variant), body: summary.ExplicitBullishBearish},
		{title: sectionTitleImplicitBullishBearish(variant), body: summary.ImplicitBullishBearish},
		{title: sectionTitleStocks(variant), body: summary.Stocks},
	}
	var b strings.Builder
	for i, section := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(section.title)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(section.body))
	}
	return b.String(), nil
}

func sectionTitleCoreSummary(_ SummaryVariant) string {
	return "核心摘要"
}

func sectionTitleOverlookedInformation(variant SummaryVariant) string {
	if variant.Code == VariantTraditional.Code {
		return "容易被忽略但有價值的資訊"
	}
	return "容易被忽略但有价值的信息"
}

func sectionTitleExplicitBullishBearish(variant SummaryVariant) string {
	if variant.Code == VariantTraditional.Code {
		return "直觀地可以 bullish / bearish on 什麼"
	}
	return "直观地可以 bullish / bearish on 什么"
}

func sectionTitleImplicitBullishBearish(variant SummaryVariant) string {
	if variant.Code == VariantTraditional.Code {
		return "隱含地可以 bullish / bearish on 什麼"
	}
	return "隐含地可以 bullish / bearish on 什么"
}

func sectionTitleStocks(_ SummaryVariant) string {
	return "可能利好/利空的股票"
}

func noStocksSentence(variant SummaryVariant) string {
	if variant.Code == VariantTraditional.Code {
		return "本期未提及具體標的"
	}
	return "本期未提及具体标的"
}
