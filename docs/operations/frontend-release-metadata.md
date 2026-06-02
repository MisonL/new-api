# Frontend Release Metadata

`new-api-release.json` is generated into each frontend `dist` directory during release builds. The backend reads the embedded files at startup to verify that the default and classic frontend bundles belong to the same release as the backend binary.

The file is an internal release artifact. Static serving blocks direct browser access to this filename.

## Schema 1

Example:

```json
{
  "schema": 1,
  "app": "new-api",
  "frontend": "default",
  "version": "v1.1.0",
  "build_commit": "abc123",
  "build_date": "2026-06-02T00:00:00Z"
}
```

Fields:

- `schema`: metadata schema version. Current value is `1`.
- `app`: application identifier. Must be `new-api`.
- `frontend`: frontend bundle name. Must be `default` or `classic`.
- `version`: release version used by the frontend build. Must be non-empty and match backend `common.Version` when the backend version is known.
- `build_commit`: source commit used by the frontend build. Must match backend `common.BuildCommit` when the backend commit is known.
- `build_date`: UTC build timestamp or `unknown`.

## Evolution Rules

- Bump `schema` when removing a field, changing field meaning, changing validation rules incompatibly, or changing a field type.
- Do not bump `schema` when adding optional fields that older backends can ignore.
- Keep schema `1` readers strict for `app`, `frontend`, `version`, and `build_commit`; mismatches should fail startup instead of silently serving mixed frontend and backend assets.
- Update `scripts/write-frontend-release-metadata.sh`, `router/frontend_release.go`, and their tests in the same change when schema rules change.
