#!/usr/bin/env python3
import argparse
import sys
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser(description="Transcribe WAV audio with faster-whisper")
    parser.add_argument("--input", required=True, help="WAV file or directory containing part_*.wav files")
    parser.add_argument("--output", required=True, help="Path to write the transcript text")
    parser.add_argument("--model", required=True, help="faster-whisper model name or path")
    parser.add_argument("--device", required=True, help="Inference device, such as cpu or cuda")
    parser.add_argument("--compute", required=True, help="Compute type, such as int8 or float16")
    parser.add_argument("--segment-sec", type=positive_int, required=True, help="Seconds represented by each split input segment")
    args = parser.parse_args()

    try:
        from faster_whisper import WhisperModel
    except ImportError as err:
        print(f"failed to import faster_whisper: {err}", file=sys.stderr)
        return 1

    try:
        inputs = input_paths(Path(args.input))
        output = Path(args.output)
        output.parent.mkdir(parents=True, exist_ok=True)
        model = WhisperModel(args.model, device=args.device, compute_type=args.compute)
        lines = transcribe_inputs(model, inputs, args.segment_sec)
        output.write_text("".join(lines), encoding="utf-8")
    except Exception as err:
        print(f"transcription failed: {err}", file=sys.stderr)
        return 1

    return 0


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be a positive integer")
    return parsed


def input_paths(path: Path) -> list[Path]:
    if path.is_file():
        return [path]
    if path.is_dir():
        parts = sorted(path.glob("part_*.wav"))
        if parts:
            return parts
        raise FileNotFoundError(f"no part_*.wav files in {path}")
    raise FileNotFoundError(f"input not found: {path}")


def transcribe_inputs(model, inputs: list[Path], segment_sec: int) -> list[str]:
    lines: list[str] = []
    has_multiple_inputs = len(inputs) > 1
    for index, path in enumerate(inputs):
        offset = index * segment_sec if has_multiple_inputs else 0
        segments, _ = model.transcribe(str(path))
        for segment in segments:
            text = segment.text.strip()
            if text:
                lines.append(f"[{timestamp(offset + segment.start)} --> {timestamp(offset + segment.end)}] {text}\n")
    return lines


def timestamp(seconds: float) -> str:
    millis = round(seconds * 1000)
    hours, remainder = divmod(millis, 3_600_000)
    minutes, remainder = divmod(remainder, 60_000)
    secs, millis = divmod(remainder, 1000)
    return f"{hours:02d}:{minutes:02d}:{secs:02d}.{millis:03d}"


if __name__ == "__main__":
    raise SystemExit(main())
