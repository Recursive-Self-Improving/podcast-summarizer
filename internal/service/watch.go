package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/display"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
)

type WatchRepository interface {
	CreateOrLoadWatchFeed(ctx context.Context, feed db.WatchFeed) (db.WatchFeed, bool, error)
	UpsertWatchSubscription(ctx context.Context, sub db.WatchSubscription) (db.WatchSubscription, error)
	RemoveWatchSubscription(ctx context.Context, provider, providerFeedID string, chatID int64) (bool, error)
	ListWatchSubscriptionsForChat(ctx context.Context, chatID int64) ([]db.WatchSubscription, error)
	ListActiveWatchFeeds(ctx context.Context) ([]db.WatchFeed, error)
	ListWatchSubscriptionsForFeed(ctx context.Context, feedID int64) ([]db.WatchSubscription, error)
	InsertBaselineWatchEpisodes(ctx context.Context, feedID int64, episodes []db.WatchEpisode) (int64, error)
	FindWatchEpisode(ctx context.Context, feedID int64, providerEpisodeID string) (db.WatchEpisode, bool, error)
	InsertWatchEpisodeQueued(ctx context.Context, episode db.WatchEpisode, mediaItemID int64) (db.WatchEpisode, bool, error)
	InsertWatchEpisodeSkipped(ctx context.Context, episode db.WatchEpisode) (db.WatchEpisode, bool, error)
	BackfillWatchFeedTitle(ctx context.Context, feedID int64, title string) (db.WatchFeed, error)
	MarkWatchEpisodeFailed(ctx context.Context, episodeID int64, message string) error
	MarkWatchFeedChecked(ctx context.Context, feedID int64) error
	MarkWatchFeedError(ctx context.Context, feedID int64, message string) error
}

type WatchPodcastRegistry interface {
	Find(rawURL string) (provider.PodcastProvider, provider.PodcastRef, error)
}

type WatchSummaryQueue interface {
	RequestWatchSummary(ctx context.Context, command WatchSummaryCommand) (WatchSummaryResult, error)
}

type WatchService struct {
	Repo     WatchRepository
	Registry WatchPodcastRegistry
	Summary  WatchSummaryQueue
	Logger   *slog.Logger
}

type WatchSubscribeCommand struct {
	RawURL          string
	ChatID          int64
	ChatType        string
	ChatTitle       string
	CreatedByUserID int64
}

type WatchUnsubscribeCommand struct {
	RawURL string
	ChatID int64
}

type WatchResponse struct {
	Text string
}

func (s WatchService) Subscribe(ctx context.Context, command WatchSubscribeCommand) (WatchResponse, error) {
	podcastProvider, ref, err := s.findPodcast(command.RawURL)
	if err != nil {
		if errors.Is(err, provider.ErrInvalidURL) {
			return WatchResponse{Text: invalidWatchURLText()}, nil
		}
		return WatchResponse{}, err
	}
	feed, createdFeed, err := s.Repo.CreateOrLoadWatchFeed(ctx, db.WatchFeed{Provider: ref.Provider, ProviderFeedID: ref.ProviderFeedID, CanonicalURL: ref.CanonicalURL, Title: ref.Title, Status: "active"})
	if err != nil {
		return WatchResponse{}, err
	}
	baselined := createdFeed || feed.LastCheckedAt == ""
	if baselined {
		fetched, err := podcastProvider.FetchEpisodes(ctx, ref)
		if err != nil {
			_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
			return WatchResponse{}, err
		}
		feed, err = s.backfillFeedTitle(ctx, feed, fetched.Podcast.Title)
		if err != nil {
			return WatchResponse{}, err
		}
		if _, err := s.Repo.InsertBaselineWatchEpisodes(ctx, feed.ID, watchEpisodesFromRefs(feed.ID, fetched.Episodes, "baseline")); err != nil {
			return WatchResponse{}, err
		}
		if err := s.Repo.MarkWatchFeedChecked(ctx, feed.ID); err != nil {
			return WatchResponse{}, err
		}
	} else {
		feed, err = s.backfillFeedTitle(ctx, feed, ref.Title)
		if err != nil {
			return WatchResponse{}, err
		}
	}
	if _, err := s.Repo.UpsertWatchSubscription(ctx, db.WatchSubscription{FeedID: feed.ID, ChatID: command.ChatID, ChatType: command.ChatType, ChatTitle: command.ChatTitle, CreatedByUserID: command.CreatedByUserID}); err != nil {
		return WatchResponse{}, err
	}
	return WatchResponse{Text: subscribeSuccessText(feed, baselined)}, nil
}

