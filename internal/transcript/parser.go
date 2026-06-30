package transcript

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type Line struct {
	StartSeconds float64
	EndSeconds   float64
	Text         string
}

var tagPattern = regexp.MustCompile(`<[^>]+>`)
var whitespacePattern = regexp.MustCompile(`\s+`)

func ParseSRT(input string) ([]Line, error) {
	return parseCues(strings.ReplaceAll(input, "\r\n", "\n"), false)
}

func ParseVTT(input string) ([]Line, error) {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.TrimPrefix(input, "\ufeff")
	input = strings.TrimPrefix(input, "WEBVTT")
	return parseCues(input, true)
}

func FormatLines(lines []Line) string {
	var out []string
	for _, line := range lines {
		out = append(out, fmt.Sprintf("[%.1fs -> %.1fs] %s", line.StartSeconds, line.EndSeconds, line.Text))
	}
	return strings.Join(out, "\n")
}

func parseCues(input string, _ bool) ([]Line, error) {
	blocks := strings.Split(input, "\n\n")
	var lines []Line
	for _, block := range blocks {
		blockLines := nonEmptyLines(block)
		if len(blockLines) == 0 {
			continue
		}
		timeIndex := 0
		if !strings.Contains(blockLines[0], "-->") && len(blockLines) > 1 {
			timeIndex = 1
		}
		if !strings.Contains(blockLines[timeIndex], "-->") {
			continue
		}
		start, end, err := parseTimeRange(blockLines[timeIndex])
		if err != nil {
			continue
		}
		text := normalizeText(strings.Join(blockLines[timeIndex+1:], " "))
		if text == "" {
			continue
		}
		lines = append(lines, Line{StartSeconds: start, EndSeconds: end, Text: text})
	}
	return lines, nil
}

func nonEmptyLines(block string) []string {
	raw := strings.Split(strings.TrimSpace(block), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "NOTE") {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseTimeRange(line string) (float64, float64, error) {
	parts := strings.Split(line, "-->")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid timestamp range")
	}
	startFields := strings.Fields(strings.TrimSpace(parts[0]))
	if len(startFields) == 0 {
		return 0, 0, fmt.Errorf("missing start timestamp")
	}
	start, err := parseTimestamp(startFields[0])
	if err != nil {
		return 0, 0, err
	}
	endFields := strings.Fields(strings.TrimSpace(parts[1]))
	if len(endFields) == 0 {
		return 0, 0, fmt.Errorf("missing end timestamp")
	}
	end, err := parseTimestamp(endFields[0])
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func parseTimestamp(value string) (float64, error) {
	value = strings.Replace(value, ",", ".", 1)
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp: %s", value)
	}
	hours := 0
	minutePart := parts[0]
	secondPart := parts[1]
	if len(parts) == 3 {
		parsedHours, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, err
		}
		hours = parsedHours
		minutePart = parts[1]
		secondPart = parts[2]
	}
	minutes, err := strconv.Atoi(minutePart)
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(secondPart, 64)
	if err != nil {
		return 0, err
	}
	return float64(hours*3600+minutes*60) + seconds, nil
}

func normalizeText(text string) string {
	text = html.UnescapeString(text)
	text = tagPattern.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, " ", " ")
	text = whitespacePattern.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}
