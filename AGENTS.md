# Lessons

- Runtime temp directory configuration is `TEMP_ROOT`; leave it empty to preserve Go's default temp behavior (`TMPDIR`/`/tmp`) and set it explicitly only for deployments where the service user cannot write the system temp directory.
- Runtime external tools (`YT_DLP_PATH`, `FFMPEG_PATH`, `PYTHON_PATH`) should support absolute paths because deployment users and systemd services may not inherit an interactive shell `PATH`.
