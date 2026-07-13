# GORM Logger Parameterization Verification

## Scope

Slow SQL logging must retain query structure and timing while excluding bound values. This applies to the primary database logger, maintenance-session logger, and migration logger because they share `newGormLoggerWithSlowThreshold`.

## Change

The shared GORM logger enables `ParameterizedQueries`. A regression test executes a slow bound query containing a sentinel access-token value and verifies that the output does not contain that value.

## Verification

| Command | Exit | Result |
| --- | --- | --- |
| `go test ./model -run TestNewGormLoggerWithSlowThresholdRedactsBoundValues -count=1 -timeout=60s` | 0 | The bound sentinel value is absent from slow SQL output. |
| `go test ./model -run 'Test(NewGormLogger|RunDBMigration|BackgroundDB)' -count=1 -timeout=90s` | 0 | Logger filtering and maintenance logger behavior passed. |
| `go test ./model -count=1 -timeout=120s` | 0 | Model package regression suite passed. |
| `go test -race ./model -count=1 -timeout=180s` | 0 | No race reported. |
| `go vet ./model` | 0 | Passed. |
| `git diff --check` | 0 | No whitespace errors. |

## Boundary

This change redacts values emitted by the GORM logger. It does not alter database query execution, stored values, authentication, or other application log sources.
