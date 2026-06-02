package router

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func frontendReleaseMetadata(app string, frontend string, version string, commit string) []byte {
	raw, err := common.Marshal(struct {
		App         string `json:"app"`
		Frontend    string `json:"frontend"`
		Version     string `json:"version"`
		BuildCommit string `json:"build_commit"`
	}{
		App:         app,
		Frontend:    frontend,
		Version:     version,
		BuildCommit: commit,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func frontendReleaseAssetsFS(defaultPayload []byte, classicPayload []byte) fstest.MapFS {
	fs := fstest.MapFS{}
	if defaultPayload != nil {
		fs["web/default/dist/new-api-release.json"] = &fstest.MapFile{Data: defaultPayload}
	}
	if classicPayload != nil {
		fs["web/classic/dist/new-api-release.json"] = &fstest.MapFile{Data: classicPayload}
	}
	return fs
}

func TestValidateFrontendReleaseMetadataRequiresMatchingFrontend(t *testing.T) {
	fs := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
		},
	}

	err := validateFrontendReleaseMetadata(fs, "new-api-release.json", "classic")
	require.ErrorContains(t, err, "frontend mismatch")
}

func TestValidateFrontendReleaseMetadataChecksBackendVersionAndCommit(t *testing.T) {
	// DO NOT RUN IN PARALLEL: this test mutates common.Version and common.BuildCommit.
	previousVersion := common.Version
	previousCommit := common.BuildCommit
	common.Version = "v1.1.0"
	common.BuildCommit = "abc123"
	t.Cleanup(func() {
		common.Version = previousVersion
		common.BuildCommit = previousCommit
	})

	fs := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
		},
	}

	require.NoError(t, validateFrontendReleaseMetadata(fs, "new-api-release.json", "default"))

	badVersionFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.1", "abc123"),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(badVersionFS, "new-api-release.json", "default"), "does not match backend version")

	badCommitFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.0", "def456"),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(badCommitFS, "new-api-release.json", "default"), "does not match backend commit")

	badAppFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("other-app", "default", "v1.1.0", "abc123"),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(badAppFS, "new-api-release.json", "default"), "app mismatch")

	emptyVersionFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "", "abc123"),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(emptyVersionFS, "new-api-release.json", "default"), "version is empty")

	whitespaceVersionFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", strings.Repeat(" ", 3), "abc123"),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(whitespaceVersionFS, "new-api-release.json", "default"), "version is empty")

	emptyCommitFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.0", ""),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(emptyCommitFS, "new-api-release.json", "default"), "build_commit is empty")

	whitespaceCommitFS := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.0", strings.Repeat(" ", 3)),
		},
	}
	require.ErrorContains(t, validateFrontendReleaseMetadata(whitespaceCommitFS, "new-api-release.json", "default"), "build_commit is empty")

	missingFileFS := fstest.MapFS{}
	require.ErrorContains(t, validateFrontendReleaseMetadata(missingFileFS, "new-api-release.json", "default"), "metadata missing")
}

func TestValidateFrontendReleaseMetadataSkipsWhenBackendUnknown(t *testing.T) {
	// DO NOT RUN IN PARALLEL: this test mutates common.Version and common.BuildCommit.
	previousVersion := common.Version
	previousCommit := common.BuildCommit
	common.Version = "unknown"
	common.BuildCommit = "unknown"
	t.Cleanup(func() {
		common.Version = previousVersion
		common.BuildCommit = previousCommit
	})

	fs := fstest.MapFS{
		"new-api-release.json": {
			Data: frontendReleaseMetadata("new-api", "default", "v1.1.1", "def456"),
		},
	}

	require.NoError(t, validateFrontendReleaseMetadata(fs, "new-api-release.json", "default"))
}

func TestValidateFrontendReleaseAssetsUsesEmbeddedDistPaths(t *testing.T) {
	// DO NOT RUN IN PARALLEL: this test mutates common.Version and common.BuildCommit.
	// The t.Cleanup below restores both globals for later tests.
	previousVersion := common.Version
	previousCommit := common.BuildCommit
	common.Version = "v1.1.0"
	common.BuildCommit = "abc123"
	t.Cleanup(func() {
		common.Version = previousVersion
		common.BuildCommit = previousCommit
	})

	fs := frontendReleaseAssetsFS(
		frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
		frontendReleaseMetadata("new-api", "classic", "v1.1.0", "abc123"),
	)
	require.NoError(t, validateFrontendReleaseAssets(fs, fs))

	missingClassicFS := frontendReleaseAssetsFS(
		frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
		nil,
	)
	require.ErrorContains(t, validateFrontendReleaseAssets(missingClassicFS, missingClassicFS), "classic frontend release metadata missing")

	mismatchedClassicFS := frontendReleaseAssetsFS(
		frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
		frontendReleaseMetadata("new-api", "default", "v1.1.0", "abc123"),
	)
	require.ErrorContains(t, validateFrontendReleaseAssets(mismatchedClassicFS, mismatchedClassicFS), "classic frontend release metadata frontend mismatch")
}
