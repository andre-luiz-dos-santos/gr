package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	exitMatch      = 0
	exitNoMatch    = 1
	exitUsageError = 2
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type config struct {
	patterns         []searchPattern
	files            []string
	ignoreCase       bool
	usageHandled     bool
	macFileCandidate string
}

type searchPattern struct {
	text       string
	ignoreCase bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args, stderr)
	if err != nil {
		if cfg.usageHandled {
			return exitMatch
		}
		fmt.Fprintln(stderr, err)
		return exitUsageError
	}

	matched, hadReadError := false, false
	files := cfg.files
	if cfg.macFileCandidate != "" {
		matches, err := discoverFilePatternMatches(".", cfg.macFileCandidate)
		if err != nil {
			fmt.Fprintf(stderr, ".: %v\n", err)
			hadReadError = true
		} else if len(matches) > 0 {
			files = matches
			cfg.patterns = cfg.patterns[1:]
		}
	}
	if len(files) == 0 {
		var err error
		files, err = discoverDetailFiles(".")
		if err != nil {
			fmt.Fprintf(stderr, ".: %v\n", err)
			hadReadError = true
		}
	} else {
		var err error
		files, err = resolveFilePatterns(files, ".")
		if err != nil {
			fmt.Fprintf(stderr, ".: %v\n", err)
			hadReadError = true
			files = nil
		}
	}

	printedAny := false
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			hadReadError = true
			continue
		}

		fileMatched, err := scanReader(file, stdout, cfg.patterns, printedAny)
		closeErr := file.Close()
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			hadReadError = true
		}
		if closeErr != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, closeErr)
			hadReadError = true
		}
		if fileMatched {
			matched = true
			printedAny = true
		}
	}

	if !matched || hadReadError {
		return exitNoMatch
	}
	return exitMatch
}

func parseArgs(args []string, output io.Writer) (config, error) {
	var cfg config
	var expressions stringList
	var macs stringList

	fs := flag.NewFlagSet("gr", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Var(&expressions, "e", "fixed string to search for; may be repeated")
	fs.Var(&macs, "mac", "MAC address to search for in hyphen, colon, compact, or Cisco dotted format; may be repeated")
	fs.BoolVar(&cfg.ignoreCase, "i", false, "ignore case distinctions")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gr [flags] PATTERN [FILE ...]")
		fmt.Fprintln(fs.Output(), "       gr [flags] -e PATTERN [-e PATTERN ...] [FILE ...]")
		fmt.Fprintln(fs.Output(), "       gr [flags] -mac MAC [-mac MAC ...] [FILE ...]")
		fmt.Fprintln(fs.Output(), "If no FILE is provided, recursively scans files with detail- in their name.")
		fmt.Fprintln(fs.Output(), "Bare FILE arguments without a directory are matched recursively as basename glob patterns.")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			cfg.usageHandled = true
			return cfg, nil
		}
		return cfg, err
	}

	remaining := fs.Args()
	var normalPatterns []string
	if len(expressions) > 0 {
		normalPatterns = expressions
		cfg.files = remaining
	} else if len(macs) > 0 {
		if allExistingFiles(remaining) {
			cfg.files = remaining
		} else if len(remaining) > 0 {
			normalPatterns = []string{remaining[0]}
			if len(remaining) == 1 {
				cfg.macFileCandidate = remaining[0]
			}
			cfg.files = remaining[1:]
		}
	} else {
		if len(remaining) == 0 {
			fs.Usage()
			return cfg, errors.New("missing pattern")
		}
		normalPatterns = []string{remaining[0]}
		cfg.files = remaining[1:]
	}

	for _, pattern := range normalPatterns {
		if pattern == "" {
			fs.Usage()
			return cfg, errors.New("empty pattern")
		}
		cfg.patterns = append(cfg.patterns, searchPattern{
			text:       pattern,
			ignoreCase: cfg.ignoreCase,
		})
	}

	seenMACPattern := make(map[string]bool)
	for _, mac := range macs {
		variants, err := macVariants(mac)
		if err != nil {
			fs.Usage()
			return cfg, err
		}
		for _, variant := range variants {
			key := strings.ToLower(variant)
			if seenMACPattern[key] {
				continue
			}
			seenMACPattern[key] = true
			cfg.patterns = append(cfg.patterns, searchPattern{
				text:       variant,
				ignoreCase: true,
			})
		}
	}

	return cfg, nil
}

func allExistingFiles(paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func resolveFilePatterns(files []string, root string) ([]string, error) {
	var resolved []string
	for _, name := range files {
		if hasDirSeparator(name) {
			resolved = append(resolved, name)
			continue
		}

		matches, err := discoverFilePatternMatches(root, name)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			resolved = append(resolved, name)
			continue
		}
		resolved = append(resolved, matches...)
	}
	return resolved, nil
}

func hasDirSeparator(path string) bool {
	return strings.ContainsRune(path, os.PathSeparator)
}

func macVariants(mac string) ([]string, error) {
	compact := strings.NewReplacer("-", "", ":", "", ".", "").Replace(mac)
	if compact == "" {
		return nil, errors.New("empty MAC address")
	}
	if len(compact) != 12 {
		return nil, fmt.Errorf("invalid MAC address %q: expected 12 hex characters", mac)
	}
	for i := 0; i < len(compact); i++ {
		if !isHexDigit(compact[i]) {
			return nil, fmt.Errorf("invalid MAC address %q: contains non-hex character", mac)
		}
	}

	compact = strings.ToUpper(compact)
	return []string{
		formatMAC(compact, "-", 2),
		formatMAC(compact, ":", 2),
		compact,
		formatMAC(compact, ".", 4),
	}, nil
}

func isHexDigit(ch byte) bool {
	return ('0' <= ch && ch <= '9') || ('a' <= ch && ch <= 'f') || ('A' <= ch && ch <= 'F')
}

func formatMAC(compact, separator string, groupSize int) string {
	var b strings.Builder
	for i := 0; i < len(compact); i += groupSize {
		if i > 0 {
			b.WriteString(separator)
		}
		b.WriteString(compact[i : i+groupSize])
	}
	return b.String()
}

func discoverDetailFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.Contains(entry.Name(), "detail-") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func discoverFilePatternMatches(root, pattern string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			return err
		}
		if matched {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func scanReader(r io.Reader, output io.Writer, patterns []searchPattern, prefixBlankLine bool) (bool, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	matched := false
	printedAny := prefixBlankLine
	var record []string

	flush := func() error {
		if len(record) == 0 {
			return nil
		}
		text := strings.Join(record, "\n")
		record = record[:0]

		if !recordMatches(text, patterns) {
			return nil
		}
		if printedAny {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output, text); err != nil {
			return err
		}
		printedAny = true
		matched = true
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return matched, err
			}
			continue
		}
		record = append(record, line)
	}
	if err := scanner.Err(); err != nil {
		return matched, err
	}
	if err := flush(); err != nil {
		return matched, err
	}

	return matched, nil
}

func recordMatches(record string, patterns []searchPattern) bool {
	var lowerRecord string
	for _, pattern := range patterns {
		recordToSearch := record
		patternToSearch := pattern.text
		if pattern.ignoreCase {
			if lowerRecord == "" {
				lowerRecord = strings.ToLower(record)
			}
			recordToSearch = lowerRecord
			patternToSearch = strings.ToLower(pattern.text)
		}
		if strings.Contains(recordToSearch, patternToSearch) {
			return true
		}
	}
	return false
}
