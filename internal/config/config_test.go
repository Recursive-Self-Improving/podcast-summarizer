package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

func TestLoadDotenvIgnoresMissingFile(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), ".env")

	if err := loadDotenv(missingPath); err != nil {
		t.Fatalf("loadDotenv returned error: %v", err)
	}
}

func TestLoadDotenvLoadsFileWithoutOverridingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	fromFileKey := "PODCAST_SUMMARIZER_TEST_DOTENV_FROM_FILE"
	precedenceKey := "PODCAST_SUMMARIZER_TEST_DOTENV_PRECEDENCE"

	if err := os.Unsetenv(fromFileKey); err != nil {
		t.Fatalf("unset test env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(fromFileKey) })
	t.Setenv(precedenceKey, "shell-value")

	contents := fromFileKey + "=file-value\n" + precedenceKey + "=dotenv-value\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write dotenv file: %v", err)
	}

	if err := loadDotenv(path); err != nil {
		t.Fatalf("loadDotenv returned error: %v", err)
	}
	if value := os.Getenv(fromFileKey); value != "file-value" {
		t.Fatalf("%s = %q", fromFileKey, value)
	}
	if value := os.Getenv(precedenceKey); value != "shell-value" {
		t.Fatalf("%s = %q", precedenceKey, value)
	}
}

func TestLoadDotenvRejectsInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("BROKEN=\"unterminated\n"), 0o600); err != nil {
		t.Fatalf("write dotenv file: %v", err)
	}

	err := loadDotenv(path)
	if err == nil || !strings.Contains(err.Error(), "load") {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestLoadWithLookupAppliesDefaults(t *testing.T) {
	env := map[string]string{
		"TELEGRAM_BOT_TOKEN": "telegram-token",
		"BOT_OWNER_ID":       "12345",
		"OPENAI_API_KEY":     "openai-key",
		"OPENAI_MODEL":       "gpt-test",
	}

	cfg, err := LoadWithLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadWithLookup returned error: %v", err)
	}

	if cfg.TelegramBotToken != "telegram-token" {
		t.Fatalf("TelegramBotToken = %q", cfg.TelegramBotToken)
	}
	if cfg.BotOwnerID != 12345 {
		t.Fatalf("BotOwnerID = %d", cfg.BotOwnerID)
	}
	if cfg.OpenAIBaseURL != defaultOpenAIBaseURL {
		t.Fatalf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
	if cfg.SQLitePath != defaultSQLitePath {
		t.Fatalf("SQLitePath = %q", cfg.SQLitePath)
	}
	if cfg.TempRoot != "" {
		t.Fatalf("TempRoot = %q", cfg.TempRoot)
	}
	if cfg.YTDLPPath != defaultYTDLPPath {
		t.Fatalf("YTDLPPath = %q", cfg.YTDLPPath)
	}
	if cfg.FFmpegPath != defaultFFmpegPath {
		t.Fatalf("FFmpegPath = %q", cfg.FFmpegPath)
	}
	if cfg.PythonPath != defaultPythonPath {
		t.Fatalf("PythonPath = %q", cfg.PythonPath)
	}
	if cfg.WhisperModel != defaultWhisperModel {
		t.Fatalf("WhisperModel = %q", cfg.WhisperModel)
	}
	if cfg.WhisperDevice != defaultWhisperDevice {
		t.Fatalf("WhisperDevice = %q", cfg.WhisperDevice)
	}
	if cfg.WhisperCompute != defaultWhisperCompute {
		t.Fatalf("WhisperCompute = %q", cfg.WhisperCompute)
	}
	if cfg.WhisperSegmentSecs != defaultWhisperSegmentSecond {
		t.Fatalf("WhisperSegmentSecs = %d", cfg.WhisperSegmentSecs)
	}
	if !cfg.TelegramSkipOld {
		t.Fatal("TelegramSkipOld should default to true")
	}
	if cfg.SummaryBroadcastChannelID != 0 {
		t.Fatalf("SummaryBroadcastChannelID = %d", cfg.SummaryBroadcastChannelID)
	}
}

func TestLoadWithLookupReadsOverrides(t *testing.T) {
	env := map[string]string{
		"TELEGRAM_BOT_TOKEN":           "telegram-token",
		"BOT_OWNER_ID":                 "12345",
		"OPENAI_BASE_URL":              "https://llm.example/v1",
		"OPENAI_API_KEY":               "openai-key",
		"OPENAI_MODEL":                 "custom-model",
		"SQLITE_PATH":                  "/tmp/bot.db",
		"TEMP_ROOT":                    "/var/lib/bot/tmp",
		"YT_DLP_PATH":                  "/usr/local/bin/yt-dlp",
		"YT_DLP_ARGS":                  "--extractor-args \"youtube:player_client=mweb\" --cookies /var/lib/bot/cookies.txt",
		"FFMPEG_PATH":                  "/usr/local/bin/ffmpeg",
		"PYTHON_PATH":                  "/usr/local/bin/python3",
		"WHISPER_MODEL":                "medium",
		"WHISPER_DEVICE":               "cuda",
		"WHISPER_COMPUTE":              "float16",
		"WHISPER_SEGMENT_SECONDS":      "600",
		"TELEGRAM_SKIP_OLD_UPDATES":    "false",
		"SUMMARY_BROADCAST_CHANNEL_ID": "-1001234567890",
	}

	cfg, err := LoadWithLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadWithLookup returned error: %v", err)
	}

	if cfg.OpenAIBaseURL != "https://llm.example/v1" {
		t.Fatalf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIModel != "custom-model" {
		t.Fatalf("OpenAIModel = %q", cfg.OpenAIModel)
	}
	if cfg.SQLitePath != "/tmp/bot.db" {
		t.Fatalf("SQLitePath = %q", cfg.SQLitePath)
	}
	if cfg.TempRoot != "/var/lib/bot/tmp" {
		t.Fatalf("TempRoot = %q", cfg.TempRoot)
	}
	if cfg.YTDLPPath != "/usr/local/bin/yt-dlp" {
		t.Fatalf("YTDLPPath = %q", cfg.YTDLPPath)
	}
	wantYTDLPArgs := []string{"--extractor-args", "youtube:player_client=mweb", "--cookies", "/var/lib/bot/cookies.txt"}
	if !slices.Equal(cfg.YTDLPArgs, wantYTDLPArgs) {
		t.Fatalf("YTDLPArgs = %#v", cfg.YTDLPArgs)
	}
	if cfg.FFmpegPath != "/usr/local/bin/ffmpeg" {
		t.Fatalf("FFmpegPath = %q", cfg.FFmpegPath)
	}
	if cfg.PythonPath != "/usr/local/bin/python3" {
		t.Fatalf("PythonPath = %q", cfg.PythonPath)
	}
	if cfg.WhisperModel != "medium" {
		t.Fatalf("WhisperModel = %q", cfg.WhisperModel)
	}
	if cfg.WhisperDevice != "cuda" {
		t.Fatalf("WhisperDevice = %q", cfg.WhisperDevice)
	}
	if cfg.WhisperCompute != "float16" {
		t.Fatalf("WhisperCompute = %q", cfg.WhisperCompute)
	}
	if cfg.WhisperSegmentSecs != 600 {
		t.Fatalf("WhisperSegmentSecs = %d", cfg.WhisperSegmentSecs)
	}
	if cfg.TelegramSkipOld {
		t.Fatal("TelegramSkipOld should parse false")
	}
	if cfg.SummaryBroadcastChannelID != -1001234567890 {
		t.Fatalf("SummaryBroadcastChannelID = %d", cfg.SummaryBroadcastChannelID)
	}
}

func TestLoadWithLookupRequiresValues(t *testing.T) {
	_, err := LoadWithLookup(mapLookup(map[string]string{}))
	if err == nil {
		t.Fatal("expected error")
	}

	message := err.Error()
	for _, name := range []string{"TELEGRAM_BOT_TOKEN", "BOT_OWNER_ID", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		if !strings.Contains(message, name) {
			t.Fatalf("error %q does not mention %s", message, name)
		}
	}
}

func TestLoadWithLookupRejectsInvalidOwnerID(t *testing.T) {
	env := requiredEnv()
	env["BOT_OWNER_ID"] = "not-int"

	_, err := LoadWithLookup(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "BOT_OWNER_ID") {
		t.Fatalf("expected BOT_OWNER_ID error, got %v", err)
	}
}

func TestLoadWithLookupRejectsInvalidWhisperSegmentSeconds(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnv()
			env["WHISPER_SEGMENT_SECONDS"] = value

			_, err := LoadWithLookup(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "WHISPER_SEGMENT_SECONDS") {
				t.Fatalf("expected WHISPER_SEGMENT_SECONDS error, got %v", err)
			}
		})
	}
}

