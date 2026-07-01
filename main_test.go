package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPatterns(ignoreCase bool, values ...string) []searchPattern {
	patterns := make([]searchPattern, 0, len(values))
	for _, value := range values {
		patterns = append(patterns, searchPattern{text: value, ignoreCase: ignoreCase})
	}
	return patterns
}

func TestScanReaderSingleRecord(t *testing.T) {
	input := "Acct-Session-Id = \"abc\"\nUser-Name = \"alice\"\n"
	var out bytes.Buffer

	matched, err := scanReader(strings.NewReader(input), &out, testPatterns(false, "alice"), false)
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

	matched, err := scanReader(strings.NewReader(input), &out, testPatterns(false, "bob"), false)
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

	matched, err := scanReader(strings.NewReader(input), &out, testPatterns(false, "last"), false)
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

	matched, err := scanReader(strings.NewReader(input), &out, testPatterns(false, "alice"), false)
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

	matched, err := scanReader(strings.NewReader("User-Name = \"alice\"\n"), &out, testPatterns(false, "bob"), false)
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

	matched, err := scanReader(strings.NewReader(input), &out, testPatterns(true, "alice"), false)
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

func TestRunMACSearchMatchesFormatsCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "detail-20260701")
	records := []string{
		"Calling-Station-Id = \"D8-36-5F-D2-D3-7C\"",
		"Calling-Station-Id = \"d8-36-5f-d2-d3-7c\"",
		"Calling-Station-Id = \"d8:36:5f:d2:d3:7c\"",
		"Calling-Station-Id = \"D8365FD2D37C\"",
		"Calling-Station-Id = \"d836.5fd2.d37c\"",
		"Calling-Station-Id = \"D8:36:5f:D2:d3:7C\"",
	}
	input := strings.Join(records, "\n\n") + "\n"
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"-mac", "D8-36-5F-D2-D3-7C", file}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	if stdout.String() != input {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", input, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr, got %q", stderr.String())
	}
}

func TestRunMACSearchAcceptsInputFormats(t *testing.T) {
	for _, mac := range []string{
		"D8:36:5F:D2:D3:7C",
		"D8365FD2D37C",
		"D836.5FD2.D37C",
	} {
		t.Run(mac, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "detail-20260701")
			input := "Calling-Station-Id = \"d8-36-5f-d2-d3-7c\"\n"
			if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer

			code := run([]string{"-mac", mac, file}, strings.NewReader(""), &stdout, &stderr)
			if code != exitMatch {
				t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
			}
			if stdout.String() != input {
				t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", input, stdout.String())
			}
		})
	}
}

func TestRunMACSearchWithoutPositionalPattern(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	input := "Calling-Station-Id = \"D836.5FD2.D37C\"\n"
	if err := os.WriteFile(filepath.Join(dir, "detail-20260701"), []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"-mac", "D8-36-5F-D2-D3-7C"}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	if stdout.String() != input {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", input, stdout.String())
	}
}

func TestRunMACSearchIsAdditiveWithExpressions(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "detail-20260701")
	input := strings.Join([]string{
		"User-Name = \"alice\"",
		"",
		"Calling-Station-Id = \"D836.5FD2.D37C\"",
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	code := run([]string{"-e", "alice", "-mac", "D8-36-5F-D2-D3-7C", file}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	want := "User-Name = \"alice\"\n\nCalling-Station-Id = \"D836.5FD2.D37C\"\n"
	if stdout.String() != want {
		t.Fatalf("stdout mismatch\nwant: %q\ngot:  %q", want, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"-mac", "D8-36-5F-D2-D3-7C", "alice", file}, strings.NewReader(""), &stdout, &stderr)
	if code != exitMatch {
		t.Fatalf("exit code mismatch with positional pattern: want %d, got %d; stderr=%q", exitMatch, code, stderr.String())
	}
	if stdout.String() != want {
		t.Fatalf("stdout mismatch with positional pattern\nwant: %q\ngot:  %q", want, stdout.String())
	}
}

func TestRunRejectsInvalidMAC(t *testing.T) {
	for _, tc := range []struct {
		name string
		mac  string
		want string
	}{
		{name: "empty", mac: "", want: "empty MAC address"},
		{name: "short", mac: "D8-36", want: "expected 12 hex characters"},
		{name: "non_hex", mac: "D8-36-5F-D2-D3-ZC", want: "contains non-hex character"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run([]string{"-mac", tc.mac}, strings.NewReader(""), &stdout, &stderr)
			if code != exitUsageError {
				t.Fatalf("exit code mismatch: want %d, got %d", exitUsageError, code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("expected no stdout, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr does not mention %q: %q", tc.want, stderr.String())
			}
		})
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
