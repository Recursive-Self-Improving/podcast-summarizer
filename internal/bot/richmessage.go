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
// the collapsible block so it stays visible; the entire summary body is
// wrapped in a single <details><summary>…</summary>…</details> that is
// collapsed by default. Each detected section renders as a bold title line
// followed by its body (paragraphs/bullets); sections are not individually
// collapsible. When the five investment sections cannot be detected, the
// whole trimmed summary is rendered inside the collapsible block.
func renderSummaryRichHTML(summary string, metadata display.SummaryMetadata) string {
	var b strings.Builder
	if header := renderSummaryMetadataHeaderHTML(metadata); header != "" {
		b.WriteString(header)
		b.WriteString("\n\n")
	}

	body := renderSummaryRichBodyHTML(summary)
	b.WriteString("<details><summary>")
	b.WriteString(html.EscapeString(richMessageSummaryLabel))
	b.WriteString("</summary>")
	b.WriteString(body)
	b.WriteString("</details>")
	return b.String()
}

// renderSummaryRichBodyHTML renders the summary body (without metadata) as
// rich-message HTML inside the collapsible block.
func renderSummaryRichBodyHTML(summary string) string {
	summary = strings.ReplaceAll(strings.TrimSpace(summary), "\r\n", "\n")
	if summary == "" {
		return ""
	}

	sections, _, ok := detectSummarySections(summary)
	if !ok {
		return renderRichParagraphsHTML(summary)
	}

	var b strings.Builder
	for i, section := range sections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("<b>")
		b.WriteString(html.EscapeString(strings.TrimSpace(section.Title)))
		b.WriteString("</b>\n")
		b.WriteString(renderRichSectionBodyHTML(section.Body))
	}
	return b.String()
}

// renderRichSectionBodyHTML renders a section body as rich-message HTML,
// preserving fenced code blocks verbatim and converting markdown
// headings/bullets/inline formatting. Fenced blocks are escaped and emitted
// as <pre> so their content stays literal; markdown inside fences is not
// reformatted.
func renderRichSectionBodyHTML(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	fenceLen := 0
	var out strings.Builder
	first := true
	for _, line := range lines {
		if fenceLen == 0 {
			if markerLen, ok := openingFenceLine(line); ok {
				fenceLen = markerLen
				if !first {
					out.WriteString("\n")
				}
				out.WriteString("<pre>")
				out.WriteString(html.EscapeString(line))
				out.WriteString("\n")
				first = false
				continue
			}
		} else {
			out.WriteString(html.EscapeString(line))
			out.WriteString("\n")
			if closingFenceLine(line, fenceLen) {
				out.WriteString("</pre>")
				fenceLen = 0
			}
			continue
		}

		rendered := renderSummaryMarkdownLineHTML(line)
		if rendered == "" {
			continue
		}
		if !first {
			out.WriteString("\n")
		}
		out.WriteString(rendered)
		first = false
	}
	if fenceLen > 0 {
		out.WriteString("</pre>")
	}
	return out.String()
}

// renderRichParagraphsHTML renders free-form (non-sectioned) summary text as
// rich-message HTML paragraphs/bullets, preserving fenced code blocks.
func renderRichParagraphsHTML(text string) string {
	return renderRichSectionBodyHTML(text)
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
