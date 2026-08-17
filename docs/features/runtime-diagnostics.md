# Local runtime diagnostics

The authenticated V2 settings page reads `GET /api/runtime-diagnostics` to show
a deliberately small local status summary.

The response contains only:

- the sanitized application version;
- process uptime in seconds;
- whether a bounded database ping succeeded; and
- aggregate application-cache file count, byte count, scan-error count, and a
  flag indicating whether the bounded scan completed.

The database ping has a short timeout. Cache enumeration has both a short
timeout and a fixed entry ceiling, skips symbolic links and Windows reparse
points, and reports only aggregate values. Missing cache roots are treated as
empty.

The endpoint never returns absolute paths, filenames, database contents,
environment variables, network state, or underlying error text. It does not
run shell commands, probe external services, refresh caches, or expose a write
method. It is a diagnostic snapshot, not a monitoring or maintenance API.
