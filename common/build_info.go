package common

import (
	"fmt"
	"strings"
)

func BuildInfoLines() []string {
	return []string{
		"version=" + normalizeBuildValue(Version),
		"commit=" + normalizeBuildValue(BuildCommit),
		"date=" + normalizeBuildValue(BuildDate),
		"source=" + normalizeBuildValue(BuildSource),
	}
}

func BuildSummary() string {
	version := normalizeBuildValue(Version)
	commit := normalizeBuildValue(BuildCommit)
	date := normalizeBuildValue(BuildDate)
	if commit == "unknown" && date == "unknown" {
		return version
	}
	return fmt.Sprintf("%s commit=%s built=%s", version, commit, date)
}

func normalizeBuildValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
