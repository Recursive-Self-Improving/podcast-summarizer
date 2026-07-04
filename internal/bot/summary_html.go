package bot

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"unicode"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

type summarySection struct {
	Title string
	Body  string
}

type summaryPart struct {
	Body     string
	FenceLen int
}

var expectedSummarySectionTitles = []string{"核心摘要", "容易被忽略但有价值的信息", "直观地可以 bullish / bearish on 什么", "隐含地可以 bullish / bearish on 什么", "可能利好/利空的股票"}
var expectedTraditionalSummarySectionTitles = []string{"核心摘要", "容易被忽略但有價值的資訊", "直觀地可以 bullish / bearish on 什麼", "隱含地可以 bullish / bearish on 什麼", "可能利好/利空的股票"}

const (
	simplifiedPlaceholderSummary  = "（消息占位符，为切换到长版占位。）"
	traditionalPlaceholderSummary = "（消息佔位符，為切換到長版佔位。）"
)

var errSummaryTooManyMessages = errors.New("summary renders to more than two telegram messages")

type summaryRenderOptions struct {
	linkLabel string
}

var defaultSummaryRenderOptions = summaryRenderOptions{linkLabel: "链接"}
var broadcastSummaryRenderOptions = summaryRenderOptions{linkLabel: "来源"}

func (o summaryRenderOptions) metadataLinkLabel() string {
	if strings.TrimSpace(o.linkLabel) == "" {
		return defaultSummaryRenderOptions.linkLabel
	}
	return strings.TrimSpace(o.linkLabel)
}

func renderFinalSummaryHTMLMessages(summary string, limit int) []string {
	messages, _, _ := renderFinalSummaryHTMLMessagesRaw(summary, limit, display.SummaryMetadata{})
	return messages
}

func renderFinalSummaryHTMLParts(summary string, limit int) ([]string, error) {
	return renderFinalSummaryHTMLPartsWithMetadata(summary, display.SummaryMetadata{}, limit)
}

func renderFinalSummaryHTMLPartsWithMetadata(summary string, metadata display.SummaryMetadata, limit int) ([]string, error) {
	return renderFinalSummaryHTMLPartsWithMetadataOptions(summary, metadata, limit, defaultSummaryRenderOptions)
}

func renderFinalSummaryHTMLPartsWithMetadataOptions(summary string, metadata display.SummaryMetadata, limit int, opts summaryRenderOptions) ([]string, error) {
	messages, traditional, err := renderFinalSummaryHTMLMessagesRawOptions(summary, limit, metadata, opts)
	if err != nil {
		return nil, err
	}
	if len(messages) == 1 {
		placeholder := simplifiedPlaceholderSummary
		if traditional {
			placeholder = traditionalPlaceholderSummary
		}
		return []string{messages[0], renderExpandableSectionHTML("Summary", placeholder)}, nil
	}
	if len(messages) > 2 {
		if !metadata.Empty() {
			return renderFinalSummaryHTMLPartsWithMetadataOptions(summary, display.SummaryMetadata{}, limit, opts)
		}
		return nil, errSummaryTooManyMessages
	}
	return messages, nil
}

func renderFinalSummaryHTMLMessagesRaw(summary string, limit int, metadata display.SummaryMetadata) ([]string, bool, error) {
	return renderFinalSummaryHTMLMessagesRawOptions(summary, limit, metadata, defaultSummaryRenderOptions)
}

func renderFinalSummaryHTMLMessagesRawOptions(summary string, limit int, metadata display.SummaryMetadata, opts summaryRenderOptions) ([]string, bool, error) {
	if limit <= 0 {
		limit = maxTelegramTextChars
	}
	sections, traditional, ok := detectSummarySections(summary)
	if !ok {
		sections = []summarySection{{Title: "Summary", Body: strings.TrimSpace(summary)}}
	}
	if len(sections) == 0 || strings.TrimSpace(sections[0].Body) == "" {
		return nil, traditional, nil
	}

	reservedLimit := limit - runeLen("<b>Summary 999/999</b>\n\n")
	if reservedLimit < 1 {
		reservedLimit = limit
	}

	fragments := renderSummaryFragments(sections, reservedLimit)
	if header := renderSummaryMetadataHeaderHTMLOptions(metadata, opts); header != "" {
		fragments = append([]string{header}, fragments...)
	}
	messages := packHTMLFragments(fragments, limit)
	if len(messages) == 0 {
		return nil, traditional, nil
	}
	if len(messages) <= 1 {
		return messages, traditional, nil
	}

	for {
		prefixReserve := runeLen(fmt.Sprintf("<b>Summary %d/%d</b>\n\n", len(messages), len(messages)))
		next := packHTMLFragments(fragments, limit-prefixReserve)
		if len(next) == len(messages) {
			messages = next
			break
		}
		messages = next
	}
	for i := range messages {
		messages[i] = fmt.Sprintf("<b>Summary %d/%d</b>\n\n%s", i+1, len(messages), messages[i])
	}
	return messages, traditional, nil
}

