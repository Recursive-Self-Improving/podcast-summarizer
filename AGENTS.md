# Lessons

- Runtime temp directory configuration is `TEMP_ROOT`; leave it empty to preserve Go's default temp behavior (`TMPDIR`/`/tmp`) and set it explicitly only for deployments where the service user cannot write the system temp directory.
