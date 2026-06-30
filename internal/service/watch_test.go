package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
)

func TestWatchServiceSubscribeBaselinesAndLists(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Title: "Feed Title"}}}
	registry.providers = map[string]*fakePodcastProvider{"soundon": {episodes: []provider.EpisodeRef{watchEpisodeRef("ep1", time.Now())}}}
	summary := &fakeWatchSummaryQueue{}
	service := WatchService{Repo: repo, Registry: registry, Summary: summary}

	response, err := service.Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 10, ChatType: "group", ChatTitle: "Group", CreatedByUserID: 100})
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "Subscribed to SoundOn") || !strings.Contains(response.Text, "baselined") || !strings.Contains(response.Text, "podcast") {
		t.Fatalf("response = %q", response.Text)
	}
	if len(repo.episodes) != 1 || len(summary.commands) != 0 {
		t.Fatalf("episodes=%#v summary=%#v", repo.episodes, summary.commands)
	}
	list, err := service.ListSubscriptions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSubscriptions returned error: %v", err)
	}
	if !strings.Contains(list.Text, "Feed Title") || !strings.Contains(list.Text, "podcast") {
		t.Fatalf("list = %q", list.Text)
	}
	registry.providers["soundon"].episodes = append(registry.providers["soundon"].episodes, watchEpisodeRef("new-unseen", time.Now()))

	response, err = service.Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 10, ChatType: "supergroup", ChatTitle: "New Group", CreatedByUserID: 101})
	if err != nil {
		t.Fatalf("duplicate Subscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "already baselined") || len(repo.subs) != 1 || repo.subs[0].ChatTitle != "New Group" || len(repo.episodes) != 1 {
		t.Fatalf("subs=%#v episodes=%#v response=%q", repo.subs, repo.episodes, response.Text)
	}
	response, err = service.Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 11, ChatType: "private", CreatedByUserID: 102})
	if err != nil {
		t.Fatalf("new chat Subscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "already baselined") || len(repo.subs) != 2 || len(repo.episodes) != 1 {
		t.Fatalf("subs=%#v episodes=%#v response=%q", repo.subs, repo.episodes, response.Text)
	}
}

func TestWatchServiceSubscribeFailureDoesNotCreateSubscription(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		repo     *watchRepo
		provider *fakePodcastProvider
	}{
		{name: "fetch", repo: newWatchRepo(), provider: &fakePodcastProvider{err: errors.New("fetch failed")}},
		{name: "baseline", repo: func() *watchRepo {
			repo := newWatchRepo()
			repo.baselineErr = errors.New("baseline failed")
			return repo
		}(), provider: &fakePodcastProvider{episodes: []provider.EpisodeRef{watchEpisodeRef("ep1", time.Now())}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": tc.provider}}
			_, err := (WatchService{Repo: tc.repo, Registry: registry, Summary: &fakeWatchSummaryQueue{}}).Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 10, ChatType: "group", CreatedByUserID: 100})
			if err == nil {
				t.Fatal("Subscribe returned nil error")
			}
			if len(tc.repo.subs) != 0 {
				t.Fatalf("subs = %#v", tc.repo.subs)
			}
		})
	}
}

func TestWatchServiceSubscribeRetryBaselinesIncompleteFeed(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	providerFake := &fakePodcastProvider{err: errors.New("temporary fetch failed")}
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": providerFake}}
	service := WatchService{Repo: repo, Registry: registry, Summary: &fakeWatchSummaryQueue{}}

	if _, err := service.Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 10, ChatType: "group", CreatedByUserID: 100}); err == nil {
		t.Fatal("first Subscribe returned nil error")
	}
	if len(repo.feeds) != 1 || repo.feeds[1].LastCheckedAt != "" || len(repo.subs) != 0 {
		t.Fatalf("feeds=%#v subs=%#v", repo.feeds, repo.subs)
	}
	providerFake.err = nil
	providerFake.episodes = []provider.EpisodeRef{watchEpisodeRef("historical", time.Now())}
	response, err := service.Subscribe(ctx, WatchSubscribeCommand{RawURL: "podcast", ChatID: 10, ChatType: "group", CreatedByUserID: 100})
	if err != nil {
		t.Fatalf("retry Subscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "baselined") || len(repo.subs) != 1 || len(repo.episodes) != 1 || repo.feeds[1].LastCheckedAt == "" {
		t.Fatalf("response=%q feeds=%#v subs=%#v episodes=%#v", response.Text, repo.feeds, repo.subs, repo.episodes)
	}
}

