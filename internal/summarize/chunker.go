package summarize

import "strings"

const DefaultMaxChunkChars = 50000

func ChunkText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkChars
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	for len(text) > maxChars {
		cut := strings.LastIndex(text[:maxChars+1], "\n\n")
		if cut <= 0 {
			cut = strings.LastIndex(text[:maxChars+1], "\n")
		}
		if cut <= 0 {
			cut = maxChars
		}
		chunks = append(chunks, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}

func ChunkTranscript(transcript string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = DefaultMaxChunkChars
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil
	}
	if len(transcript) <= maxChars {
		return []string{transcript}
	}

	lines := strings.Split(transcript, "\n")
	var chunks []string
	var current strings.Builder
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		candidateLen := len(line)
		if current.Len() > 0 {
			candidateLen += current.Len() + 1
		}
		if current.Len() > 0 && candidateLen > maxChars {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
