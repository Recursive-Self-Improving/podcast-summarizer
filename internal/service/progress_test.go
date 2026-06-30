package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
)

func TestProgressNotifierInitialRequestStatusUsesOwnerProgressAndLaterNotice(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}
	media := db.MediaItem{ID: 1, CanonicalURL: "https://www.youtube.com/watch?v=abc12345678"}
	owner := db.SummaryRequest{ID: 1, MediaItemID: media.ID, ChatID: 10, MessageID: 100}
	later := db.SummaryRequest{ID: 2, MediaItemID: media.ID, ChatID: 10, MessageID: 101}
	repo.firstByMediaChat[progressMediaChat{mediaID: media.ID, chatID: 10}] = owner.ID

	if err := notifier.InitialRequestStatus(ctx, SummaryCommandResult{Media: media, Request: owner, CreatedTranscriptionJob: true}); err != nil {
		t.Fatalf("owner InitialRequestStatus returned error: %v", err)
	}
	if err := notifier.InitialRequestStatus(ctx, SummaryCommandResult{Media: media, Request: later}); err != nil {
		t.Fatalf("later InitialRequestStatus returned error: %v", err)
	}

	if !slices.Equal(sender.replyTargets, []int64{100, 101}) {
		t.Fatalf("reply targets = %#v", sender.replyTargets)
	}
	if len(repo.messages) != 2 || repo.messages[0].Kind != RequestMessageKindProgress || repo.messages[1].Kind != RequestMessageKindNotice {
		t.Fatalf("messages = %#v", repo.messages)
	}
}

func TestProgressNotifierInitialRequestStatusSkipsWatchRequests(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.InitialRequestStatus(ctx, SummaryCommandResult{Media: db.MediaItem{ID: 1}, Request: db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 0}}); err != nil {
		t.Fatalf("InitialRequestStatus returned error: %v", err)
	}
	if len(sender.texts) != 0 || len(repo.messages) != 0 {
		t.Fatalf("texts=%#v messages=%#v", sender.texts, repo.messages)
	}
}

func TestProgressNotifierMediaProgressReplacesPreviousProgress(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	owner := db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 100}
	repo.owners[1] = []db.SummaryRequest{owner}
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.MediaProgress(ctx, 1, "first"); err != nil {
		t.Fatalf("first MediaProgress returned error: %v", err)
	}
	if err := notifier.MediaProgress(ctx, 1, "second"); err != nil {
		t.Fatalf("second MediaProgress returned error: %v", err)
	}

	if !slices.Equal(sender.texts, []string{"first", "second"}) {
		t.Fatalf("texts = %#v", sender.texts)
	}
	if !slices.Equal(sender.deleted, []int64{1}) {
		t.Fatalf("deleted = %#v", sender.deleted)
	}
	if repo.messages[0].DeletedAt == "" || repo.messages[1].DeletedAt != "" {
		t.Fatalf("messages = %#v", repo.messages)
	}
}

func TestProgressNotifierFinalSummaryCleansIntermediateMessages(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 100}
	repo.messages = []db.SummaryRequestMessage{
		{ID: 1, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 11, Kind: RequestMessageKindProgress},
		{ID: 2, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 12, Kind: RequestMessageKindNotice},
	}
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalSummary(ctx, request, "summary"); err != nil {
		t.Fatalf("FinalSummary returned error: %v", err)
	}

	if !slices.Equal(sender.replyTargets, []int64{100}) || !slices.Equal(sender.texts, []string{"summary"}) {
		t.Fatalf("replies = %#v texts = %#v", sender.replyTargets, sender.texts)
	}
	if !slices.Equal(sender.deleted, []int64{11, 12}) {
		t.Fatalf("deleted = %#v", sender.deleted)
	}
	if repo.messages[0].DeletedAt == "" || repo.messages[1].DeletedAt == "" {
		t.Fatalf("messages = %#v", repo.messages)
	}
}

