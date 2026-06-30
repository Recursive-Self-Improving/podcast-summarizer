package db

type WhitelistedGroup struct {
	ChatID          int64
	Title           string
	CreatedAt       string
	CreatedByUserID int64
}

type WhitelistedDMUser struct {
	UserID          int64
	Username        string
	FirstName       string
	CreatedAt       string
	CreatedByUserID int64
}

type MediaItem struct {
	ID               int64
	Provider         string
	ProviderMediaID  string
	CanonicalURL     string
	Title            string
	DurationSeconds  int64
	Status           string
	StatusDetail     string
	TranscriptSource string
	TranscriptText   string
	CreatedAt        string
	UpdatedAt        string
}

type TranscriptionJob struct {
	ID           int64
	MediaItemID  int64
	Status       string
	AttemptCount int
	LastError    string
	CreatedAt    string
	StartedAt    string
	FinishedAt   string
}

type SummaryCache struct {
	ID          int64
	MediaItemID int64
	PromptHash  string
	PromptText  string
	SummaryText string
	Model       string
	CreatedAt   string
}

type SummaryRequest struct {
	ID             int64
	MediaItemID    int64
	ChatID         int64
	UserID         int64
	MessageID      int64
	PromptHash     string
	PromptText     string
	Status         string
	SummaryCacheID int64
	Error          string
	CreatedAt      string
	UpdatedAt      string
}

type SummaryRequestMessage struct {
	ID                int64
	SummaryRequestID  int64
	ChatID            int64
	TelegramMessageID int64
	Kind              string
	DeletedAt         string
	CreatedAt         string
}

type WatchFeed struct {
	ID             int64
	Provider       string
	ProviderFeedID string
	CanonicalURL   string
	Title          string
	Status         string
	LastCheckedAt  string
	LastError      string
	CreatedAt      string
	UpdatedAt      string
}

type WatchSubscription struct {
	ID              int64
	FeedID          int64
	ChatID          int64
	ChatType        string
	ChatTitle       string
	CreatedByUserID int64
	CreatedAt       string
	Feed            WatchFeed
}

type WatchEpisode struct {
	ID                int64
	FeedID            int64
	ProviderEpisodeID string
	MediaItemID       int64
	CanonicalURL      string
	Title             string
	PubDate           string
	Status            string
	FirstSeenAt       string
	ProcessedAt       string
	LastError         string
}
