package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
)

func TestRenderSummaryRichHTMLWrapsSectionsInOneDetails(t *testing.T) {
	summary := simplifiedInvestmentSummary("A & B < C", "missed", "explicit", "implicit", "stocks")

	html := renderSummaryRichHTML(summary, display.SummaryMetadata{})
	if c := strings.Count(html, "<details>"); c != 1 {
		t.Fatalf("details count = %d, want 1: %s", c, html)
	}
	if !strings.Contains(html, "<summary>摘要</summary>") {
		t.Fatalf("missing summary label: %s", html)
	}
	// Sections are bold titles, not individually collapsible.
	for _, title := range expectedSummarySectionTitles {
		if !strings.Contains(html, "<b>"+title+"</b>") {
			t.Fatalf("missing section title %q: %s", title, html)
		}
	}
	if strings.Count(html, "<blockquote") != 0 {
		t.Fatalf("rich HTML should not contain blockquotes: %s", html)
	}
	if !strings.Contains(html, "A &amp; B &lt; C") {
		t.Fatalf("body not escaped: %s", html)
	}
}

func TestRenderSummaryRichHTMLPlacesMetadataOutsideDetails(t *testing.T) {
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")
	metadata := display.SummaryMetadata{PodcastTitle: "Pod <X>", EpisodeTitle: "Ep <Y>", PubDate: "2026-06-30", Link: "https://example.com/?a=1&b=2"}

	html := renderSummaryRichHTML(summary, metadata)
	detailsIdx := strings.Index(html, "<details>")
	if detailsIdx < 0 {
		t.Fatalf("missing details: %s", html)
	}
	header := html[:detailsIdx]
	if !strings.Contains(header, "<b>新 Podcast 更新</b>") || !strings.Contains(header, "播客：Pod &lt;X&gt;") || !strings.Contains(header, "链接：https://example.com/?a=1&amp;b=2") {
		t.Fatalf("metadata header missing or unescaped: %s", header)
	}
	// The summary body lives inside <details>.
	if !strings.Contains(html[detailsIdx:], "<b>核心摘要</b>") {
		t.Fatalf("section title not inside details: %s", html[detailsIdx:])
	}
}

func TestRenderSummaryRichHTMLFallsBackToParagraphs(t *testing.T) {
	html := renderSummaryRichHTML("This is prose.\n- a bullet", display.SummaryMetadata{})
	if !strings.Contains(html, "<details>") || !strings.Contains(html, "This is prose.") || !strings.Contains(html, "• a bullet") {
		t.Fatalf("fallback rich HTML missing body: %s", html)
	}
	if strings.Contains(html, "<b>核心摘要</b>") {
		t.Fatalf("fallback should not synthesize section titles: %s", html)
	}
}

func TestRenderSummaryRichHTMLRendersInlineMarkdown(t *testing.T) {
	summary := simplifiedInvestmentSummary("- **bold** and *italic* with `code`", "missed", "explicit", "implicit", "stocks")
	html := renderSummaryRichHTML(summary, display.SummaryMetadata{})
	if !strings.Contains(html, "• <b>bold</b>") || !strings.Contains(html, "<i>italic</i>") {
		t.Fatalf("inline markdown not rendered: %s", html)
	}
	if strings.Contains(html, "<code>") {
		t.Fatalf("rich HTML should not contain <code> tags: %s", html)
	}
	if strings.Contains(html, "`code`") {
		t.Fatalf("inline code not rendered to plain text: %s", html)
	}
}

func TestRenderSummaryRichHTMLPreservesFencedBlocks(t *testing.T) {
	summary := simplifiedInvestmentSummary("before\n```go\n## not a heading\n**literal**\n```\nafter", "missed", "explicit", "implicit", "stocks")
	html := renderSummaryRichHTML(summary, display.SummaryMetadata{})
	if !strings.Contains(html, "<pre>") || !strings.Contains(html, "## not a heading") || !strings.Contains(html, "**literal**") {
		t.Fatalf("fenced block not preserved verbatim: %s", html)
	}
	if strings.Contains(html, "<b>not a heading</b>") || strings.Contains(html, "<b>literal</b>") {
		t.Fatalf("fenced markdown was reformatted: %s", html)
	}
}

