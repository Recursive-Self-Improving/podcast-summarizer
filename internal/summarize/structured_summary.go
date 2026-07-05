package summarize

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
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
	summary = normalizeStructuredSummary(summary, variant)
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

func normalizeStructuredSummary(summary StructuredSummary, variant SummaryVariant) StructuredSummary {
	recovered, ok := recoverStructuredSummarySections(summary, variant)
	if !ok {
		return summary
	}
	return recovered
}

func recoverStructuredSummarySections(summary StructuredSummary, variant SummaryVariant) (StructuredSummary, bool) {
	values := []string{
		summary.CoreSummary,
		summary.OverlookedInformation,
		summary.ExplicitBullishBearish,
		summary.ImplicitBullishBearish,
		summary.Stocks,
	}
	for _, value := range values {
		sections, ok := parseStructuredSummaryMarkdownSections(value, variant)
		if ok {
			return structuredSummaryFromSections(summary.Language, sections), true
		}
	}

	var b strings.Builder
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(value)
	}

	sections, ok := parseStructuredSummaryMarkdownSections(b.String(), variant)
	if !ok {
		return StructuredSummary{}, false
	}
	return structuredSummaryFromSections(summary.Language, sections), true
}

func structuredSummaryFromSections(language string, sections [structuredSummarySectionCount]string) StructuredSummary {
	return StructuredSummary{
		Language:               language,
		CoreSummary:            sections[structuredSummarySectionCore],
		OverlookedInformation:  sections[structuredSummarySectionOverlooked],
		ExplicitBullishBearish: sections[structuredSummarySectionExplicit],
		ImplicitBullishBearish: sections[structuredSummarySectionImplicit],
		Stocks:                 sections[structuredSummarySectionStocks],
	}
}

type structuredSummarySectionIndex int

const (
	structuredSummarySectionCore structuredSummarySectionIndex = iota
	structuredSummarySectionOverlooked
	structuredSummarySectionExplicit
	structuredSummarySectionImplicit
	structuredSummarySectionStocks
	structuredSummarySectionCount
)

func parseStructuredSummaryMarkdownSections(text string, variant SummaryVariant) ([structuredSummarySectionCount]string, bool) {
	var sections [structuredSummarySectionCount][]string
	var seen [structuredSummarySectionCount]bool
	current := structuredSummarySectionIndex(-1)
	fenceLen := 0

	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if fenceLen == 0 {
			if section, ok := structuredSummaryHeadingSection(line, variant); ok {
				if seen[section] {
					return [structuredSummarySectionCount]string{}, false
				}
				seen[section] = true
				current = section
				continue
			}
			if markerLen, ok := structuredOpeningFenceLine(line); ok {
				fenceLen = markerLen
			}
		} else if structuredClosingFenceLine(line, fenceLen) {
			fenceLen = 0
		}
		if current >= 0 {
			sections[current] = append(sections[current], line)
		}
	}

	var result [structuredSummarySectionCount]string
	for section := structuredSummarySectionIndex(0); section < structuredSummarySectionCount; section++ {
		body := strings.TrimSpace(strings.Join(sections[section], "\n"))
		if !seen[section] || body == "" {
			return [structuredSummarySectionCount]string{}, false
		}
		result[section] = body
	}
	return result, true
}

func structuredSummaryHeadingSection(line string, variant SummaryVariant) (structuredSummarySectionIndex, bool) {
	candidate := normalizeStructuredSummaryHeading(line)
	if candidate == "" {
		return 0, false
	}
	switch {
	case containsAnyText(candidate, "核心摘要"):
		return structuredSummarySectionCore, true
	case containsAnyText(candidate, sectionTitleOverlookedInformation(variant), "容易被忽略但有价值的信息", "有价值的信息", "容易被忽略但有价值的资讯", "有价值的资讯", "有价值资讯", "容易被忽略但有價值的資訊", "有價值的資訊"):
		return structuredSummarySectionOverlooked, true
	case containsAnyText(candidate, sectionTitleExplicitBullishBearish(variant), "直观地可以 bullish / bearish on 什么", "直觀地可以 bullish / bearish on 什麼", "直观", "直觀"):
		return structuredSummarySectionExplicit, true
	case containsAnyText(candidate, sectionTitleImplicitBullishBearish(variant), "隐含地可以 bullish / bearish on 什么", "隱含地可以 bullish / bearish on 什麼", "隐含", "隱含"):
		return structuredSummarySectionImplicit, true
	case containsAnyText(candidate, sectionTitleStocks(variant), "利好/利空", "利好利空") || containsAllText(candidate, "利好", "利空"):
		return structuredSummarySectionStocks, true
	default:
		return 0, false
	}
}

func normalizeStructuredSummaryHeading(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || structuredRuneLen(trimmed) > 80 {
		return ""
	}
	looksLikeHeading := strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") || structuredStartsWithNumbering(trimmed)
	if !looksLikeHeading {
		return ""
	}
	trimmed = strings.TrimLeft(trimmed, "# ")
	trimmed = strings.TrimSpace(strings.Trim(trimmed, "*"))
	trimmed = strings.TrimSpace(structuredStripNumbering(trimmed))
	trimmed = strings.TrimRight(trimmed, ":：")
	return strings.ToLower(strings.TrimSpace(trimmed))
}

func structuredStartsWithNumbering(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	r, size := structuredFirstRune(text)
	if unicode.IsDigit(r) {
		rest := strings.TrimSpace(text[size:])
		return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "、") || strings.HasPrefix(rest, ")") || strings.HasPrefix(rest, "）")
	}
	return strings.ContainsRune("一二三四五六七八九十", r) && strings.HasPrefix(strings.TrimSpace(text[size:]), "、")
}

func structuredStripNumbering(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	r, size := structuredFirstRune(text)
	if unicode.IsDigit(r) || strings.ContainsRune("一二三四五六七八九十", r) {
		rest := strings.TrimSpace(text[size:])
		separator, separatorSize := structuredFirstRune(rest)
		if separatorSize > 0 && strings.ContainsRune(".、)）", separator) {
			return strings.TrimSpace(rest[separatorSize:])
		}
	}
	return text
}

func structuredRuneLen(text string) int {
	count := 0
	for range text {
		count++
	}
	return count
}

func structuredFirstRune(text string) (rune, int) {
	for i, r := range text {
		return r, i + utf8.RuneLen(r)
	}
	return 0, 0
}

func structuredOpeningFenceLine(line string) (int, bool) {
	markerLen := structuredLeadingBackticks(strings.TrimSpace(line))
	return markerLen, markerLen >= 3
}

func structuredClosingFenceLine(line string, openingLen int) bool {
	trimmed := strings.TrimSpace(line)
	markerLen := structuredLeadingBackticks(trimmed)
	return markerLen >= openingLen && strings.TrimSpace(trimmed[markerLen:]) == ""
}

func structuredLeadingBackticks(text string) int {
	count := 0
	for count < len(text) && text[count] == '`' {
		count++
	}
	return count
}

func containsAnyText(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func containsAllText(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, strings.ToLower(needle)) {
			return false
		}
	}
	return true
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
