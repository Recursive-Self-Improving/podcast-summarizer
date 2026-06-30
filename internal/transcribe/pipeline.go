package transcribe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/commandrunner"
)

const (
	defaultSegmentSeconds = 300
	wavSampleRateHz       = 16000
	wavBytesPerSample     = 2
	maxDirectAudioBytes   = 2 << 30
	defaultHTTPTimeout    = 2 * time.Minute
)

type Pipeline struct {
	Runner         commandrunner.Runner
	YTDLP          string
	YTDLPArgs      []string
	FFmpeg         string
	FFprobe        string
	TempRoot       string
	SegmentSeconds int
	HTTPClient     *http.Client
}

type AudioSource struct {
	URL    string
	Direct bool
}

type AudioInput struct {
	WorkDir  string
	WAVPath  string
	Segments []string
}

func (p Pipeline) Process(ctx context.Context, source AudioSource, consume func(context.Context, AudioInput) error) error {
	runner := p.Runner
	if runner == nil {
		runner = commandrunner.ExecRunner{}
	}
	workdir, err := os.MkdirTemp(p.TempRoot, "audio-*")
	if err != nil {
		return fmt.Errorf("create audio workdir: %w", err)
	}
	defer os.RemoveAll(workdir)

	audioPath, err := p.downloadAudio(ctx, runner, workdir, source)
	if err != nil {
		return err
	}
	wavPath, err := p.convertAudio(ctx, runner, workdir, audioPath)
	if err != nil {
		return err
	}
	segments, err := p.splitAudioIfNeeded(ctx, runner, wavPath)
	if err != nil {
		return err
	}
	if consume == nil {
		return nil
	}
	return consume(ctx, AudioInput{WorkDir: workdir, WAVPath: wavPath, Segments: segments})
}

func (p Pipeline) downloadAudio(ctx context.Context, runner commandrunner.Runner, workdir string, source AudioSource) (string, error) {
	if source.Direct {
		return p.downloadDirectAudio(ctx, workdir, source.URL)
	}
	return p.downloadYTDLPAudio(ctx, runner, workdir, source.URL)
}

func (p Pipeline) downloadYTDLPAudio(ctx context.Context, runner commandrunner.Runner, workdir, canonicalURL string) (string, error) {
	prefix := filepath.Join(workdir, "audio")
	args := append([]string{}, p.YTDLPArgs...)
	args = append(args, "-f", "bestaudio[ext=m4a]/bestaudio", "-o", prefix+".%(ext)s", canonicalURL)
	_, err := runner.Run(ctx, defaultString(p.YTDLP, "yt-dlp"), args...)
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(prefix + ".*")
	if err != nil {
		return "", fmt.Errorf("find downloaded audio: %w", err)
	}
	sort.Strings(matches)
	for _, path := range matches {
		if filepath.Ext(path) != ".wav" {
			return path, nil
		}
	}
	return "", fmt.Errorf("downloaded audio file not found")
}

func (p Pipeline) downloadDirectAudio(ctx context.Context, workdir, audioURL string) (string, error) {
	parsed, err := url.Parse(audioURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("invalid direct audio URL")
	}
	ext := filepath.Ext(parsed.Path)
	if ext == "" {
		ext = ".audio"
	}
	audioPath := filepath.Join(workdir, "audio"+ext)
	client := p.httpClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "podcast-summarizer/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download direct audio: HTTP %d", resp.StatusCode)
	}
	file, err := os.Create(audioPath)
	if err != nil {
		return "", err
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxDirectAudioBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download direct audio body: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("finalize direct audio file: %w", closeErr)
	}
	if written > maxDirectAudioBytes {
		return "", fmt.Errorf("direct audio response too large")
	}
	return audioPath, nil
}

func (p Pipeline) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: defaultHTTPTimeout}
}

func (p Pipeline) convertAudio(ctx context.Context, runner commandrunner.Runner, workdir, audioPath string) (string, error) {
	wavPath := filepath.Join(workdir, "audio.wav")
	_, err := runner.Run(ctx, defaultString(p.FFmpeg, "ffmpeg"), "-y", "-i", audioPath, "-ar", "16000", "-ac", "1", wavPath)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(wavPath); err != nil {
		return "", fmt.Errorf("converted wav not found: %w", err)
	}
	return wavPath, nil
}

func (p Pipeline) splitAudioIfNeeded(ctx context.Context, runner commandrunner.Runner, wavPath string) ([]string, error) {
	duration, err := p.durationSeconds(ctx, runner, wavPath)
	if err != nil {
		return nil, err
	}
	segmentSeconds := p.segmentSeconds()
	if duration <= float64(segmentSeconds) {
		return nil, nil
	}
	segmentDir := filepath.Join(filepath.Dir(wavPath), "segments")
	if err := os.Mkdir(segmentDir, 0o755); err != nil {
		return nil, fmt.Errorf("create segment dir: %w", err)
	}
	pattern := filepath.Join(segmentDir, "part_%03d.wav")
	_, err = runner.Run(
		ctx,
		defaultString(p.FFmpeg, "ffmpeg"),
		"-y", "-i", wavPath,
		"-f", "segment", "-segment_time", strconv.Itoa(segmentSeconds),
		"-c", "copy", pattern,
	)
	if err != nil {
		return nil, err
	}
	segments, err := filepath.Glob(filepath.Join(segmentDir, "part_*.wav"))
	if err != nil {
		return nil, fmt.Errorf("find audio segments: %w", err)
	}
	sort.Strings(segments)
	if len(segments) == 0 {
		return nil, fmt.Errorf("audio segments not found")
	}
	return segments, nil
}

func (p Pipeline) durationSeconds(ctx context.Context, runner commandrunner.Runner, wavPath string) (float64, error) {
	result, err := runner.Run(ctx, defaultString(p.FFprobe, "ffprobe"), "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", wavPath)
	if err == nil {
		if duration, parseErr := strconv.ParseFloat(strings.TrimSpace(result.Stdout), 64); parseErr == nil {
			return duration, nil
		}
	}
	if commandrunner.HasCleanupFailure(err) {
		return 0, err
	}
	info, statErr := os.Stat(wavPath)
	if statErr != nil {
		return 0, fmt.Errorf("stat wav for duration fallback: %w", statErr)
	}
	return float64(info.Size()) / float64(wavSampleRateHz*wavBytesPerSample), nil
}

func (p Pipeline) segmentSeconds() int {
	if p.SegmentSeconds > 0 {
		return p.SegmentSeconds
	}
	return defaultSegmentSeconds
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