func detectSummarySections(summary string) ([]summarySection, bool, bool) {
	type detectedSection struct {
		title string
		body  []string
	}

	var sections []detectedSection
	current := -1
	fenceLen := 0
	for _, line := range strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n") {
		if fenceLen == 0 {
			if title, ok := summaryHeadingTitle(line); ok {
				sections = append(sections, detectedSection{title: title})
				current = len(sections) - 1
				continue
			}
			if markerLen, ok := openingFenceLine(line); ok {
				fenceLen = markerLen
			}
		} else if closingFenceLine(line, fenceLen) {
			fenceLen = 0
		}
		if current >= 0 {
			sections[current].body = append(sections[current].body, line)
		}
	}

	if len(sections) != len(expectedSummarySectionTitles) {
		return nil, false, false
	}

	seen := map[string]bool{}
	result := make([]summarySection, 0, len(expectedSummarySectionTitles))
	for _, section := range sections {
		body := strings.TrimSpace(strings.Join(section.body, "\n"))
		if seen[section.title] || body == "" {
			return nil, false, false
		}
		seen[section.title] = true
		result = append(result, summarySection{Title: section.title, Body: body})
	}
	if hasAllTitles(seen, expectedSummarySectionTitles) {
		return result, false, true
	}
	if hasAllTitles(seen, expectedTraditionalSummarySectionTitles) {
		return result, true, true
	}
	return nil, false, false
}

func hasAllTitles(seen map[string]bool, titles []string) bool {
	for _, title := range titles {
		if !seen[title] {
			return false
		}
	}
	return true
}

func summaryHeadingTitle(line string) (string, bool) {
	candidate := normalizeSummaryHeading(line)
	if candidate == "" {
		return "", false
	}
	switch {
	case containsAny(candidate, "核心摘要"):
		return "核心摘要", true
	case containsAny(candidate, "容易被忽略但有價值的資訊", "有價值的資訊"):
		return "容易被忽略但有價值的資訊", true
	case containsAny(candidate, "容易被忽略但有价值的信息", "有价值的信息", "容易被忽略但有价值的资讯", "有价值的资讯", "有价值资讯"):
		return "容易被忽略但有价值的信息", true
	case containsAny(candidate, "隱含地可以 bullish / bearish on 什麼", "隱含"):
		return "隱含地可以 bullish / bearish on 什麼", true
	case containsAny(candidate, "隐含地可以 bullish / bearish on 什么", "隐含"):
		return "隐含地可以 bullish / bearish on 什么", true
	case containsAny(candidate, "直觀地可以 bullish / bearish on 什麼", "直觀"):
		return "直觀地可以 bullish / bearish on 什麼", true
	case containsAny(candidate, "直观地可以 bullish / bearish on 什么", "直观"):
		return "直观地可以 bullish / bearish on 什么", true
	case containsAny(candidate, "可能利好/利空的股票", "利好/利空", "利好利空") || containsAll(candidate, "利好", "利空"):
		return "可能利好/利空的股票", true
	default:
		return "", false
	}
}

func normalizeSummaryHeading(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || runeLen(trimmed) > 80 {
		return ""
	}
	looksLikeHeading := strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") || startsWithNumbering(trimmed)
	if !looksLikeHeading {
		return ""
	}
	trimmed = strings.TrimLeft(trimmed, "# ")
	trimmed = strings.TrimSpace(strings.Trim(trimmed, "*"))
	trimmed = strings.TrimSpace(stripNumbering(trimmed))
	trimmed = strings.TrimRight(trimmed, ":：")
	return strings.ToLower(strings.TrimSpace(trimmed))
}