func (s WatchService) Unsubscribe(ctx context.Context, command WatchUnsubscribeCommand) (WatchResponse, error) {
	_, ref, err := s.findPodcast(command.RawURL)
	if err != nil {
		if errors.Is(err, provider.ErrInvalidURL) {
			return WatchResponse{Text: invalidWatchURLText()}, nil
		}
		return WatchResponse{}, err
	}
	removed, err := s.Repo.RemoveWatchSubscription(ctx, ref.Provider, ref.ProviderFeedID, command.ChatID)
	if err != nil {
		return WatchResponse{}, err
	}
	if !removed {
		return WatchResponse{Text: "No subscription found for this chat."}, nil
	}
	return WatchResponse{Text: "Subscription removed for this chat."}, nil
}

func (s WatchService) ListSubscriptions(ctx context.Context, chatID int64) (WatchResponse, error) {
	subs, err := s.Repo.ListWatchSubscriptionsForChat(ctx, chatID)
	if err != nil {
		return WatchResponse{}, err
	}
	if len(subs) == 0 {
		return WatchResponse{Text: "No podcast subscriptions for this chat."}, nil
	}
	lines := []string{"Podcast subscriptions for this chat:"}
	for _, sub := range subs {
		lines = append(lines, fmt.Sprintf("- %s: %s — %s", providerDisplayName(sub.Feed.Provider), watchFeedDisplay(sub.Feed), sub.Feed.CanonicalURL))
	}
	return WatchResponse{Text: strings.Join(lines, "\n")}, nil
}

func (s WatchService) CheckFeedsOnce(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}
	feeds, err := s.Repo.ListActiveWatchFeeds(ctx)
	if err != nil {
		return err
	}
	var checkErr error
	for _, feed := range feeds {
		if err := s.checkFeed(ctx, feed); err != nil {
			checkErr = errors.Join(checkErr, err)
		}
	}
	return checkErr
}

func (s WatchService) checkFeed(ctx context.Context, feed db.WatchFeed) error {
	podcastProvider, ref, err := s.providerForFeed(feed)
	if err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	fetched, err := podcastProvider.FetchEpisodes(ctx, ref)
	if err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	feed, err = s.backfillFeedTitle(ctx, feed, fetched.Podcast.Title)
	if err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	unseen, err := s.unseenEpisodes(ctx, feed.ID, fetched.Episodes)
	if err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	if len(unseen) == 0 {
		return s.Repo.MarkWatchFeedChecked(ctx, feed.ID)
	}
	subscribers, err := s.Repo.ListWatchSubscriptionsForFeed(ctx, feed.ID)
	if err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	if len(subscribers) == 0 {
		return s.Repo.MarkWatchFeedChecked(ctx, feed.ID)
	}
	for _, episode := range unseen[1:] {
		if _, _, err := s.Repo.InsertWatchEpisodeSkipped(ctx, watchEpisodeFromRef(feed.ID, episode, "skipped")); err != nil {
			_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
			return err
		}
	}
	newest := unseen[0]
	result, err := s.Summary.RequestWatchSummary(ctx, WatchSummaryCommand{
		Provider:        newest.Provider,
		ProviderMediaID: newest.ProviderMediaID,
		CanonicalURL:    newest.CanonicalURL,
		Subscribers:     watchSubscribers(subscribers),
		Metadata: display.SummaryMetadata{
			PodcastTitle: watchFeedDisplay(feed),
			PodcastURL:   feed.CanonicalURL,
			EpisodeTitle: newest.Title,
			PubDate:      formatEpisodePubDate(newest.PubDate),
			Link:         newest.CanonicalURL,
		},
	})
	if err != nil {
		if result.Media.ID != 0 && IsDurableSummaryDeliveryError(err) {
			if _, _, insertErr := s.Repo.InsertWatchEpisodeQueued(ctx, watchEpisodeFromRef(feed.ID, newest, "queued"), result.Media.ID); insertErr != nil {
				_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(insertErr))
				return insertErr
			}
			_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
			return err
		}
		failedEpisode, _, insertErr := s.Repo.InsertWatchEpisodeSkipped(ctx, watchEpisodeFromRef(feed.ID, newest, "skipped"))
		if insertErr == nil && failedEpisode.ID != 0 {
			_ = s.Repo.MarkWatchEpisodeFailed(ctx, failedEpisode.ID, shortError(err))
		}
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	if _, _, err := s.Repo.InsertWatchEpisodeQueued(ctx, watchEpisodeFromRef(feed.ID, newest, "queued"), result.Media.ID); err != nil {
		_ = s.Repo.MarkWatchFeedError(ctx, feed.ID, shortError(err))
		return err
	}
	return s.Repo.MarkWatchFeedChecked(ctx, feed.ID)
}

