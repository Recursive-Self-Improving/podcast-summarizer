package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
)

const (
	defaultOpenAIBaseURL        = "https://api.openai.com/v1"
	defaultSQLitePath           = "./data/bot.db"
	defaultYTDLPPath            = "yt-dlp"
	defaultFFmpegPath           = "ffmpeg"
	defaultPythonPath           = "python3"
	defaultWhisperModel         = "small"
	defaultWhisperDevice        = "cpu"
	defaultWhisperCompute       = "int8"
	defaultWhisperSegmentSecond = 300
)

type Config struct {
	TelegramBotToken   string
	TelegramSkipOld    bool
	BotOwnerID         int64
	OpenAIBaseURL      string
	OpenAIAPIKey       string
	OpenAIModel        string
	SQLitePath         string
	TempRoot           string
	YTDLPPath          string
	YTDLPArgs          []string
	FFmpegPath         string
	PythonPath         string
	WhisperModel       string
	WhisperDevice      string
	WhisperCompute     string
	WhisperSegmentSecs int
}

type LookupFunc func(string) (string, bool)

func Load() (Config, error) {
	if err := loadDotenv(".env"); err != nil {
		return Config{}, err
	}
	return LoadWithLookup(os.LookupEnv)
}

func loadDotenv(path string) error {
	if err := godotenv.Load(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
}

func LoadWithLookup(lookup LookupFunc) (Config, error) {
	cfg := Config{
		OpenAIBaseURL:      getOrDefault(lookup, "OPENAI_BASE_URL", defaultOpenAIBaseURL),
		SQLitePath:         getOrDefault(lookup, "SQLITE_PATH", defaultSQLitePath),
		TempRoot:           getOrDefault(lookup, "TEMP_ROOT", ""),
		YTDLPPath:          getOrDefault(lookup, "YT_DLP_PATH", defaultYTDLPPath),
		FFmpegPath:         getOrDefault(lookup, "FFMPEG_PATH", defaultFFmpegPath),
		PythonPath:         getOrDefault(lookup, "PYTHON_PATH", defaultPythonPath),
		WhisperModel:       getOrDefault(lookup, "WHISPER_MODEL", defaultWhisperModel),
		WhisperDevice:      getOrDefault(lookup, "WHISPER_DEVICE", defaultWhisperDevice),
		WhisperCompute:     getOrDefault(lookup, "WHISPER_COMPUTE", defaultWhisperCompute),
		WhisperSegmentSecs: defaultWhisperSegmentSecond,
	}

	var missing []string
	cfg.TelegramBotToken = getRequired(lookup, "TELEGRAM_BOT_TOKEN", &missing)
	cfg.OpenAIAPIKey = getRequired(lookup, "OPENAI_API_KEY", &missing)
	cfg.OpenAIModel = getRequired(lookup, "OPENAI_MODEL", &missing)
	ownerID := getRequired(lookup, "BOT_OWNER_ID", &missing)
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", joinNames(missing))
	}

	parsedOwnerID, err := strconv.ParseInt(ownerID, 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("BOT_OWNER_ID must be an int64: %w", err)
	}
	cfg.BotOwnerID = parsedOwnerID

	telegramSkipOld, err := getBoolOrDefault(lookup, "TELEGRAM_SKIP_OLD_UPDATES", true)
	if err != nil {
		return Config{}, err
	}
	cfg.TelegramSkipOld = telegramSkipOld

	cfg.YTDLPArgs, err = getArgList(lookup, "YT_DLP_ARGS")
	if err != nil {
		return Config{}, err
	}

	if raw, ok := lookup("WHISPER_SEGMENT_SECONDS"); ok && raw != "" {
		segmentSecs, err := strconv.Atoi(raw)
		if err != nil || segmentSecs <= 0 {
			return Config{}, errors.New("WHISPER_SEGMENT_SECONDS must be a positive integer")
		}
		cfg.WhisperSegmentSecs = segmentSecs
	}

	return cfg, nil
}

func getRequired(lookup LookupFunc, name string, missing *[]string) string {
	value, ok := lookup(name)
	if !ok || value == "" {
		*missing = append(*missing, name)
		return ""
	}
	return value
}

func getOrDefault(lookup LookupFunc, name, defaultValue string) string {
	value, ok := lookup(name)
	if !ok || value == "" {
		return defaultValue
	}
	return value
}

func getBoolOrDefault(lookup LookupFunc, name string, defaultValue bool) (bool, error) {
	value, ok := lookup(name)
	if !ok || value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}

func getArgList(lookup LookupFunc, name string) ([]string, error) {
	value, ok := lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return nil, nil
	}
	args, err := splitArgs(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return args, nil
}

func splitArgs(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	fieldStarted := false
	escaped := false

	flush := func() {
		args = append(args, current.String())
		current.Reset()
		fieldStarted = false
	}

	for _, char := range input {
		if escaped {
			current.WriteRune(char)
			fieldStarted = true
			escaped = false
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
				continue
			}
			if quote == '"' && char == '\\' {
				escaped = true
				continue
			}
			current.WriteRune(char)
			fieldStarted = true
			continue
		}
		if unicode.IsSpace(char) {
			if fieldStarted {
				flush()
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			fieldStarted = true
			continue
		}
		if char == '\\' {
			escaped = true
			fieldStarted = true
			continue
		}
		current.WriteRune(char)
		fieldStarted = true
	}
	if escaped {
		return nil, errors.New("unfinished escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if fieldStarted {
		flush()
	}
	return args, nil
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	out := names[0]
	for _, name := range names[1:] {
		out += ", " + name
	}
	return out
}