func TestWatchServiceListScopesCurrentChat(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	first := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed-1", CanonicalURL: "podcast-1", Title: "First", Status: "active"})
	second := repo.addFeed(db.WatchFeed{Provider: "xiaoyuzhou", ProviderFeedID: "feed-2", CanonicalURL: "podcast-2", Title: "Second", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: first.ID, ChatID: 10})
	repo.addSub(db.WatchSubscription{FeedID: second.ID, ChatID: 11})
	service := WatchService{Repo: repo, Registry: fakePodcastRegistry{}, Summary: &fakeWatchSummaryQueue{}}

	response, err := service.ListSubscriptions(ctx, 10)
	if err != nil {
		t.Fatalf("ListSubscriptions returned error: %v", err)
	}
	if !strings.Contains(response.Text, "First") || strings.Contains(response.Text, "Second") {
		t.Fatalf("response = %q", response.Text)
	}
}

func TestWatchServiceUnsubscribeScopesCurrentChat(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}}
	registry.providers = map[string]*fakePodcastProvider{"soundon": {}}
	service := WatchService{Repo: repo, Registry: registry, Summary: &fakeWatchSummaryQueue{}}
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 10})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 11})

	response, err := service.Unsubscribe(ctx, WatchUnsubscribeCommand{RawURL: "podcast", ChatID: 10})
	if err != nil {
		t.Fatalf("Unsubscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "removed") || len(repo.subs) != 1 || repo.subs[0].ChatID != 11 {
		t.Fatalf("response=%q subs=%#v", response.Text, repo.subs)
	}
	response, err = service.Unsubscribe(ctx, WatchUnsubscribeCommand{RawURL: "podcast", ChatID: 10})
	if err != nil {
		t.Fatalf("second Unsubscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "No subscription") {
		t.Fatalf("response = %q", response.Text)
	}
}

func TestWatchServiceInvalidURLResponses(t *testing.T) {
	service := WatchService{Repo: newWatchRepo(), Registry: fakePodcastRegistry{}, Summary: &fakeWatchSummaryQueue{}}
	response, err := service.Subscribe(context.Background(), WatchSubscribeCommand{RawURL: "youtube", ChatID: 10})
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if !strings.Contains(response.Text, "SoundOn") || !strings.Contains(response.Text, "xiaoyuzhou") {
		t.Fatalf("response = %q", response.Text)
	}
}

func TestWatchServiceCheckFeedsNoUnseenUpdatesChecked(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 10})
	existing := watchEpisodeRef("ep1", time.Now())
	repo.episodes[watchEpisodeKey{feedID: feed.ID, providerEpisodeID: "ep1"}] = db.WatchEpisode{ID: 1, FeedID: feed.ID, ProviderEpisodeID: "ep1", Status: "baseline"}
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": {episodes: []provider.EpisodeRef{existing}}}}
	summary := &fakeWatchSummaryQueue{}

	if err := (WatchService{Repo: repo, Registry: registry, Summary: summary}).CheckFeedsOnce(ctx); err != nil {
		t.Fatalf("CheckFeedsOnce returned error: %v", err)
	}
	if repo.feeds[feed.ID].LastCheckedAt == "" || len(summary.commands) != 0 {
		t.Fatalf("feed=%#v commands=%#v", repo.feeds[feed.ID], summary.commands)
	}
}

func TestWatchServiceCheckFeedsWithNoSubscribersDoesNotRecordEpisodes(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.activeFeeds = []db.WatchFeed{feed}
	episodes := []provider.EpisodeRef{watchEpisodeRef("new", time.Now()), watchEpisodeRef("old", time.Now().Add(-time.Hour))}
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": {episodes: episodes}}}
	summary := &fakeWatchSummaryQueue{}

	if err := (WatchService{Repo: repo, Registry: registry, Summary: summary}).CheckFeedsOnce(ctx); err != nil {
		t.Fatalf("CheckFeedsOnce returned error: %v", err)
	}
	if len(repo.episodes) != 0 || len(summary.commands) != 0 || repo.feeds[feed.ID].LastCheckedAt == "" {
		t.Fatalf("episodes=%#v commands=%#v feed=%#v", repo.episodes, summary.commands, repo.feeds[feed.ID])
	}
}