func TestLoadWithLookupRejectsInvalidTelegramSkipOldUpdates(t *testing.T) {
	env := requiredEnv()
	env["TELEGRAM_SKIP_OLD_UPDATES"] = "maybe"

	_, err := LoadWithLookup(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_SKIP_OLD_UPDATES") {
		t.Fatalf("expected TELEGRAM_SKIP_OLD_UPDATES error, got %v", err)
	}
}

func TestLoadWithLookupRejectsInvalidSummaryBroadcastChannelID(t *testing.T) {
	env := requiredEnv()
	env["SUMMARY_BROADCAST_CHANNEL_ID"] = "@channel"

	_, err := LoadWithLookup(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "SUMMARY_BROADCAST_CHANNEL_ID") {
		t.Fatalf("expected SUMMARY_BROADCAST_CHANNEL_ID error, got %v", err)
	}
}

func TestLoadWithLookupRejectsInvalidYTDLPArgs(t *testing.T) {
	env := requiredEnv()
	env["YT_DLP_ARGS"] = "--extractor-args \"youtube:player_client=mweb"

	_, err := LoadWithLookup(mapLookup(env))
	if err == nil || !strings.Contains(err.Error(), "YT_DLP_ARGS") {
		t.Fatalf("expected YT_DLP_ARGS error, got %v", err)
	}
}

func TestLoadWithLookupDefaultSummaryVariantDefaultsToSimplified(t *testing.T) {
	cfg, err := LoadWithLookup(mapLookup(requiredEnv()))
	if err != nil {
		t.Fatalf("LoadWithLookup returned error: %v", err)
	}
	if cfg.DefaultSummaryVariant != summarize.DefaultSummaryVariant() {
		t.Fatalf("DefaultSummaryVariant = %#v, want %#v", cfg.DefaultSummaryVariant, summarize.DefaultSummaryVariant())
	}
}

func TestLoadWithLookupDefaultSummaryVariantAcceptsTraditionalOverride(t *testing.T) {
	env := requiredEnv()
	env["DEFAULT_SUMMARY_VARIANT"] = "zh-hant"

	cfg, err := LoadWithLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadWithLookup returned error: %v", err)
	}
	if cfg.DefaultSummaryVariant != summarize.VariantTraditional {
		t.Fatalf("DefaultSummaryVariant = %#v, want %#v", cfg.DefaultSummaryVariant, summarize.VariantTraditional)
	}
}

func TestLoadWithLookupDefaultSummaryVariantAcceptsSimplifiedOverride(t *testing.T) {
	env := requiredEnv()
	env["DEFAULT_SUMMARY_VARIANT"] = "zh-hans"

	cfg, err := LoadWithLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadWithLookup returned error: %v", err)
	}
	if cfg.DefaultSummaryVariant != summarize.VariantSimplified {
		t.Fatalf("DefaultSummaryVariant = %#v, want %#v", cfg.DefaultSummaryVariant, summarize.VariantSimplified)
	}
}

func TestLoadWithLookupDefaultSummaryVariantRejectsAliasesAndUnknown(t *testing.T) {
	for _, value := range []string{"js", "fs", "simplified", "traditional", "zh", "zh-CN", "zh-TW", "en", "ZH-HANT", "ZH-HANS"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnv()
			env["DEFAULT_SUMMARY_VARIANT"] = value

			_, err := LoadWithLookup(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "DEFAULT_SUMMARY_VARIANT") {
				t.Fatalf("expected DEFAULT_SUMMARY_VARIANT error for %q, got %v", value, err)
			}
		})
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"TELEGRAM_BOT_TOKEN": "telegram-token",
		"BOT_OWNER_ID":       "12345",
		"OPENAI_API_KEY":     "openai-key",
		"OPENAI_MODEL":       "gpt-test",
	}
}

func mapLookup(values map[string]string) LookupFunc {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
