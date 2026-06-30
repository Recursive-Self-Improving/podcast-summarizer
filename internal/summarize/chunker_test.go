package summarize

import (
	"strings"
	"testing"
)

func TestChunkTranscriptOneChunk(t *testing.T) {
	chunks := ChunkTranscript("[1.0s -> 2.0s] hello", 100)
	if len(chunks) != 1 || chunks[0] != "[1.0s -> 2.0s] hello" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestChunkTranscriptSplitsOnLineBoundaries(t *testing.T) {
	transcript := strings.Join([]string{
		"[1.0s -> 2.0s] first",
		"[2.0s -> 3.0s] second",
		"[3.0s -> 4.0s] third",
	}, "\n")
	chunks := ChunkTranscript(transcript, 35)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %#v", chunks)
	}
	for _, chunk := range chunks {
		if strings.Count(chunk, "[") != 1 {
			t.Fatalf("chunk crosses boundary unexpectedly: %q", chunk)
		}
	}
}

func TestChunkTranscriptEmpty(t *testing.T) {
	if chunks := ChunkTranscript(" \n\t ", 10); chunks != nil {
		t.Fatalf("chunks = %#v", chunks)
	}
}