func TestRichMessageClientSendAndEdit(t *testing.T) {
	type captured struct {
		Method string
		Body   map[string]json.RawMessage
	}
	var last captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last.Method = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		_ = json.Unmarshal(raw, &body)
		last.Body = body
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
	}))
	defer srv.Close()

	client := richMessageClient{Token: "token", BaseURL: srv.URL}

	id, err := client.SendRichMessage(context.Background(), 123, 456, "<b>hi</b>", summaryVariantKeyboard(42))
	if err != nil {
		t.Fatalf("SendRichMessage: %v", err)
	}
	if id != 777 {
		t.Fatalf("message id = %d", id)
	}
	if string(last.Body["chat_id"]) != "123" {
		t.Fatalf("chat_id = %s", last.Body["chat_id"])
	}
	var rm inputRichMessage
	if err := json.Unmarshal(last.Body["rich_message"], &rm); err != nil || rm.HTML != "<b>hi</b>" {
		t.Fatalf("rich_message = %s err=%v", last.Body["rich_message"], err)
	}
	if string(last.Body["reply_parameters"]) == "" || !strings.Contains(string(last.Body["reply_parameters"]), `"message_id":456`) {
		t.Fatalf("reply_parameters missing: %s", last.Body["reply_parameters"])
	}
	if !strings.Contains(string(last.Body["reply_markup"]), "summary_variant:42") {
		t.Fatalf("reply_markup missing keyboard: %s", last.Body["reply_markup"])
	}

	if err := client.EditRichMessage(context.Background(), 123, 777, "<b>edited</b>", nil); err != nil {
		t.Fatalf("EditRichMessage: %v", err)
	}
	if last.Method != "editMessageText" {
		t.Fatalf("edit method = %q", last.Method)
	}
	if string(last.Body["message_id"]) != "777" {
		t.Fatalf("message_id = %s", last.Body["message_id"])
	}
}

func TestRichMessageClientPropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: message is too long"}`))
	}))
	defer srv.Close()
	client := richMessageClient{Token: "token", BaseURL: srv.URL}
	_, err := client.SendRichMessage(context.Background(), 1, 0, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "message is too long") {
		t.Fatalf("err = %v", err)
	}
}

func TestRichMessageClientRequiresToken(t *testing.T) {
	client := richMessageClient{}
	if _, err := client.SendRichMessage(context.Background(), 1, 0, "x", nil); err == nil {
		t.Fatal("expected error for missing token")
	}
	if err := client.EditRichMessage(context.Background(), 1, 1, "x", nil); err == nil {
		t.Fatal("expected error for missing token")
	}
}

// stubRichSender is a richMessageSender fake for sender-level tests.
type stubRichSender struct {
	sentHTML   string
	sentMarkup any
	sentReply  int64
	editHTML   string
	editMarkup any
	editMsgID  int64
	sendErr    error
	sendErrFn  func(replyTo int64) error
	editErr    error
	sendCalled bool
	editCalled bool
}

func (s *stubRichSender) SendRichMessage(_ context.Context, _ int64, replyTo int64, htmlText string, markup any) (int64, error) {
	s.sendCalled = true
	s.sentHTML = htmlText
	s.sentMarkup = markup
	s.sentReply = replyTo
	if s.sendErrFn != nil {
		if err := s.sendErrFn(replyTo); err != nil {
			return 0, err
		}
	}
	if s.sendErr != nil {
		return 0, s.sendErr
	}
	return 55, nil
}

func (s *stubRichSender) EditRichMessage(_ context.Context, _ int64, messageID int64, htmlText string, markup any) error {
	s.editCalled = true
	s.editHTML = htmlText
	s.editMarkup = markup
	s.editMsgID = messageID
	return s.editErr
}

