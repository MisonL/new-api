package router

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const frontendReleaseMetadataName = "new-api-release.json"

func validateFrontendReleaseAssets(defaultFS fs.FS, classicFS fs.FS) error {
	if err := validateFrontendReleaseMetadata(defaultFS, "web/default/dist/"+frontendReleaseMetadataName, "default"); err != nil {
		return err
	}
	if err := validateFrontendReleaseMetadata(classicFS, "web/classic/dist/"+frontendReleaseMetadataName, "classic"); err != nil {
		return err
	}
	return nil
}

func validateFrontendReleaseMetadata(fsEmbed fs.FS, metadataPath string, frontend string) error {
	raw, err := fs.ReadFile(fsEmbed, metadataPath)
	if err != nil {
		return fmt.Errorf("%s frontend release metadata missing: %w", frontend, err)
	}

	var metadata struct {
		App         string `json:"app"`
		Frontend    string `json:"frontend"`
		Version     string `json:"version"`
		BuildCommit string `json:"build_commit"`
	}
	if err := common.Unmarshal(raw, &metadata); err != nil {
		return fmt.Errorf("%s frontend release metadata invalid: %w", frontend, err)
	}
	if metadata.App != "new-api" {
		return fmt.Errorf("%s frontend release metadata app mismatch: %q", frontend, metadata.App)
	}
	if metadata.Frontend != frontend {
		return fmt.Errorf("%s frontend release metadata frontend mismatch: %q", frontend, metadata.Frontend)
	}
	metadata.Version = strings.TrimSpace(metadata.Version)
	metadata.BuildCommit = strings.TrimSpace(metadata.BuildCommit)
	if metadata.Version == "" {
		return fmt.Errorf("%s frontend release metadata version is empty", frontend)
	}
	if metadata.BuildCommit == "" {
		return fmt.Errorf("%s frontend release metadata build_commit is empty", frontend)
	}
	backendVersion := normalizedBackendVersion()
	backendCommit := normalizedBackendCommit()
	if backendVersion != "" && metadata.Version != backendVersion {
		return fmt.Errorf("%s frontend version %q does not match backend version %q", frontend, metadata.Version, backendVersion)
	}
	if backendCommit != "" && metadata.BuildCommit != backendCommit {
		return fmt.Errorf("%s frontend build commit %q does not match backend commit %q", frontend, metadata.BuildCommit, backendCommit)
	}
	return nil
}

func normalizedBackendVersion() string {
	version := strings.TrimSpace(common.Version)
	if version == "unknown" {
		return ""
	}
	return version
}

func normalizedBackendCommit() string {
	commit := strings.TrimSpace(common.BuildCommit)
	if commit == "unknown" {
		return ""
	}
	return commit
}
