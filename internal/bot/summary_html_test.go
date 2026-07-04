package bot

import (
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

func TestRenderFinalSummaryHTMLMessagesDetectsFiveInvestmentSections(t *testing.T) {
	summary := simplifiedInvestmentSummary("A & B < C", "hidden", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, maxTelegramTextChars)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	for _, title := range expectedSummarySectionTitles {
		if !strings.Contains(message, "<b>"+title+"</b>") {
			t.Fatalf("message missing title %q: %s", title, message)
		}
	}
	if count := strings.Count(message, "<blockquote expandable>"); count != 5 {
		t.Fatalf("blockquote count = %d, message = %s", count, message)
	}
	if !strings.Contains(message, "A &amp; B &lt; C") {
		t.Fatalf("message did not escape body: %s", message)
	}
}

func TestRenderFinalSummaryHTMLPartsWithMetadataAddsEscapedHeader(t *testing.T) {
	messages, err := renderFinalSummaryHTMLPartsWithMetadata(
		simplifiedInvestmentSummary("summary", "missed", "explicit", "implicit", "stocks"),
		display.SummaryMetadata{PodcastTitle: "A&B <Podcast>", EpisodeTitle: "E1 <Title>", PubDate: "2026-05-30", Link: "https://example.com/?a=1&b=2"},
		maxTelegramTextChars,
	)
	if err != nil {
		t.Fatalf("renderFinalSummaryHTMLPartsWithMetadata returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(messages[0], "<b>新 Podcast 更新</b>") || !strings.Contains(messages[0], "播客：A&amp;B &lt;Podcast&gt;") || !strings.Contains(messages[0], "单集：E1 &lt;Title&gt;") || !strings.Contains(messages[0], "链接：https://example.com/?a=1&amp;b=2") {
		t.Fatalf("metadata header missing or unescaped: %s", messages[0])
	}
	if count := strings.Count(strings.Join(messages, "\n"), "<blockquote expandable>"); count != 6 {
		t.Fatalf("blockquote count = %d, messages = %#v", count, messages)
	}
}

func TestRenderFinalSummaryHTMLPartsAddsSimplifiedPlaceholder(t *testing.T) {
	messages, err := renderFinalSummaryHTMLParts(simplifiedInvestmentSummary("summary", "missed", "explicit", "implicit", "stocks"), maxTelegramTextChars)
	if err != nil {
		t.Fatalf("renderFinalSummaryHTMLParts returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(messages[1], simplifiedPlaceholderSummary) {
		t.Fatalf("placeholder message = %s", messages[1])
	}
}

func TestRenderFinalSummaryHTMLPartsAddsTraditionalPlaceholder(t *testing.T) {
	messages, err := renderFinalSummaryHTMLParts(traditionalInvestmentSummary("summary", "missed", "explicit", "implicit", "stocks"), maxTelegramTextChars)
	if err != nil {
		t.Fatalf("renderFinalSummaryHTMLParts returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if !strings.Contains(messages[1], traditionalPlaceholderSummary) {
		t.Fatalf("placeholder message = %s", messages[1])
	}
}

func TestRenderFinalSummaryHTMLMessagesIgnoresHeadingsInsideFences(t *testing.T) {
	summary := simplifiedInvestmentSummary("```md\n## 容易被忽略但有价值的信息\nnot a section\n```\nbody", "real missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, maxTelegramTextChars)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if !strings.Contains(message, "<b>容易被忽略但有价值的信息</b>\n<blockquote expandable>real missed</blockquote>") {
		t.Fatalf("message did not detect real overlooked section: %s", message)
	}
	if !strings.Contains(message, "## 容易被忽略但有价值的信息\nnot a section") {
		t.Fatalf("message did not preserve fenced heading text: %s", message)
	}
}

func TestRenderFinalSummaryHTMLMessagesFormatsMarkdownInsideBlockquotes(t *testing.T) {
	summary := simplifiedInvestmentSummary(
		"- **神经递质的\"单一功能\"迷思**：A & B < C\n- *病理连续体* 的风险",
		"Use `yt-dlp` when needed",
		"explicit",
		"implicit",
		"stocks",
	)

	messages := renderFinalSummaryHTMLMessages(summary, maxTelegramTextChars)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if !strings.Contains(message, "• <b>神经递质的&#34;单一功能&#34;迷思</b>：A &amp; B &lt; C") {
		t.Fatalf("message did not render bold bullet body: %s", message)
	}
	if !strings.Contains(message, "• <i>病理连续体</i> 的风险") {
		t.Fatalf("message did not render italic bullet body: %s", message)
	}
	if !strings.Contains(message, "Use yt-dlp when needed") {
		t.Fatalf("message did not render inline code text: %s", message)
	}
	if strings.Contains(message, "<code>") {
		t.Fatalf("message nested code inside blockquote: %s", message)
	}
	if strings.Contains(message, "**神经递质") || strings.Contains(message, "`yt-dlp`") {
		t.Fatalf("message leaked raw markdown: %s", message)
	}
}

func TestRenderFinalSummaryHTMLMessagesDoesNotNestCodeInBold(t *testing.T) {
	summary := simplifiedInvestmentSummary("- **Use `yt-dlp` here**", "missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, maxTelegramTextChars)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if strings.Contains(message, "<b>Use <code>") {
		t.Fatalf("message nested code inside bold: %s", message)
	}
	if !strings.Contains(message, "• <b>Use yt-dlp here</b>") {
		t.Fatalf("message did not render nested code safely: %s", message)
	}
}

func TestRenderSummaryMarkdownHTMLEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unmatched delimiter",
			body: "Use **bold",
			want: "Use **bold",
		},
		{
			name: "escaped delimiter",
			body: `Use \**literal** text`,
			want: `Use \**literal** text`,
		},
		{
			name: "fenced code",
			body: "```go\n  # literal\n  - literal\n  **literal**\n  x < y\n```",
			want: "```go\n  # literal\n  - literal\n  **literal**\n  x &lt; y\n```",
		},
		{
			name: "longer fenced code",
			body: "````\n```json\n# literal\n```\n````",
			want: "````\n```json\n# literal\n```\n````",
		},
		{
			name: "code span inside bold",
			body: "**Use `**kwargs` safely**",
			want: "<b>Use **kwargs safely</b>",
		},
		{
			name: "literal arithmetic asterisks",
			body: "2 * 3 * 4",
			want: "2 * 3 * 4",
		},
		{
			name: "hashtag is not heading",
			body: "#hashtag stays plain",
			want: "#hashtag stays plain",
		},
		{
			name: "markdown heading",
			body: "### Heading",
			want: "<b>Heading</b>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderSummaryMarkdownHTML(tt.body); got != tt.want {
				t.Fatalf("renderSummaryMarkdownHTML(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestRenderFinalSummaryHTMLMessagesFallsBackToWholeSummary(t *testing.T) {
	summary := "This is just prose.\n- maybe markdown-ish"

	messages := renderFinalSummaryHTMLMessages(summary, maxTelegramTextChars)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if !strings.Contains(message, "<b>Summary</b>") || !strings.Contains(message, "<blockquote expandable>") {
		t.Fatalf("message did not use fallback block: %s", message)
	}
	if !strings.Contains(message, "This is just prose.") {
		t.Fatalf("message missing summary body: %s", message)
	}
}

func TestSummaryHeadingTitleAcceptsNumberedInvestmentHeadings(t *testing.T) {
	tests := map[string]string{
		"1. 核心摘要": "核心摘要",
		"二、容易被忽略但有價值的資訊":                  "容易被忽略但有價值的資訊",
		"**直观地可以 bullish/bearish on 什么**": "直观地可以 bullish / bearish on 什么",
		"## 容易被忽略但有价值的资讯":                 "容易被忽略但有价值的信息",
		"隱含地可以 bullish/bearish on 什麼：":    "隱含地可以 bullish / bearish on 什麼",
		"5）可能利好/利空的股票":                    "可能利好/利空的股票",
		"## 可能利好、利空的股票":                   "可能利好/利空的股票",
		"可能利好，利空的股票：":                     "可能利好/利空的股票",
	}
	for heading, want := range tests {
		t.Run(heading, func(t *testing.T) {
			got, ok := summaryHeadingTitle(heading)
			if !ok || got != want {
				t.Fatalf("summaryHeadingTitle(%q) = %q, %v; want %q, true", heading, got, ok, want)
			}
		})
	}
}

func TestRenderFinalSummaryHTMLMessagesSplitsIntoValidHTML(t *testing.T) {
	summary := simplifiedInvestmentSummary(strings.Repeat("a ", 200), strings.Repeat("b ", 200), strings.Repeat("c ", 200), strings.Repeat("d ", 200), strings.Repeat("e ", 200))

	messages := renderFinalSummaryHTMLMessages(summary, 420)
	assertValidHTMLMessages(t, messages, 420)
}

func TestRenderFinalSummaryHTMLMessagesBudgetsContinuationTitles(t *testing.T) {
	summary := simplifiedInvestmentSummary(strings.Repeat("a", 900), "missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, 420)
	assertValidHTMLMessages(t, messages, 420)
}

func TestRenderFinalSummaryHTMLMessagesSplitsFormattedSpanIntoValidHTML(t *testing.T) {
	summary := simplifiedInvestmentSummary("**"+strings.Repeat("bold ", 160)+"**", "missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, 420)
	assertValidHTMLMessages(t, messages, 420)
}

func TestRenderFinalSummaryHTMLMessagesDoesNotSplitFencedBlocks(t *testing.T) {
	summary := simplifiedInvestmentSummary("before\n```go\n  # literal\n"+strings.Repeat("  - literal\n", 80)+"  **literal**\n```\nafter", "missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, 420)
	assertValidHTMLMessages(t, messages, 420)
	joined := strings.Join(messages, "\n")
	if strings.Contains(joined, "<b>literal</b>") || strings.Contains(joined, "• literal") {
		t.Fatalf("fenced markdown was reformatted after splitting: %s", joined)
	}
	if !strings.Contains(joined, "  # literal") || !strings.Contains(joined, "  - literal") {
		t.Fatalf("fenced indentation was not preserved after splitting: %s", joined)
	}
	if strings.Contains(joined, "\n- literal") {
		t.Fatalf("fenced continuation indentation was stripped after splitting: %s", joined)
	}
}

func TestRenderFinalSummaryHTMLMessagesTracksLongerFencesAcrossSplits(t *testing.T) {
	body := "before\n````\n```json\n" + strings.Repeat("# literal\n", 40) + "```\n**literal**\n````\nafter"
	summary := simplifiedInvestmentSummary(body, "missed", "explicit", "implicit", "stocks")

	messages := renderFinalSummaryHTMLMessages(summary, 420)
	assertValidHTMLMessages(t, messages, 420)
	joined := strings.Join(messages, "\n")
	if strings.Contains(joined, "<b>literal</b>") {
		t.Fatalf("inner fence closed the outer fence after splitting: %s", joined)
	}
	if !strings.Contains(joined, "```json") || !strings.Contains(joined, "**literal**") {
		t.Fatalf("longer fenced block content was not preserved after splitting: %s", joined)
	}
}

func simplifiedInvestmentSummary(core, missed, explicit, implicit, stocks string) string {
	return strings.Join([]string{
		"## 核心摘要\n" + core,
		"## 容易被忽略但有价值的信息\n" + missed,
		"## 直观地可以 bullish / bearish on 什么\n" + explicit,
		"## 隐含地可以 bullish / bearish on 什么\n" + implicit,
		"## 可能利好/利空的股票\n" + stocks,
	}, "\n\n")
}

func traditionalInvestmentSummary(core, missed, explicit, implicit, stocks string) string {
	return strings.Join([]string{
		"## 核心摘要\n" + core,
		"## 容易被忽略但有價值的資訊\n" + missed,
		"## 直觀地可以 bullish / bearish on 什麼\n" + explicit,
		"## 隱含地可以 bullish / bearish on 什麼\n" + implicit,
		"## 可能利好/利空的股票\n" + stocks,
	}, "\n\n")
}

func assertValidHTMLMessages(t *testing.T, messages []string, limit int) {
	t.Helper()
	if len(messages) < 2 {
		t.Fatalf("expected split messages, got %#v", messages)
	}
	for _, message := range messages {
		if runeLen(message) > limit {
			t.Fatalf("oversized message length %d: %s", runeLen(message), message)
		}
		if strings.Count(message, "<blockquote expandable>") != strings.Count(message, "</blockquote>") {
			t.Fatalf("unbalanced blockquote tags: %s", message)
		}
		if strings.Count(message, "<b>") != strings.Count(message, "</b>") {
			t.Fatalf("unbalanced bold tags: %s", message)
		}
		if strings.Count(message, "<i>") != strings.Count(message, "</i>") {
			t.Fatalf("unbalanced italic tags: %s", message)
		}
		if strings.Contains(message, "<code>") {
			t.Fatalf("code tags are not allowed inside expandable blockquotes: %s", message)
		}
		if strings.Contains(message, "&am") && !strings.Contains(message, "&amp;") {
			t.Fatalf("message appears to split entity: %s", message)
		}
	}
}