func startsWithNumbering(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	r, size := utf8FirstRune(text)
	if unicode.IsDigit(r) {
		rest := strings.TrimSpace(text[size:])
		return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "、") || strings.HasPrefix(rest, ")") || strings.HasPrefix(rest, "）")
	}
	return strings.ContainsRune("一二三四五六七八九十", r) && strings.HasPrefix(strings.TrimSpace(text[size:]), "、")
}

func stripNumbering(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	r, size := utf8FirstRune(text)
	if unicode.IsDigit(r) || strings.ContainsRune("一二三四五六七八九十", r) {
		rest := strings.TrimSpace(text[size:])
		separator, separatorSize := utf8FirstRune(rest)
		if separatorSize > 0 && strings.ContainsRune(".、)）", separator) {
			return strings.TrimSpace(rest[separatorSize:])
		}
	}
	return text
}

func utf8FirstRune(text string) (rune, int) {
	for i, r := range text {
		return r, len(text[:i+len(string(r))])
	}
	return 0, 0
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}

func renderSummaryMetadataHeaderHTML(metadata display.SummaryMetadata) string {
	return renderSummaryMetadataHeaderHTMLOptions(metadata, defaultSummaryRenderOptions)
}

func renderSummaryMetadataHeaderHTMLOptions(metadata display.SummaryMetadata, opts summaryRenderOptions) string {
	if metadata.Empty() {
		return ""
	}
	lines := []string{"<b>新 Podcast 更新</b>"}
	if strings.TrimSpace(metadata.PodcastTitle) != "" {
		lines = append(lines, "播客："+html.EscapeString(strings.TrimSpace(metadata.PodcastTitle)))
	}
	if strings.TrimSpace(metadata.EpisodeTitle) != "" {
		lines = append(lines, "单集："+html.EscapeString(strings.TrimSpace(metadata.EpisodeTitle)))
	}
	if strings.TrimSpace(metadata.PubDate) != "" {
		lines = append(lines, "发布时间："+html.EscapeString(strings.TrimSpace(metadata.PubDate)))
	}
	if strings.TrimSpace(metadata.Link) != "" {
		lines = append(lines, opts.metadataLinkLabel()+"："+html.EscapeString(strings.TrimSpace(metadata.Link)))
	}
	return strings.Join(lines, "\n")
}

func summaryTextWithMetadata(summary string, metadata display.SummaryMetadata) string {
	return summaryTextWithMetadataOptions(summary, metadata, defaultSummaryRenderOptions)
}

func summaryTextWithMetadataOptions(summary string, metadata display.SummaryMetadata, opts summaryRenderOptions) string {
	if metadata.Empty() {
		return summary
	}
	lines := []string{"新 Podcast 更新"}
	if strings.TrimSpace(metadata.PodcastTitle) != "" {
		lines = append(lines, "播客："+strings.TrimSpace(metadata.PodcastTitle))
	}
	if strings.TrimSpace(metadata.EpisodeTitle) != "" {
		lines = append(lines, "单集："+strings.TrimSpace(metadata.EpisodeTitle))
	}
	if strings.TrimSpace(metadata.PubDate) != "" {
		lines = append(lines, "发布时间："+strings.TrimSpace(metadata.PubDate))
	}
	if strings.TrimSpace(metadata.Link) != "" {
		lines = append(lines, opts.metadataLinkLabel()+"："+strings.TrimSpace(metadata.Link))
	}
	return strings.Join(lines, "\n") + "\n\n" + strings.TrimSpace(summary)
}

func renderSummaryFragments(sections []summarySection, limit int) []string {
	var fragments []string
	for _, section := range sections {
		parts := splitSummaryBody(section.Title, section.Body, limit)
		for i, part := range parts {
			title := section.Title
			if len(parts) > 1 {
				title = fmt.Sprintf("%s (%d/%d)", section.Title, i+1, len(parts))
			}
			fragments = append(fragments, renderExpandableSectionHTMLWithFence(title, part.Body, part.FenceLen))
		}
	}
	return fragments
}

func renderExpandableSectionHTML(title, body string) string {
	return renderExpandableSectionHTMLWithFence(title, body, 0)
}

