package logx

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func TestNewAccessRotateWriterValidation(t *testing.T) {
	_, err := NewAccessRotateWriter(AccessLogRotateOptions{Path: "", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1})
	if err == nil {
		t.Fatalf("expected error for empty path")
	}

	_, err = NewAccessRotateWriter(AccessLogRotateOptions{Path: "./a.log", MaxSizeMB: 0, MaxBackups: 1, MaxAgeDays: 1})
	if err == nil {
		t.Fatalf("expected error for invalid max_size_mb")
	}

	_, err = NewAccessRotateWriter(AccessLogRotateOptions{Path: "./a.log", MaxSizeMB: 1, MaxBackups: 0, MaxAgeDays: 1})
	if err == nil {
		t.Fatalf("expected error for invalid max_backups")
	}

	_, err = NewAccessRotateWriter(AccessLogRotateOptions{Path: "./a.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: -1})
	if err == nil {
		t.Fatalf("expected error for invalid max_age_days")
	}
}

func TestAccessRotateWriterRotateBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	clock := &testClock{now: time.Date(2026, 2, 1, 12, 0, 0, 1, time.Local)}

	w, err := NewAccessRotateWriter(AccessLogRotateOptions{
		Path:       path,
		MaxSizeMB:  1,
		MaxBackups: 10,
		MaxAgeDays: 14,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewAccessRotateWriter err=%v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	first := strings.Repeat("a", 900*1024)
	second := strings.Repeat("b", 300*1024)
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatalf("first write err=%v", err)
	}
	if _, err := w.Write([]byte(second)); err != nil {
		t.Fatalf("second write err=%v", err)
	}

	archives := listArchives(t, dir)
	if len(archives) != 1 {
		t.Fatalf("expected 1 archive, got %d (%v)", len(archives), archives)
	}
}

func TestAccessRotateWriterRotateByDay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	clock := &testClock{now: time.Date(2026, 2, 1, 23, 59, 0, 123, time.Local)}

	w, err := NewAccessRotateWriter(AccessLogRotateOptions{
		Path:       path,
		MaxSizeMB:  100,
		MaxBackups: 10,
		MaxAgeDays: 14,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewAccessRotateWriter err=%v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.Write([]byte("day1\n")); err != nil {
		t.Fatalf("write day1 err=%v", err)
	}
	clock.now = clock.now.AddDate(0, 0, 1)
	if _, err := w.Write([]byte("day2\n")); err != nil {
		t.Fatalf("write day2 err=%v", err)
	}

	archives := listArchives(t, dir)
	if len(archives) != 1 {
		t.Fatalf("expected 1 archive, got %d (%v)", len(archives), archives)
	}
}

func TestAccessRotateWriterCleanupBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	clock := &testClock{now: time.Date(2026, 1, 1, 10, 0, 0, 1, time.Local)}

	w, err := NewAccessRotateWriter(AccessLogRotateOptions{
		Path:       path,
		MaxSizeMB:  100,
		MaxBackups: 2,
		MaxAgeDays: 0,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewAccessRotateWriter err=%v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	for i := 0; i < 5; i++ {
		if _, err := fmt.Fprintf(w, "d-%d\n", i); err != nil {
			t.Fatalf("write #%d err=%v", i, err)
		}
		if i < 4 {
			clock.now = clock.now.AddDate(0, 0, 1)
		}
	}

	archives := listArchives(t, dir)
	if len(archives) != 2 {
		t.Fatalf("expected 2 archives after cleanup, got %d (%v)", len(archives), archives)
	}
}

func TestAccessRotateWriterCleanupAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	clock := &testClock{now: time.Date(2026, 1, 1, 10, 0, 0, 1, time.Local)}

	w, err := NewAccessRotateWriter(AccessLogRotateOptions{
		Path:       path,
		MaxSizeMB:  100,
		MaxBackups: 20,
		MaxAgeDays: 2,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewAccessRotateWriter err=%v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	for i := 0; i < 6; i++ {
		if _, err := fmt.Fprintf(w, "d-%d\n", i); err != nil {
			t.Fatalf("write #%d err=%v", i, err)
		}
		if i < 5 {
			clock.now = clock.now.AddDate(0, 0, 1)
		}
	}

	archives := listArchives(t, dir)
	if len(archives) != 3 {
		t.Fatalf("expected 3 archives after age cleanup, got %d (%v)", len(archives), archives)
	}
	cutoff := clock.now.AddDate(0, 0, -2)
	for _, name := range archives {
		when, ok := parseArchiveTime(name, "access.log")
		if !ok {
			t.Fatalf("invalid archive name: %s", name)
		}
		if when.Before(cutoff) {
			t.Fatalf("archive %s should have been removed by age cutoff=%s", name, cutoff.Format(time.RFC3339))
		}
	}
}

func TestAccessRotateWriterCompressArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	clock := &testClock{now: time.Date(2026, 2, 1, 10, 0, 0, 1, time.Local)}

	w, err := NewAccessRotateWriter(AccessLogRotateOptions{
		Path:       path,
		MaxSizeMB:  100,
		MaxBackups: 10,
		MaxAgeDays: 14,
		Compress:   true,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewAccessRotateWriter err=%v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	if _, err := w.Write([]byte("line-day1\n")); err != nil {
		t.Fatalf("write day1 err=%v", err)
	}
	clock.now = clock.now.AddDate(0, 0, 1)
	if _, err := w.Write([]byte("line-day2\n")); err != nil {
		t.Fatalf("write day2 err=%v", err)
	}

	archives := listArchives(t, dir)
	if len(archives) != 1 {
		t.Fatalf("expected 1 archive, got %d (%v)", len(archives), archives)
	}
	if !strings.HasSuffix(archives[0], ".gz") {
		t.Fatalf("expected gz archive, got %s", archives[0])
	}

	f, err := os.Open(filepath.Join(dir, archives[0]))
	if err != nil {
		t.Fatalf("open archive err=%v", err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close archive file: %v", closeErr)
		}
	})

	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader err=%v", err)
	}
	t.Cleanup(func() {
		if closeErr := zr.Close(); closeErr != nil {
			t.Errorf("close gzip reader: %v", closeErr)
		}
	})
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gz err=%v", err)
	}
	if !strings.Contains(string(body), "line-day1") {
		t.Fatalf("unexpected gz body: %q", string(body))
	}
}

func listArchives(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir err=%v", err)
	}
	out := make([]string, 0)
	prefix := "access.log."
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out
}

func parseArchiveTime(name string, base string) (time.Time, bool) {
	prefix := base + "."
	if !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}
	ts := strings.TrimPrefix(name, prefix)
	ts = strings.TrimSuffix(ts, ".gz")
	parsed, err := time.ParseInLocation(archiveTimeLayout, ts, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
