package logger

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCleanupOldLogsRemovesOnlyStaleRotatedFiles(t *testing.T) {
	dir := t.TempDir()

	stale := filepath.Join(dir, "oneapi-20200101000000.log")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(dir, "oneapi-20260102030405.log")
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	unrelated := filepath.Join(dir, "other-20200101000000.log")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unrelated, past, past); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(dir, "oneapi-sub.log"), 0o755); err != nil {
		t.Fatal(err)
	}

	currentLogPathMu.Lock()
	prev := currentLogPath
	currentLogPath = fresh
	currentLogPathMu.Unlock()
	defer func() {
		currentLogPathMu.Lock()
		currentLogPath = prev
		currentLogPathMu.Unlock()
	}()

	cleanupOldLogs(dir, 7)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale log not removed: err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh log removed: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "oneapi-sub.log")); err != nil {
		t.Fatalf("directory removed: %v", err)
	}
}

func TestLogRetentionDaysParsing(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"-3", 0},
		{"abc", 0},
		{"7", 7},
	}
	for _, c := range cases {
		t.Setenv("LOG_RETENTION_DAYS", c.env)
		got := func() int {
			days, err := strconv.Atoi(strings.TrimSpace(os.Getenv("LOG_RETENTION_DAYS")))
			if err != nil || days <= 0 {
				return 0
			}
			return days
		}()
		if got != c.want {
			t.Errorf("LOG_RETENTION_DAYS=%q: got %d, want %d", c.env, got, c.want)
		}
	}
}
