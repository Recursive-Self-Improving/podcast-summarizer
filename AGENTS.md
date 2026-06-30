# Lessons

- Runtime temp directory configuration is `TEMP_ROOT`; leave it empty to preserve Go's default temp behavior (`TMPDIR`/`/tmp`) and set it explicitly only for deployments where the service user cannot write the system temp directory.
- Runtime external tools (`YT_DLP_PATH`, `FFMPEG_PATH`, `PYTHON_PATH`) should support absolute paths because deployment users and systemd services may not inherit an interactive shell `PATH`.
- For yt-dlp 2026.x YouTube JS challenges, install a JS runtime (`nodejs` or `deno`) and pass `--js-runtimes node --remote-components ejs:github`; keep `youtube:player_client=mweb` attached to `--extractor-args`, never as a standalone positional argument.