func TestWatchServiceCheckFeedsQueuesNewestAndSkipsOlder(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 10, CreatedByUserID: 100})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 11, CreatedByUserID: 101})
	episodes := []provider.EpisodeRef{watchEpisodeRef("new", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)), watchEpisodeRef("old", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": {title: "Fetched Podcast", episodes: episodes}}}
	summary := &fakeWatchSummaryQueue{mediaID: 55}

	if err := (WatchService{Repo: repo, Registry: registry, Summary: summary}).CheckFeedsOnce(ctx); err != nil {
		t.Fatalf("CheckFeedsOnce returned error: %v", err)
	}
	if len(summary.commands) != 1 || summary.commands[0].ProviderMediaID != "media-new" || len(summary.commands[0].Subscribers) != 2 {
		t.Fatalf("commands = %#v", summary.commands)
	}
	metadata := summary.commands[0].Metadata
	if metadata.PodcastTitle != "Fetched Podcast" || metadata.PodcastURL != "podcast" || metadata.EpisodeTitle != "Episode new" || metadata.PubDate != "2026-01-02 00:00:00" || metadata.Link != "episode-new" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if repo.feeds[feed.ID].Title != "Fetched Podcast" {
		t.Fatalf("feed title = %q", repo.feeds[feed.ID].Title)
	}
	queued := repo.episodes[watchEpisodeKey{feedID: feed.ID, providerEpisodeID: "new"}]
	skipped := repo.episodes[watchEpisodeKey{feedID: feed.ID, providerEpisodeID: "old"}]
	if queued.Status != "queued" || queued.MediaItemID != 55 || skipped.Status != "skipped" {
		t.Fatalf("queued=%#v skipped=%#v", queued, skipped)
	}
	if repo.feeds[feed.ID].LastCheckedAt == "" || repo.feeds[feed.ID].LastError != "" {
		t.Fatalf("feed = %#v", repo.feeds[feed.ID])
	}
}

func TestWatchServiceCheckFeedsRecordsSummaryError(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 10, CreatedByUserID: 100})
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": {episodes: []provider.EpisodeRef{watchEpisodeRef("new", time.Now())}}}}
	summary := &fakeWatchSummaryQueue{err: errors.New("queue failed")}

	if err := (WatchService{Repo: repo, Registry: registry, Summary: summary}).CheckFeedsOnce(ctx); err == nil {
		t.Fatal("CheckFeedsOnce returned nil error")
	}
	episode := repo.episodes[watchEpisodeKey{feedID: feed.ID, providerEpisodeID: "new"}]
	if episode.Status != "failed" || repo.feeds[feed.ID].LastError == "" {
		t.Fatalf("episode=%#v feed=%#v", episode, repo.feeds[feed.ID])
	}
}

func TestWatchServiceCheckFeedsRecordsProviderError(t *testing.T) {
	ctx := context.Background()
	repo := newWatchRepo()
	feed := repo.addFeed(db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	repo.addSub(db.WatchSubscription{FeedID: feed.ID, ChatID: 10})
	registry := fakePodcastRegistry{refs: map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}}, providers: map[string]*fakePodcastProvider{"soundon": {err: errors.New("fetch failed")}}}

	if err := (WatchService{Repo: repo, Registry: registry, Summary: &fakeWatchSummaryQueue{}}).CheckFeedsOnce(ctx); err == nil {
		t.Fatal("CheckFeedsOnce returned nil error")
	}
	if repo.feeds[feed.ID].LastError == "" || len(repo.episodes) != 0 {
		t.Fatalf("feed=%#v episodes=%#v", repo.feeds[feed.ID], repo.episodes)
	}
}