func renderExpandableSectionHTMLWithFence(title, body string, fenceLen int) string {
	rendered, _ := renderSummaryMarkdownHTMLWithFence(body, fenceLen)
	return fmt.Sprintf("<b>%s</b>\n<blockquote expandable>%s</blockquote>", html.EscapeString(strings.TrimSpace(title)), rendered)
}

func renderSummaryMarkdownHTML(text string) string {
	rendered, _ := renderSummaryMarkdownHTMLWithFence(text, 0)
	return rendered
}

func renderSummaryMarkdownHTMLWithFence(text string, fenceLen int) (string, int) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if fenceLen == 0 {
		text = strings.TrimSpace(text)
	} else {
		text = strings.Trim(text, "\r\n")
	}
	if text == "" {
		return "", fenceLen
	}

	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if markerLen, ok := openingFenceLine(line); ok && fenceLen == 0 {
			lines[i] = html.EscapeString(line)
			fenceLen = markerLen
			continue
		}
		if fenceLen > 0 {
			lines[i] = html.EscapeString(line)
			if closingFenceLine(line, fenceLen) {
				fenceLen = 0
			}
			continue
		}
		lines[i] = renderSummaryMarkdownLineHTML(line)
	}
	return strings.Join(lines, "\n"), fenceLen
}

func renderSummaryMarkdownLineHTML(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	if heading, ok := markdownHeading(trimmed); ok {
		return "<b>" + renderMarkdownInlineHTML(heading) + "</b>"
	}
	if item, ok := markdownBullet(trimmed); ok {
		if item == "" {
			return "•"
		}
		return "• " + renderMarkdownInlineHTML(item)
	}
	return renderMarkdownInlineHTML(trimmed)
}

func markdownHeading(line string) (string, bool) {
	markerEnd := 0
	for markerEnd < len(line) && line[markerEnd] == '#' {
		markerEnd++
	}
	if markerEnd == 0 || markerEnd > 6 || markerEnd >= len(line) || line[markerEnd] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(line[markerEnd:])
	return title, title != ""
}