func (s WatchService) unseenEpisodes(ctx context.Context, feedID int64, episodes []provider.EpisodeRef) ([]provider.EpisodeRef, error) {
	unseen := make([]provider.EpisodeRef, 0, len(episodes))
	for _, episode := range episodes {
		if episode.ProviderEpisodeID == "" {
			continue
		}
		_, found, err := s.Repo.FindWatchEpisode(ctx, feedID, episode.ProviderEpisodeID)
		if err != nil {
			return nil, err
		}
		if !found {
			unseen = append(unseen, episode)
		}
	}
	return unseen, nil
}

func (s WatchService) providerForFeed(feed db.WatchFeed) (provider.PodcastProvider, provider.PodcastRef, error) {
	podcastProvider, ref, err := s.findPodcast(feed.CanonicalURL)
	if err != nil {
		return nil, provider.PodcastRef{}, err
	}
	if ref.Provider != feed.Provider || ref.ProviderFeedID != feed.ProviderFeedID {
		return nil, provider.PodcastRef{}, fmt.Errorf("watch feed provider mismatch")
	}
	if strings.TrimSpace(feed.Title) != "" {
		ref.Title = feed.Title
	}
	return podcastProvider, ref, nil
}

func (s WatchService) backfillFeedTitle(ctx context.Context, feed db.WatchFeed, title string) (db.WatchFeed, error) {
	title = strings.TrimSpace(title)
	if strings.TrimSpace(feed.Title) != "" || title == "" {
		return feed, nil
	}
	updated, err := s.Repo.BackfillWatchFeedTitle(ctx, feed.ID, title)
	if err != nil {
		return db.WatchFeed{}, err
	}
	return updated, nil
}

func (s WatchService) findPodcast(rawURL string) (provider.PodcastProvider, provider.PodcastRef, error) {
	if err := s.validate(); err != nil {
		return nil, provider.PodcastRef{}, err
	}
	return s.Registry.Find(rawURL)
}

func (s WatchService) validate() error {
	if s.Repo == nil {
		return errors.New("watch repository is required")
	}
	if s.Registry == nil {
		return errors.New("watch podcast registry is required")
	}
	if s.Summary == nil {
		return errors.New("watch summary queue is required")
	}
	return nil
}

func watchEpisodesFromRefs(feedID int64, episodes []provider.EpisodeRef, status string) []db.WatchEpisode {
	watchEpisodes := make([]db.WatchEpisode, 0, len(episodes))
	for _, episode := range episodes {
		watchEpisodes = append(watchEpisodes, watchEpisodeFromRef(feedID, episode, status))
	}
	return watchEpisodes
}

func watchEpisodeFromRef(feedID int64, episode provider.EpisodeRef, status string) db.WatchEpisode {
	return db.WatchEpisode{
		FeedID:            feedID,
		ProviderEpisodeID: episode.ProviderEpisodeID,
		CanonicalURL:      episode.CanonicalURL,
		Title:             episode.Title,
		PubDate:           formatEpisodePubDate(episode.PubDate),
		Status:            status,
	}
}

func watchSubscribers(subs []db.WatchSubscription) []WatchSummarySubscriber {
	subscribers := make([]WatchSummarySubscriber, 0, len(subs))
	for _, sub := range subs {
		subscribers = append(subscribers, WatchSummarySubscriber{ChatID: sub.ChatID, UserID: sub.CreatedByUserID})
	}
	return subscribers
}

func formatEpisodePubDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.DateTime)
}

func subscribeSuccessText(feed db.WatchFeed, baselined bool) string {
	if baselined {
		return fmt.Sprintf("Subscribed to %s: %s\nURL: %s\nExisting episodes were baselined; only future episodes will be summarized.", providerDisplayName(feed.Provider), watchFeedDisplay(feed), feed.CanonicalURL)
	}
	return fmt.Sprintf("Subscribed to %s: %s\nURL: %s\nThis podcast was already baselined; only future episodes will be summarized.", providerDisplayName(feed.Provider), watchFeedDisplay(feed), feed.CanonicalURL)
}

func invalidWatchURLText() string {
	return "Please send a SoundOn or xiaoyuzhou podcast URL. Episode URLs and YouTube URLs are not supported for subscriptions."
}

func providerDisplayName(providerName string) string {
	switch providerName {
	case "soundon":
		return "SoundOn"
	case "xiaoyuzhou":
		return "xiaoyuzhou"
	default:
		return providerName
	}
}

func watchFeedDisplay(feed db.WatchFeed) string {
	if strings.TrimSpace(feed.Title) != "" {
		return feed.Title
	}
	return feed.CanonicalURL
}