func TestWatchServiceListEmpty(t *testing.T) {
	response, err := (WatchService{Repo: newWatchRepo(), Registry: fakePodcastRegistry{}, Summary: &fakeWatchSummaryQueue{}}).ListSubscriptions(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSubscriptions returned error: %v", err)
	}
	if !strings.Contains(response.Text, "No podcast subscriptions") {
		t.Fatalf("response = %q", response.Text)
	}
}

func TestWatchServiceRepositoryPreventsDuplicateWork(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(ctx, database); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}
	repo := db.NewRepository(database)
	feed, _, err := repo.CreateOrLoadWatchFeed(ctx, db.WatchFeed{Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast", Status: "active"})
	if err != nil {
		t.Fatalf("CreateOrLoadWatchFeed returned error: %v", err)
	}
	for _, sub := range []db.WatchSubscription{
		{FeedID: feed.ID, ChatID: 10, ChatType: "group", CreatedByUserID: 100},
		{FeedID: feed.ID, ChatID: 11, ChatType: "private", CreatedByUserID: 101},
	} {
		if _, err := repo.UpsertWatchSubscription(ctx, sub); err != nil {
			t.Fatalf("UpsertWatchSubscription returned error: %v", err)
		}
	}
	episode := watchEpisodeRef("new", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	podcastProvider := &fakePodcastProvider{episodes: []provider.EpisodeRef{episode}}
	registry := fakePodcastRegistry{
		refs:      map[string]provider.PodcastRef{"podcast": {Provider: "soundon", ProviderFeedID: "feed", CanonicalURL: "podcast"}},
		providers: map[string]*fakePodcastProvider{"soundon": podcastProvider},
	}
	watchService := WatchService{Repo: repo, Registry: registry, Summary: SummaryService{Repo: repo, Model: "model"}}

	if err := watchService.CheckFeedsOnce(ctx); err != nil {
		t.Fatalf("CheckFeedsOnce returned error: %v", err)
	}
	assertDuplicateWorkCounts(t, database)

	if err := watchService.CheckFeedsOnce(ctx); err != nil {
		t.Fatalf("second CheckFeedsOnce returned error: %v", err)
	}
	assertDuplicateWorkCounts(t, database)
	if podcastProvider.fetchCount != 2 {
		t.Fatalf("fetchCount = %d", podcastProvider.fetchCount)
	}
}

func assertDuplicateWorkCounts(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, tc := range []struct {
		name  string
		query string
		want  int64
	}{
		{name: "media", query: `SELECT COUNT(*) FROM media_items`, want: 1},
		{name: "transcription jobs", query: `SELECT COUNT(*) FROM transcription_jobs`, want: 1},
		{name: "summary requests", query: `SELECT COUNT(*) FROM summary_requests`, want: 2},
		{name: "watch episodes", query: `SELECT COUNT(*) FROM watch_episodes WHERE status = 'queued'`, want: 1},
	} {
		var got int64
		if err := database.QueryRowContext(context.Background(), tc.query).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s count = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func watchEpisodeRef(id string, pubDate time.Time) provider.EpisodeRef {
	return provider.EpisodeRef{Provider: "soundon", ProviderEpisodeID: id, ProviderMediaID: "media-" + id, CanonicalURL: "episode-" + id, Title: "Episode " + id, PubDate: pubDate}
}

type fakePodcastRegistry struct {
	refs      map[string]provider.PodcastRef
	providers map[string]*fakePodcastProvider
}

func (r fakePodcastRegistry) Find(rawURL string) (provider.PodcastProvider, provider.PodcastRef, error) {
	ref, ok := r.refs[rawURL]
	if !ok {
		return nil, provider.PodcastRef{}, provider.ErrInvalidURL
	}
	podcastProvider := r.providers[ref.Provider]
	if podcastProvider == nil {
		podcastProvider = &fakePodcastProvider{}
	}
	podcastProvider.name = ref.Provider
	return podcastProvider, ref, nil
}

type fakePodcastProvider struct {
	name       string
	title      string
	episodes   []provider.EpisodeRef
	err        error
	fetchCount int
}

func (p *fakePodcastProvider) Name() string             { return p.name }
func (p *fakePodcastProvider) MatchPodcast(string) bool { return false }
func (p *fakePodcastProvider) NormalizePodcast(string) (provider.PodcastRef, error) {
	return provider.PodcastRef{}, nil
}
func (p *fakePodcastProvider) FetchEpisodes(_ context.Context, ref provider.PodcastRef) (provider.PodcastEpisodes, error) {
	p.fetchCount++
	if p.err != nil {
		return provider.PodcastEpisodes{}, p.err
	}
	ref.Title = p.title
	return provider.PodcastEpisodes{Podcast: ref, Episodes: append([]provider.EpisodeRef(nil), p.episodes...)}, nil
}

type fakeWatchSummaryQueue struct {
	mediaID  int64
	err      error
	commands []WatchSummaryCommand
}

func (q *fakeWatchSummaryQueue) RequestWatchSummary(_ context.Context, command WatchSummaryCommand) (WatchSummaryResult, error) {
	q.commands = append(q.commands, command)
	if q.err != nil {
		return WatchSummaryResult{}, q.err
	}
	mediaID := q.mediaID
	if mediaID == 0 {
		mediaID = int64(len(q.commands))
	}
	return WatchSummaryResult{Media: db.MediaItem{ID: mediaID, Provider: command.Provider, ProviderMediaID: command.ProviderMediaID, CanonicalURL: command.CanonicalURL}}, nil
}

type watchEpisodeKey struct {
	feedID            int64
	providerEpisodeID string
}

type watchRepo struct {
	nextFeedID    int64
	nextSubID     int64
	nextEpisodeID int64
	feeds         map[int64]db.WatchFeed
	feedsByKey    map[string]int64
	subs          []db.WatchSubscription
	episodes      map[watchEpisodeKey]db.WatchEpisode
	baselineErr   error
	activeFeeds   []db.WatchFeed
}

func newWatchRepo() *watchRepo {
	return &watchRepo{nextFeedID: 1, nextSubID: 1, nextEpisodeID: 1, feeds: map[int64]db.WatchFeed{}, feedsByKey: map[string]int64{}, episodes: map[watchEpisodeKey]db.WatchEpisode{}}
}

func (r *watchRepo) addFeed(feed db.WatchFeed) db.WatchFeed {
	feed.ID = r.nextFeedID
	r.nextFeedID++
	if feed.Status == "" {
		feed.Status = "active"
	}
	r.feeds[feed.ID] = feed
	r.feedsByKey[feed.Provider+":"+feed.ProviderFeedID] = feed.ID
	return feed
}

func (r *watchRepo) addSub(sub db.WatchSubscription) db.WatchSubscription {
	sub.ID = r.nextSubID
	r.nextSubID++
	r.subs = append(r.subs, sub)
	return sub
}

func (r *watchRepo) CreateOrLoadWatchFeed(_ context.Context, feed db.WatchFeed) (db.WatchFeed, bool, error) {
	key := feed.Provider + ":" + feed.ProviderFeedID
	if id, ok := r.feedsByKey[key]; ok {
		return r.feeds[id], false, nil
	}
	return r.addFeed(feed), true, nil
}

func (r *watchRepo) BackfillWatchFeedTitle(_ context.Context, feedID int64, title string) (db.WatchFeed, error) {
	feed := r.feeds[feedID]
	if strings.TrimSpace(feed.Title) == "" && strings.TrimSpace(title) != "" {
		feed.Title = strings.TrimSpace(title)
		r.feeds[feedID] = feed
		for i, activeFeed := range r.activeFeeds {
			if activeFeed.ID == feedID {
				r.activeFeeds[i] = feed
			}
		}
	}
	return feed, nil
}

func (r *watchRepo) UpsertWatchSubscription(_ context.Context, sub db.WatchSubscription) (db.WatchSubscription, error) {
	for i, existing := range r.subs {
		if existing.FeedID == sub.FeedID && existing.ChatID == sub.ChatID {
			sub.ID = existing.ID
			r.subs[i] = sub
			return sub, nil
		}
	}
	return r.addSub(sub), nil
}

func (r *watchRepo) RemoveWatchSubscription(_ context.Context, providerName, providerFeedID string, chatID int64) (bool, error) {
	feedID := r.feedsByKey[providerName+":"+providerFeedID]
	for i, sub := range r.subs {
		if sub.FeedID == feedID && sub.ChatID == chatID {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (r *watchRepo) ListWatchSubscriptionsForChat(_ context.Context, chatID int64) ([]db.WatchSubscription, error) {
	var subs []db.WatchSubscription
	for _, sub := range r.subs {
		feed := r.feeds[sub.FeedID]
		if sub.ChatID == chatID && feed.Status == "active" {
			sub.Feed = feed
			subs = append(subs, sub)
		}
	}
	return subs, nil
}

func (r *watchRepo) ListActiveWatchFeeds(context.Context) ([]db.WatchFeed, error) {
	if r.activeFeeds != nil {
		return append([]db.WatchFeed(nil), r.activeFeeds...), nil
	}
	var feeds []db.WatchFeed
	for _, feed := range r.feeds {
		if feed.Status != "active" {
			continue
		}
		if slices.ContainsFunc(r.subs, func(sub db.WatchSubscription) bool { return sub.FeedID == feed.ID }) {
			feeds = append(feeds, feed)
		}
	}
	return feeds, nil
}

func (r *watchRepo) ListWatchSubscriptionsForFeed(_ context.Context, feedID int64) ([]db.WatchSubscription, error) {
	var subs []db.WatchSubscription
	for _, sub := range r.subs {
		if sub.FeedID == feedID {
			subs = append(subs, sub)
		}
	}
	return subs, nil
}

func (r *watchRepo) InsertBaselineWatchEpisodes(_ context.Context, feedID int64, episodes []db.WatchEpisode) (int64, error) {
	if r.baselineErr != nil {
		return 0, r.baselineErr
	}
	var inserted int64
	for _, episode := range episodes {
		episode.FeedID = feedID
		episode.Status = "baseline"
		if r.insertEpisode(episode, 0) {
			inserted++
		}
	}
	return inserted, nil
}

func (r *watchRepo) FindWatchEpisode(_ context.Context, feedID int64, providerEpisodeID string) (db.WatchEpisode, bool, error) {
	episode, ok := r.episodes[watchEpisodeKey{feedID: feedID, providerEpisodeID: providerEpisodeID}]
	return episode, ok, nil
}

func (r *watchRepo) InsertWatchEpisodeQueued(_ context.Context, episode db.WatchEpisode, mediaItemID int64) (db.WatchEpisode, bool, error) {
	created := r.insertEpisode(episode, mediaItemID)
	stored := r.episodes[watchEpisodeKey{feedID: episode.FeedID, providerEpisodeID: episode.ProviderEpisodeID}]
	return stored, created, nil
}

func (r *watchRepo) InsertWatchEpisodeSkipped(_ context.Context, episode db.WatchEpisode) (db.WatchEpisode, bool, error) {
	created := r.insertEpisode(episode, 0)
	stored := r.episodes[watchEpisodeKey{feedID: episode.FeedID, providerEpisodeID: episode.ProviderEpisodeID}]
	return stored, created, nil
}

func (r *watchRepo) insertEpisode(episode db.WatchEpisode, mediaItemID int64) bool {
	key := watchEpisodeKey{feedID: episode.FeedID, providerEpisodeID: episode.ProviderEpisodeID}
	if _, ok := r.episodes[key]; ok {
		return false
	}
	episode.ID = r.nextEpisodeID
	r.nextEpisodeID++
	episode.MediaItemID = mediaItemID
	if episode.Status == "" {
		episode.Status = "queued"
	}
	r.episodes[key] = episode
	return true
}

func (r *watchRepo) MarkWatchEpisodeFailed(_ context.Context, episodeID int64, message string) error {
	for key, episode := range r.episodes {
		if episode.ID == episodeID {
			episode.Status = "failed"
			episode.LastError = message
			r.episodes[key] = episode
			return nil
		}
	}
	return db.ErrNotFound
}

func (r *watchRepo) MarkWatchFeedChecked(_ context.Context, feedID int64) error {
	feed := r.feeds[feedID]
	feed.LastCheckedAt = "checked"
	feed.LastError = ""
	r.feeds[feedID] = feed
	return nil
}

func (r *watchRepo) MarkWatchFeedError(_ context.Context, feedID int64, message string) error {
	feed := r.feeds[feedID]
	feed.LastError = message
	r.feeds[feedID] = feed
	return nil
}