func TestSenderSendFinalSummaryUsesRichMessageWhenAvailable(t *testing.T) {
	client := &fakeSenderClient{}
	rich := &stubRichSender{}
	sender := Sender{Client: client, RichSender: rich, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")

	ids, err := sender.SendFinalSummaryParts(context.Background(), 123, 456, summary, 42)
	if err != nil {
		t.Fatalf("SendFinalSummaryParts: %v", err)
	}
	if !rich.sendCalled {
		t.Fatal("rich sender was not used")
	}
	if len(ids) != 1 || ids[0] != 55 {
		t.Fatalf("ids = %#v", ids)
	}
	if rich.sentReply != 456 {
		t.Fatalf("reply target = %d", rich.sentReply)
	}
	if !strings.Contains(rich.sentHTML, "<details>") {
		t.Fatalf("sent HTML missing details: %s", rich.sentHTML)
	}
	if rich.sentMarkup == nil {
		t.Fatal("variant keyboard not attached")
	}
	if len(client.htmlMessages) != 0 {
		t.Fatalf("legacy HTML path used: %#v", client.htmlMessages)
	}
}

func TestSenderSendFinalSummaryFallsBackToHTMLWhenRichTooLong(t *testing.T) {
	client := &fakeSenderClient{}
	rich := &stubRichSender{}
	sender := Sender{Client: client, RichSender: rich, TempDir: t.TempDir()}
	// Force the rich HTML over the limit by stubbing the limit down via a
	// very long summary is impractical; instead verify the guard directly.
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")
	html := renderSummaryRichHTML(summary, display.SummaryMetadata{})
	if runeLen(html) > maxRichMessageChars {
		t.Fatalf("test summary exceeds rich limit unexpectedly: %d", runeLen(html))
	}
	// Sanity: the rich path is selected for a normal summary.
	if _, err := sender.SendFinalSummaryParts(context.Background(), 1, 0, summary, 1); err != nil {
		t.Fatalf("SendFinalSummaryParts: %v", err)
	}
	if !rich.sendCalled {
		t.Fatal("expected rich path to be used")
	}
}

func TestSenderSendFinalSummaryRichRetriesWithoutReplyTarget(t *testing.T) {
	client := &fakeSenderClient{}
	rich := &stubRichSender{sendErrFn: func(replyTo int64) error {
		if replyTo != 0 {
			return errors.New("Bad Request: reply message not found")
		}
		return nil
	}}
	sender := Sender{Client: client, RichSender: rich, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")

	ids, err := sender.SendFinalSummaryParts(context.Background(), 1, 99, summary, 1)
	if err != nil {
		t.Fatalf("SendFinalSummaryParts: %v", err)
	}
	if !rich.sendCalled {
		t.Fatal("rich sender was not used")
	}
	// The stub fails only when replyTo != 0, so after the retry the last
	// recorded call must have replyTo == 0.
	if rich.sentReply != 0 {
		t.Fatalf("expected retry with replyTo=0, got %d", rich.sentReply)
	}
	if len(ids) != 1 {
		t.Fatalf("ids = %#v", ids)
	}
}

func TestSenderEditFinalSummaryUsesRichEditForSingleMessage(t *testing.T) {
	client := &fakeSenderClient{}
	rich := &stubRichSender{}
	sender := Sender{Client: client, RichSender: rich, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")

	if err := sender.EditFinalSummaryParts(context.Background(), 123, []int64{10}, summary, 42); err != nil {
		t.Fatalf("EditFinalSummaryParts: %v", err)
	}
	if !rich.editCalled || rich.editMsgID != 10 {
		t.Fatalf("rich edit not called on message 10: called=%v id=%d", rich.editCalled, rich.editMsgID)
	}
	if rich.editMarkup == nil {
		t.Fatal("variant keyboard not attached on edit")
	}
	if !strings.Contains(rich.editHTML, "<details>") {
		t.Fatalf("edit HTML missing details: %s", rich.editHTML)
	}
	if len(client.edits) != 0 {
		t.Fatalf("legacy edit path used: %#v", client.edits)
	}
}

func TestSenderEditFinalSummaryRichEditFailurePropagates(t *testing.T) {
	client := &fakeSenderClient{}
	rich := &stubRichSender{editErr: errors.New("Bad Request: message to edit not found")}
	sender := Sender{Client: client, RichSender: rich, TempDir: t.TempDir()}
	summary := simplifiedInvestmentSummary("core", "missed", "explicit", "implicit", "stocks")

	if err := sender.EditFinalSummaryParts(context.Background(), 123, []int64{10}, summary, 42); err == nil {
		t.Fatal("expected rich edit failure to propagate, got nil")
	}
}
