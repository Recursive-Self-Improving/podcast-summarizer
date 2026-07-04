package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

// maxRichMessageChars is the Telegram rich-message character ceiling
// (Bot API 10.1). Realistic podcast summaries fit comfortably; the limit is
// enforced defensively so a runaway summary degrades to the document fallback
// rather than being rejected by the API.
const maxRichMessageChars = 32768

// richMessageSummaryLabel is the always-visible summary line of the
// collapsible <details> block that wraps the whole summary.
const richMessageSummaryLabel = "摘要"

// NewRichMessageClient builds a richMessageSender backed by direct HTTP
// calls to the Telegram Bot API using the given bot token.
func NewRichMessageClient(token string) richMessageSender {
	return richMessageClient{Token: token}
}

// renderSummaryRichHTML builds the HTML body of a rich message for a podcast
// summary. The metadata header (when present) renders as bold lines outside
// any collapsible block so it stays visible. Each detected investment section
// renders as its own <details><summary>…</summary>…</details> block: the
// section title is the always-visible, clickable summary line and the section
// body (paragraphs/lists/preformatted blocks) is collapsed by default. This
// keeps all five subtitles visible without expanding, while each section's
// content stays collapsible. When the five investment sections cannot be
// detected, the whole trimmed summary is wrapped in a single collapsible
// <details><summary>摘要</summary> block as a fallback.
//
// Rich messages render as real HTML, where bare newlines collapse to spaces.
// Block-level elements (<details>/<summary>, <p>, <ul>, <pre>, <h4>) are
// therefore required to separate titles, paragraphs, and list items visually.
func renderSummaryRichHTML(summary string, metadata display.SummaryMetadata) string {
	return renderSummaryRichHTMLOptions(summary, metadata, defaultSummaryRenderOptions)
}

func renderSummaryRichHTMLOptions(summary string, metadata display.SummaryMetadata, opts summaryRenderOptions) string {
	var b strings.Builder
	if header := renderSummaryMetadataHeaderHTMLOptions(metadata, opts); header != "" {
		// The shared header joins lines with "\n", which the legacy
		// parse_mode=HTML path needs (newlines are line breaks there).
		// In rich HTML newlines collapse, so convert to <br>.
		b.WriteString(strings.ReplaceAll(header, "\n", "<br>"))
		b.WriteString("\n\n")
	}

	b.WriteString(renderSummaryRichBodyHTML(summary))
	return b.String()
}

// renderSummaryRichBodyHTML renders the summary body (without metadata) as
// rich-message HTML. When the five investment sections are detected, each
// section becomes its own collapsible <details> block with the section title
// as the <summary> label. Otherwise the whole body is wrapped in a single
// collapsible block labeled "摘要".
func renderSummaryRichBodyHTML(summary string) string {
	summary = strings.ReplaceAll(strings.TrimSpace(summary), "\r\n", "\n")
	if summary == "" {
		return ""
	}

	sections, _, ok := detectSummarySections(summary)
	if !ok {
		return renderCollapsibleSection(richMessageSummaryLabel, summary)
	}

	var b strings.Builder
	for i, section := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderCollapsibleSection(section.Title, section.Body))
	}
	return b.String()
}

// renderCollapsibleSection wraps body in a <details><summary>title</summary>
// block. The title is trimmed and HTML-escaped; the body is rendered as
// rich-message block elements via renderRichBlocksHTML.
func renderCollapsibleSection(title, body string) string {
	return "<details><summary>" + html.EscapeString(strings.TrimSpace(title)) + "</summary>" + renderRichBlocksHTML(body) + "</details>"
}

