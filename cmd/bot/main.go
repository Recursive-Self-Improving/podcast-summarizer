package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	telegram "github.com/go-telegram/bot"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/auth"
	botpkg "github.com/Recursive-Self-Improving/podcast-summarizer/internal/bot"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/config"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/db"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/queue"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/service"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcribe"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/transcript"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	if cfg.TempRoot != "" {
		if err := os.MkdirAll(cfg.TempRoot, 0o755); err != nil {
			return fmt.Errorf("create temp root: %w", err)
		}
	}

	database, err := db.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.RunMigrations(ctx, database); err != nil {
		return err
	}

	repo := db.NewRepository(database)
	requeuedJobs, err := repo.RequeueInterruptedTranscriptionJobs(ctx)
	if err != nil {
		return err
	}
	if requeuedJobs > 0 {
		logger.Warn("requeued interrupted transcription jobs", "count", requeuedJobs)
	}
	authorizer := auth.NewService(cfg.BotOwnerID, repo)
	registry := provider.DefaultRegistry()
	replyPrompts := botpkg.NewMemoryReplyPromptStore()
	var handler botpkg.Handler

	options := []telegram.Option{
		telegram.WithSkipGetMe(),
		telegram.WithErrorsHandler(func(err error) { logger.Error("telegram error", "error", err) }),
		telegram.WithDefaultHandler(botpkg.DefaultHandler(&handler)),
	}
	if cfg.TelegramSkipOld {
		options = append(options, telegram.WithInitialOffset(-1))
	}
	telegramBot, err := telegram.New(cfg.TelegramBotToken, options...)
	if err != nil {
		return fmt.Errorf("initialize telegram bot: %w", err)
	}

	sender := botpkg.Sender{Client: botpkg.TelegramSenderClient{Bot: telegramBot}, TempDir: cfg.TempRoot, Logger: logger.With("component", "telegram_sender")}
	progressNotifier := service.ProgressNotifier{Repo: repo, Sender: sender, Logger: logger.With("component", "progress_notifier")}
	llm := summarize.NewOpenAIResponses(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel)
	summaryService := service.SummaryService{
		Repo:     repo,
		Registry: registry,
		SubtitleDownloader: transcript.Downloader{
			YTDLP:     cfg.YTDLPPath,
			YTDLPArgs: cfg.YTDLPArgs,
			TempRoot:  cfg.TempRoot,
			Logger:    logger.With("component", "subtitle_downloader"),
		},
		Summarizer: summarize.Service{Summarizer: llm},
		Sender:     sender,
		Progress:   progressNotifier,
		Model:      cfg.OpenAIModel,
		Logger:     logger.With("component", "summary_service"),
	}
	requeuedRequests, err := summaryService.RequeueInterruptedSummaryRequests(ctx)
	if err != nil {
		return err
	}
	if requeuedRequests > 0 {
		logger.Warn("requeued interrupted summary requests", "count", requeuedRequests)
	}
	statusService := service.StatusService{
		Auth:     authorizer,
		Registry: registry,
		Repo:     repo,
		Model:    cfg.OpenAIModel,
		Logger:   logger.With("component", "status_service"),
	}
	watchService := service.WatchService{
		Repo:     repo,
		Registry: provider.DefaultPodcastRegistry(),
		Summary:  summaryService,
		Logger:   logger.With("component", "watch_service"),
	}
	handler = botpkg.Handler{
		Auth:         authorizer,
		Summary:      summaryService,
		Status:       statusService,
		Watch:        watchService,
		Whitelist:    repo,
		Sender:       sender,
		Progress:     progressNotifier,
		ReplyPrompts: replyPrompts,
		Logger:       logger.With("component", "bot_handler"),
	}
	botpkg.RegisterHandlers(telegramBot, handler)

	worker := queue.Worker{
		Repo:   repo,
		Logger: logger.With("component", "queue_worker"),
		Processor: queue.TranscriptionProcessor{
			Repo: repo,
			Pipeline: transcribe.Pipeline{
				YTDLP:          cfg.YTDLPPath,
				YTDLPArgs:      cfg.YTDLPArgs,
				FFmpeg:         cfg.FFmpegPath,
				SegmentSeconds: cfg.WhisperSegmentSecs,
				TempRoot:       cfg.TempRoot,
			},
			Transcriber: transcribe.Helper{
				PythonPath:     cfg.PythonPath,
				Model:          cfg.WhisperModel,
				Device:         cfg.WhisperDevice,
				Compute:        cfg.WhisperCompute,
				SegmentSeconds: cfg.WhisperSegmentSecs,
			},
		},
		Notifier:         sender,
		Progress:         progressNotifier,
		RequestProcessor: summaryService,
	}
	workerErr := make(chan error, 1)
	go func() { workerErr <- worker.Run(ctx) }()
	botDone := make(chan struct{})
	go func() {
		telegramBot.Start(ctx)
		close(botDone)
	}()
	recoveryDone := make(chan struct{})
	go func() {
		runRecoverableSummaryLoop(ctx, summaryService, logger.With("component", "summary_recovery"), time.Minute)
		close(recoveryDone)
	}()
	watchDone := make(chan struct{})
	go func() {
		runWatchLoop(ctx, watchService, logger.With("component", "watch_loop"), 10*time.Minute)
		close(watchDone)
	}()

	logger.Info("bot started")
	select {
	case err := <-workerErr:
		stop()
		<-botDone
		<-recoveryDone
		<-watchDone
		if err != nil && !isShutdownError(err) {
			return err
		}
	case <-botDone:
		stop()
		<-recoveryDone
		<-watchDone
		if err := <-workerErr; err != nil && !isShutdownError(err) {
			return err
		}
	case <-ctx.Done():
		<-botDone
		<-recoveryDone
		<-watchDone
		if err := <-workerErr; err != nil && !isShutdownError(err) {
			return err
		}
	}
	logger.Info("bot stopped")
	return nil
}

func runRecoverableSummaryLoop(ctx context.Context, summaryService service.SummaryService, logger *slog.Logger, interval time.Duration) {
	for {
		if err := summaryService.ProcessRecoverableSummaryRequests(ctx); err != nil && !isShutdownError(err) {
			logger.Error("recoverable summary request processing failed", "error", err)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func runWatchLoop(ctx context.Context, watchService service.WatchService, logger *slog.Logger, interval time.Duration) {
	for {
		if err := watchService.CheckFeedsOnce(ctx); err != nil && !isShutdownError(err) {
			logger.Error("watch feed check failed", "error", err)
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func isShutdownError(err error) bool {
	if commandrunner.HasCleanupFailure(err) {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