func TestProgressNotifierFinalSummaryUsesNormalMessageForWatchRequest(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 0}
	sender := &fakeFinalSummaryProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalSummary(ctx, request, "summary"); err != nil {
		t.Fatalf("FinalSummary returned error: %v", err)
	}
	if !slices.Equal(sender.finalReplyTargets, []int64{0}) || !slices.Equal(sender.finalSummaries, []string{"summary"}) {
		t.Fatalf("final summaries = %#v targets = %#v", sender.finalSummaries, sender.finalReplyTargets)
	}
	if len(sender.deleted) != 0 || len(repo.messages) != 1 || repo.messages[0].Kind != RequestMessageKindSummary {
		t.Fatalf("deleted=%#v messages=%#v", sender.deleted, repo.messages)
	}
}

func TestProgressNotifierFinalFailureStillRepliesForManualRequest(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, ChatID: 10, MessageID: 100}
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalFailureText(ctx, request, "failed"); err != nil {
		t.Fatalf("FinalFailureText returned error: %v", err)
	}
	if !slices.Equal(sender.replyTargets, []int64{100}) || !slices.Equal(sender.texts, []string{"failed"}) {
		t.Fatalf("reply targets=%#v texts=%#v", sender.replyTargets, sender.texts)
	}
}

func TestProgressNotifierFinalFailureUsesNormalMessageForWatchRequest(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, ChatID: 10, MessageID: 0}
	sender := &fakeProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalFailureText(ctx, request, "failed"); err != nil {
		t.Fatalf("FinalFailureText returned error: %v", err)
	}
	if !slices.Equal(sender.replyTargets, []int64{0}) || !slices.Equal(sender.texts, []string{"failed"}) {
		t.Fatalf("reply targets=%#v texts=%#v", sender.replyTargets, sender.texts)
	}
}

func TestProgressNotifierFinalSummaryUsesFinalSummarySender(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 100}
	repo.messages = []db.SummaryRequestMessage{{ID: 1, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 11, Kind: RequestMessageKindProgress}}
	sender := &fakeFinalSummaryProgressSender{}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalSummary(ctx, request, "summary"); err != nil {
		t.Fatalf("FinalSummary returned error: %v", err)
	}

	if !slices.Equal(sender.finalSummaries, []string{"summary"}) || !slices.Equal(sender.finalReplyTargets, []int64{100}) {
		t.Fatalf("final summaries = %#v targets = %#v", sender.finalSummaries, sender.finalReplyTargets)
	}
	if len(sender.texts) != 0 {
		t.Fatalf("plain texts = %#v", sender.texts)
	}
	if !slices.Equal(sender.deleted, []int64{11}) {
		t.Fatalf("deleted = %#v", sender.deleted)
	}
}

func TestProgressNotifierCleanupMarksMessagesAlreadyGone(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, ChatID: 10, MessageID: 100}
	repo.messages = []db.SummaryRequestMessage{{ID: 1, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 11, Kind: RequestMessageKindProgress}}
	sender := &fakeProgressSender{deleteErr: errors.New("Bad Request: message to delete not found")}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	notifier.CleanupRequests(ctx, []db.SummaryRequest{request})

	if repo.messages[0].DeletedAt == "" {
		t.Fatalf("message was not marked deleted: %#v", repo.messages)
	}
}

func TestProgressNotifierReplacesProgressAlreadyGone(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, MediaItemID: 1, ChatID: 10, MessageID: 100}
	repo.owners[1] = []db.SummaryRequest{request}
	repo.messages = []db.SummaryRequestMessage{{ID: 1, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 11, Kind: RequestMessageKindProgress}}
	sender := &fakeProgressSender{deleteErr: errors.New("Bad Request: message to delete not found")}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.MediaProgress(ctx, 1, "next"); err != nil {
		t.Fatalf("MediaProgress returned error: %v", err)
	}
	if repo.messages[0].DeletedAt == "" || len(repo.messages) != 2 {
		t.Fatalf("messages = %#v", repo.messages)
	}
}

func TestProgressNotifierDoesNotCleanupIfFinalSummaryFails(t *testing.T) {
	ctx := context.Background()
	repo := newProgressRepo()
	request := db.SummaryRequest{ID: 1, ChatID: 10, MessageID: 100}
	repo.messages = []db.SummaryRequestMessage{{ID: 1, SummaryRequestID: request.ID, ChatID: 10, TelegramMessageID: 11, Kind: RequestMessageKindProgress}}
	sender := &fakeProgressSender{sendErr: errors.New("telegram failed")}
	notifier := ProgressNotifier{Repo: repo, Sender: sender}

	if err := notifier.FinalSummary(ctx, request, "summary"); err == nil {
		t.Fatal("FinalSummary returned nil error")
	}
	if len(sender.deleted) != 0 || repo.messages[0].DeletedAt != "" {
		t.Fatalf("deleted = %#v messages = %#v", sender.deleted, repo.messages)
	}
}