// renderRichBlocksHTML renders markdown text as a sequence of rich-message
// block elements: consecutive bullet lines become <ul><li>…</li></ul>,
// consecutive prose lines separated by blank lines become <p>…</p>, fenced
// code blocks become <pre>…</pre> (escaped, verbatim), and markdown headings
// become <h4>. Inline formatting (bold/italic/code) is applied within <p>
// and <li> via renderMarkdownInlineHTML.
func renderRichBlocksHTML(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var b strings.Builder
	var prose []string
	var bullets []string
	fenceLen := 0

	flushProse := func() {
		if len(prose) == 0 {
			return
		}
		b.WriteString("<p>")
		for i, line := range prose {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(renderMarkdownInlineHTML(strings.TrimSpace(line)))
		}
		b.WriteString("</p>")
		prose = prose[:0]
	}

	flushBullets := func() {
		if len(bullets) == 0 {
			return
		}
		b.WriteString("<ul>")
		for _, item := range bullets {
			b.WriteString("<li>")
			if item == "" {
				b.WriteString("•")
			} else {
				b.WriteString(renderMarkdownInlineHTML(item))
			}
			b.WriteString("</li>")
		}
		b.WriteString("</ul>")
		bullets = bullets[:0]
	}

	for _, line := range lines {
		if fenceLen > 0 {
			b.WriteString(html.EscapeString(line))
			b.WriteString("\n")
			if closingFenceLine(line, fenceLen) {
				b.WriteString("</pre>")
				fenceLen = 0
			}
			continue
		}
		if markerLen, ok := openingFenceLine(line); ok {
			flushProse()
			flushBullets()
			b.WriteString("<pre>")
			b.WriteString(html.EscapeString(line))
			b.WriteString("\n")
			fenceLen = markerLen
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushProse()
			flushBullets()
			continue
		}
		if item, ok := markdownBullet(trimmed); ok {
			flushProse()
			bullets = append(bullets, item)
			continue
		}
		if heading, ok := markdownHeading(trimmed); ok {
			flushProse()
			flushBullets()
			b.WriteString("<h4>")
			b.WriteString(renderMarkdownInlineHTML(heading))
			b.WriteString("</h4>")
			continue
		}
		flushBullets()
		prose = append(prose, line)
	}
	flushProse()
	flushBullets()
	if fenceLen > 0 {
		b.WriteString("</pre>")
	}
	return b.String()
}

// richMessageClient implements richMessageSender with direct HTTP POST calls
// to the Telegram Bot API. It is the only path that uses sendRichMessage and
// editMessageText(rich_message=...); all other bot operations go through
// go-telegram/bot as before.
type richMessageClient struct {
	Token      string
	HTTPClient *http.Client
	BaseURL    string
}

const defaultRichHTTPTimeout = 30 * time.Second

func (c richMessageClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: defaultRichHTTPTimeout}
}

func (c richMessageClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.telegram.org"
}

// inputRichMessage is the JSON shape of the rich_message parameter. Only the
// HTML field is populated; Markdown is left empty.
type inputRichMessage struct {
	HTML string `json:"html,omitempty"`
}

type sendRichMessageParams struct {
	ChatID          int64                   `json:"chat_id"`
	RichMessage     inputRichMessage        `json:"rich_message"`
	ReplyParameters *models.ReplyParameters `json:"reply_parameters,omitempty"`
	ReplyMarkup     models.ReplyMarkup      `json:"reply_markup,omitempty"`
}

type editRichMessageParams struct {
	ChatID      int64              `json:"chat_id"`
	MessageID   int64              `json:"message_id"`
	RichMessage inputRichMessage   `json:"rich_message"`
	ReplyMarkup models.ReplyMarkup `json:"reply_markup,omitempty"`
}

func (c richMessageClient) SendRichMessage(ctx context.Context, chatID, replyToMessageID int64, htmlText string, markup any) (int64, error) {
	if strings.TrimSpace(c.Token) == "" {
		return 0, errors.New("telegram bot token is required for rich messages")
	}
	params := sendRichMessageParams{
		ChatID:      chatID,
		RichMessage: inputRichMessage{HTML: htmlText},
		ReplyMarkup: markup,
	}
	if replyToMessageID != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: int(replyToMessageID)}
	}
	var resp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	if err := c.call(ctx, "sendRichMessage", params, &resp); err != nil {
		return 0, err
	}
	if !resp.OK {
		return 0, fmt.Errorf("sendRichMessage: %s", resp.Description)
	}
	return resp.Result.MessageID, nil
}

func (c richMessageClient) EditRichMessage(ctx context.Context, chatID, messageID int64, htmlText string, markup any) error {
	if strings.TrimSpace(c.Token) == "" {
		return errors.New("telegram bot token is required for rich messages")
	}
	params := editRichMessageParams{
		ChatID:      chatID,
		MessageID:   messageID,
		RichMessage: inputRichMessage{HTML: htmlText},
		ReplyMarkup: markup,
	}
	var resp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := c.call(ctx, "editMessageText", params, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("editMessageText: %s", resp.Description)
	}
	return nil
}

func (c richMessageClient) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}
	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL(), c.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}
