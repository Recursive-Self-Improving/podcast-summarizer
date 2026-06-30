package transcript

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/provider"
)

const (
	subtitleLanguages    = "zh,zh-CN,zh-Hans,zh-TW,zh-Hant,en,en-US,en-GB"
	manualSubtitleSource = "manual_subtitle"
	autoSubtitleSource   = "auto_subtitle"
)

type Downloader struct {
	Runner    commandrunner.Runner
	YTDLP     string
	YTDLPArgs []string
	TempRoot  string
	Logger    *slog.Logger
}

type DownloadResult struct {
	Source string
	Text   string
}

func (d Downloader) WithLogger(logger *slog.Logger) Downloader {
	d.Logger = logger
	return d
}

func (d Downloader) Download(ctx context.Context, canonicalURL string) (DownloadResult, error) {
	if err := provider.ValidateCanonicalYouTubeURL(canonicalURL); err != nil {
		return DownloadResult{}, err
	}
	runner := d.Runner
	if runner == nil {
		runner = commandrunner.ExecRunner{}
	}
	ytdlp := d.YTDLP
	if ytdlp == "" {
		ytdlp = "yt-dlp"
	}
	workdir, err := os.MkdirTemp(d.TempRoot, "subtitles-*")
	if err != nil {
		return DownloadResult{}, fmt.Errorf("create subtitle workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	prefix := filepath.Join(workdir, "subtitle")
	if result, err := d.tryDownload(ctx, runner, ytdlp, prefix, canonicalURL, false); err == nil {
		return result, nil
	} else {
		if commandrunner.HasCleanupFailure(err) {
			return DownloadResult{}, err
		}
		d.logger().Info("subtitle attempt failed", "source", manualSubtitleSource, "error", err)
	}
	if result, err := d.tryDownload(ctx, runner, ytdlp, prefix, canonicalURL, true); err == nil {
		return result, nil
	} else {
		if commandrunner.HasCleanupFailure(err) {
			return DownloadResult{}, err
		}
		d.logger().Info("subtitle attempt failed", "source", autoSubtitleSource, "error", err)
	}
	return DownloadResult{}, fmt.Errorf("no subtitle files found")
}

func (d Downloader) tryDownload(ctx context.Context, runner commandrunner.Runner, ytdlp, prefix, canonicalURL string, auto bool) (DownloadResult, error) {
	source := manualSubtitleSource
	if auto {
		source = autoSubtitleSource
	}
	d.logger().Info("subtitle attempt started", "source", source)
	args := append([]string{}, d.YTDLPArgs...)
	if auto {
		args = append(args, "--write-auto-subs")
	} else {
		args = append(args, "--write-subs")
	}
	args = append(args,
		"--sub-langs", subtitleLanguages,
		"--sub-format", "srt/vtt/best",
		"--convert-subs", "srt",
		"--skip-download",
		"-o", prefix+".%(ext)s",
		canonicalURL,
	)
	_, runErr := runner.Run(ctx, ytdlp, args...)
	if commandrunner.HasCleanupFailure(runErr) {
		return DownloadResult{}, runErr
	}
	text, err := parseFirstSubtitle(prefix)
	if err != nil {
		if runErr != nil {
			return DownloadResult{}, runErr
		}
		return DownloadResult{}, err
	}
	d.logger().Info("subtitle attempt succeeded", "source", source)
	return DownloadResult{Source: source, Text: text}, nil
}

func sortSubtitleFiles(prefix string, paths []string) {
	preferences := strings.Split(subtitleLanguages, ",")
	order := make(map[string]int, len(preferences))
	for i, language := range preferences {
		order[language] = i
	}
	sort.SliceStable(paths, func(i, j int) bool {
		leftRank := languageRank(prefix, paths[i], order)
		rightRank := languageRank(prefix, paths[j], order)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return paths[i] < paths[j]
	})
}

func languageRank(prefix, path string, order map[string]int) int {
	base := strings.TrimSuffix(strings.TrimPrefix(path, prefix+"."), filepath.Ext(path))
	if rank, ok := order[base]; ok {
		return rank
	}
	return len(order)
}

func parseFirstSubtitle(prefix string) (string, error) {
	matches, err := filepath.Glob(prefix + "*")
	if err != nil {
		return "", fmt.Errorf("find subtitle files: %w", err)
	}
	sortSubtitleFiles(prefix, matches)
	for _, path := range matches {
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read subtitle file: %w", err)
		}
		var lines []Line
		switch filepath.Ext(path) {
		case ".srt":
			lines, err = ParseSRT(string(contents))
		case ".vtt":
			lines, err = ParseVTT(string(contents))
		default:
			continue
		}
		if err != nil {
			return "", err
		}
		if len(lines) > 0 {
			return FormatLines(lines), nil
		}
	}
	return "", fmt.Errorf("no valid subtitle files found")
}

func (d Downloader) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}