type progressMediaChat struct {
	mediaID int64
	chatID  int64
}

type progressRepo struct {
	firstByMediaChat map[progressMediaChat]int64
	owners           map[int64][]db.SummaryRequest
	messages         []db.SummaryRequestMessage
	nextMessageID    int64
}

func newProgressRepo() *progressRepo {
	return &progressRepo{
		firstByMediaChat: map[progressMediaChat]int64{},
		owners:           map[int64][]db.SummaryRequest{},
		nextMessageID:    1,
	}
}

func (r *progressRepo) IsFirstRequestForMediaChat(_ context.Context, requestID, mediaItemID, chatID int64) (bool, error) {
	return r.firstByMediaChat[progressMediaChat{mediaID: mediaItemID, chatID: chatID}] == requestID, nil
}

func (r *progressRepo) ListProgressOwnerRequestsForMedia(_ context.Context, mediaItemID int64) ([]db.SummaryRequest, error) {
	return r.owners[mediaItemID], nil
}

func (r *progressRepo) CreateSummaryRequestMessage(_ context.Context, message db.SummaryRequestMessage) (db.SummaryRequestMessage, error) {
	message.ID = r.nextMessageID
	r.nextMessageID++
	r.messages = append(r.messages, message)
	return message, nil
}

func (r *progressRepo) LatestActiveSummaryRequestMessage(_ context.Context, requestID int64, kind string) (db.SummaryRequestMessage, bool, error) {
	for i := len(r.messages) - 1; i >= 0; i-- {
		message := r.messages[i]
		if message.SummaryRequestID == requestID && message.Kind == kind && message.DeletedAt == "" {
			return message, true, nil
		}
	}
	return db.SummaryRequestMessage{}, false, nil
}

func (r *progressRepo) ListActiveSummaryRequestMessages(_ context.Context, requestIDs []int64) ([]db.SummaryRequestMessage, error) {
	requestSet := map[int64]bool{}
	for _, id := range requestIDs {
		requestSet[id] = true
	}
	var messages []db.SummaryRequestMessage
	for _, message := range r.messages {
		if requestSet[message.SummaryRequestID] && message.DeletedAt == "" {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (r *progressRepo) MarkSummaryRequestMessageDeleted(_ context.Context, id int64) error {
	return r.MarkSummaryRequestMessagesDeleted(context.Background(), []int64{id})
}

func (r *progressRepo) MarkSummaryRequestMessagesDeleted(_ context.Context, ids []int64) error {
	idSet := map[int64]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	for i := range r.messages {
		if idSet[r.messages[i].ID] {
			r.messages[i].DeletedAt = "now"
		}
	}
	return nil
}

type fakeProgressSender struct {
	nextMessageID int64
	sendErr       error
	deleteErr     error
	texts         []string
	replyTargets  []int64
	deleted       []int64
}

func (f *fakeProgressSender) SendReplyText(_ context.Context, _ int64, replyToMessageID int64, text string) (int64, error) {
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.nextMessageID++
	f.texts = append(f.texts, text)
	f.replyTargets = append(f.replyTargets, replyToMessageID)
	return f.nextMessageID, nil
}

func (f *fakeProgressSender) DeleteMessage(_ context.Context, _ int64, messageID int64) error {
	f.deleted = append(f.deleted, messageID)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

type fakeFinalSummaryProgressSender struct {
	fakeProgressSender
	finalSummaries    []string
	finalReplyTargets []int64
}

func (f *fakeFinalSummaryProgressSender) SendFinalSummary(_ context.Context, _ int64, replyToMessageID int64, text string, _ ...any) (int64, error) {
	if f.sendErr != nil {
		return 0, f.sendErr
	}
	f.nextMessageID++
	f.finalSummaries = append(f.finalSummaries, text)
	f.finalReplyTargets = append(f.finalReplyTargets, replyToMessageID)
	return f.nextMessageID, nil
}
