package migrations

import "embed"

// FS contains embedded database migration files.
//
//go:embed migrations/*.sql
var FS embed.FS

// HelperScripts contains embedded Python helper scripts.
//
//go:embed scripts/faster_whisper_transcribe.py
var HelperScripts embed.FS
