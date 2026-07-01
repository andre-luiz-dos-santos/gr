package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanReaderSingleRecord(t *testing.T) {
	input := "Acct-Session-Id = \"abc\"\nUser-Name = \"alice\"\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, []string{"alice"}, false, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if !matched {
		t.Fatal("scanReader did not report a match")
	}
	if out.String() != input {
		t.Fatalf("output mismatch\nwant: %q\ngot:  %q", input, out.String())
	}
}

func TestScanReaderMultipleRecords(t *testing.T) {
	input := strings.Join([]string{
		"Acct-Session-Id = \"one\"",
		"User-Name = \"alice\"",
		"",
		"Acct-Session-Id = \"two\"",
		"User-Name = \"bob\"",
		"",
	}, "\n")
	want := "Acct-Session-Id = \"two\"\nUser-Name = \"bob\"\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, []string{"bob"}, false, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if !matched {
		t.Fatal("scanReader did not report a match")
	}
	if out.String() != want {
		t.Fatalf("output mismatch\nwant: %q\ngot:  %q", want, out.String())
	}
}

func TestScanReaderTrailingRecordWithoutBlankLine(t *testing.T) {
	input := "Acct-Session-Id = \"last\"\nUser-Name = \"carol\""
	want := input + "\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, []string{"last"}, false, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if !matched {
		t.Fatal("scanReader did not report a match")
	}
	if out.String() != want {
		t.Fatalf("output mismatch\nwant: %q\ngot:  %q", want, out.String())
	}
}

func TestScanReaderMultipleBlankLinesBetweenRecords(t *testing.T) {
	input := "Acct-Session-Id = \"one\"\nUser-Name = \"alice\"\n\n\nAcct-Session-Id = \"two\"\nUser-Name = \"bob\"\n\n\nAcct-Session-Id = \"three\"\nUser-Name = \"alice\"\n"
	want := "Acct-Session-Id = \"one\"\nUser-Name = \"alice\"\n\nAcct-Session-Id = \"three\"\nUser-Name = \"alice\"\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, []string{"alice"}, false, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if !matched {
		t.Fatal("scanReader did not report a match")
	}
	if out.String() != want {
		t.Fatalf("output mismatch\nwant: %q\ngot:  %q", want, out.String())
	}
}

func TestScanReaderNonMatch(t *testing.T) {
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader("User-Name = \"alice\"\n"), &out, []string{"bob"}, false, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if matched {
		t.Fatal("scanReader reported a match")
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output, got %q", out.String())
	}
}

func TestScanReaderCaseInsensitive(t *testing.T) {
	input := "User-Name = \"Alice\"\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, []string{"alice"}, true, false)
	if err != nil {
		t.Fatalf("scanReader returned error: %v", err)
	}
	if !matched {
		t.Fatal("scanReader did not report a match")
	}
	if out.String() != input {
		t.Fatalf("output mismatch\nwant: %q\ngot:  %q", input, out.String())
	}
}

func TestRunDiscoversDetailFilesWhenNoFilesProvided(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "detail-20260319"), []byte("User-Name = \"alice\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "auth-detail-20250703"), []byte("User-Name = \"bob\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "first.detail"), []byte("User-Name = \"alice\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer

	code := run([]string{"-e", "alice", "-e", "bob"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	want := "User-Name = \"alice\"\n\nUser-Name = \"bob\"\n"
	if stdout.String() != want {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", want, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunReadsMultipleFilesInOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.detail")
	second := filepath.Join(dir, "second.detail")
	if err := os.WriteFile(first, []byte("User-Name = \"alice\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("User-Name = \"bob\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"-e", "alice", "-e", "bob", first, second}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	want := "User-Name = \"alice\"\n\nUser-Name = \"bob\"\n"
	if stdout.String() != want {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestRunReturnsNoMatchExitCode(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"missing"}, strings.NewReader("User-Name = \"alice\"\n"), &stdout, &stderr)
	if code != exitNoMatch {
		t.Fatalf("exit code mismatch: want %d, got %d", exitNoMatch, code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunRejectsEmptyPattern(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"-e", ""}, strings.NewReader(""), &stdout, &stderr)
	if code != exitUsageError {
		t.Fatalf("exit code mismatch: want %d, got %d", exitUsageError, code)
	}
	if !strings.Contains(stderr.String(), "empty pattern") {
		t.Fatalf("stderr does not mention empty pattern: %q", stderr.String())
	}
}

func TestRunHandlesFileReadErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"alice", filepath.Join(t.TempDir(), "missing.detail")}, strings.NewReader(""), &stdout, &stderr)
	if code != exitNoMatch {
		t.Fatalf("exit code mismatch: want %d, got %d", exitNoMatch, code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing.detail") {
		t.Fatalf("stderr does not mention missing file: %q", stderr.String())
	}
}