func markdownBullet(line string) (string, bool) {
	for _, prefix := range []string{"- ", "* ", "• "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", false
}

func renderMarkdownInlineHTML(text string) string {
	var builder strings.Builder
	for i := 0; i < len(text); {
		if isEscapedMarkdownMarker(text, i) {
			writeEscapedRune(&builder, text, &i)
			continue
		}
		switch {
		case strings.HasPrefix(text[i:], "**"):
			if writeMarkdownInlineTag(&builder, text, &i, "**", "b") {
				continue
			}
		case strings.HasPrefix(text[i:], "__"):
			if writeMarkdownInlineTag(&builder, text, &i, "__", "b") {
				continue
			}
		case text[i] == '`':
			if writeMarkdownCode(&builder, text, &i) {
				continue
			}
		case text[i] == '*' && (i+1 >= len(text) || text[i+1] != '*'):
			if writeMarkdownInlineTag(&builder, text, &i, "*", "i") {
				continue
			}
		}
		writeEscapedRune(&builder, text, &i)
	}
	return builder.String()
}

func writeMarkdownInlineTag(builder *strings.Builder, text string, index *int, marker, tag string) bool {
	contentStart := *index + len(marker)
	end := findClosingMarkdownMarker(text, marker, contentStart)
	if end <= contentStart || unicode.IsSpace(rune(text[contentStart])) || unicode.IsSpace(rune(text[end-1])) {
		return false
	}

	builder.WriteString("<" + tag + ">")
	builder.WriteString(renderMarkdownInlineHTML(text[contentStart:end]))
	builder.WriteString("</" + tag + ">")
	*index = end + len(marker)
	return true
}

func writeMarkdownCode(builder *strings.Builder, text string, index *int) bool {
	contentStart := *index + 1
	end := findClosingMarkdownMarker(text, "`", contentStart)
	if end <= contentStart {
		return false
	}

	builder.WriteString(html.EscapeString(text[contentStart:end]))
	*index = end + 1
	return true
}

func isEscapedMarkdownMarker(text string, index int) bool {
	return index > 0 && text[index-1] == '\\' && (text[index] == '*' || text[index] == '_' || text[index] == '`')
}

func findClosingMarkdownMarker(text, marker string, start int) int {
	for start < len(text) {
		idx := strings.Index(text[start:], marker)
		if idx < 0 {
			return -1
		}
		idx += start
		if marker != "`" {
			if codeStart := strings.IndexByte(text[start:idx], '`'); codeStart >= 0 {
				codeStart += start
				if codeEnd := findClosingMarkdownMarker(text, "`", codeStart+1); codeEnd >= 0 {
					start = codeEnd + 1
					continue
				}
			}
		}
		if idx > 0 && text[idx-1] == '\\' {
			start = idx + len(marker)
			continue
		}
		if marker == "*" && ((idx > 0 && text[idx-1] == '*') || (idx+1 < len(text) && text[idx+1] == '*')) {
			start = idx + 1
			continue
		}
		return idx
	}
	return -1
}

func writeEscapedRune(builder *strings.Builder, text string, index *int) {
	for _, r := range text[*index:] {
		s := string(r)
		builder.WriteString(html.EscapeString(s))
		*index += len(s)
		return
	}
}

func splitSummaryBody(title, body string, limit int) []summaryPart {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if runeLen(renderExpandableSectionHTML(title, body)) <= limit {
		return []summaryPart{{Body: body}}
	}
	continuedTitle := fmt.Sprintf("%s (999/999)", title)
	return splitByRenderedLimit(body, func(part string, fenceLen int) int {
		return runeLen(renderExpandableSectionHTMLWithFence(continuedTitle, part, fenceLen))
	}, limit)
}

func splitByRenderedLimit(text string, renderedLen func(string, int) int, limit int) []summaryPart {
	var parts []summaryPart
	fenceLen := 0
	for strings.TrimSpace(text) != "" {
		text = trimSplitText(text, fenceLen)
		cut := len(text)
		for cut > 0 && renderedLen(text[:cut], fenceLen) > limit {
			if next := summaryBoundaryCut(text[:cut], runeLen(text[:cut])/2, "\n\n"); next > 0 && next < cut {
				cut = next
			} else if next := summaryBoundaryCut(text[:cut], runeLen(text[:cut])/2, "\n"); next > 0 && next < cut {
				cut = next
			} else {
				cut = byteIndexAfterRunes(text, max(1, runeLen(text[:cut])/2))
			}
		}
		if cut <= 0 {
			cut = byteIndexAfterRunes(text, 1)
		}
		part := trimSplitText(text[:cut], fenceLen)
		parts = append(parts, summaryPart{Body: part, FenceLen: fenceLen})
		_, fenceLen = renderSummaryMarkdownHTMLWithFence(part, fenceLen)
		text = trimSplitText(text[cut:], fenceLen)
	}
	return parts
}

func trimSplitText(text string, fenceLen int) string {
	if fenceLen > 0 {
		return strings.Trim(text, "\r\n")
	}
	return strings.TrimSpace(text)
}

func summaryBoundaryCut(text string, minRunes int, separator string) int {
	idx := strings.LastIndex(text, separator)
	if idx < 0 {
		return 0
	}
	cut := idx + len(separator)
	if runeLen(text[:cut]) < minRunes {
		return 0
	}
	return cut
}

func openingFenceLine(line string) (int, bool) {
	trimmed := strings.TrimSpace(line)
	markerLen := leadingBackticks(trimmed)
	return markerLen, markerLen >= 3
}

func closingFenceLine(line string, openingLen int) bool {
	trimmed := strings.TrimSpace(line)
	markerLen := leadingBackticks(trimmed)
	return markerLen >= openingLen && strings.TrimSpace(trimmed[markerLen:]) == ""
}

func leadingBackticks(text string) int {
	count := 0
	for count < len(text) && text[count] == '`' {
		count++
	}
	return count
}

func packHTMLFragments(fragments []string, limit int) []string {
	var messages []string
	var current strings.Builder
	for _, fragment := range fragments {
		if runeLen(fragment) > limit {
			if current.Len() > 0 {
				messages = append(messages, current.String())
				current.Reset()
			}
			messages = append(messages, fragment)
			continue
		}
		separator := ""
		if current.Len() > 0 {
			separator = "\n\n"
		}
		candidate := current.String() + separator + fragment
		if current.Len() > 0 && runeLen(candidate) > limit {
			messages = append(messages, current.String())
			current.Reset()
			current.WriteString(fragment)
			continue
		}
		if separator != "" {
			current.WriteString(separator)
		}
		current.WriteString(fragment)
	}
	if current.Len() > 0 {
		messages = append(messages, current.String())
	}
	return messages
}
