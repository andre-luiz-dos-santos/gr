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
	patterns     []string
	files        []string
	ignoreCase   bool
	usageHandled bool
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
	if len(files) == 0 {
		var err error
		files, err = discoverDetailFiles(".")
		if err != nil {
			fmt.Fprintf(stderr, ".: %v\n", err)
			hadReadError = true
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

		fileMatched, err := scanReader(file, stdout, cfg.patterns, cfg.ignoreCase, printedAny)
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

	fs := flag.NewFlagSet("gr", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Var(&expressions, "e", "fixed string to search for; may be repeated")
	fs.BoolVar(&cfg.ignoreCase, "i", false, "ignore case distinctions")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gr [flags] PATTERN [FILE ...]")
		fmt.Fprintln(fs.Output(), "       gr [flags] -e PATTERN [-e PATTERN ...] [FILE ...]")
		fmt.Fprintln(fs.Output(), "If no FILE is provided, recursively scans files with detail- in their name.")
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
	if len(expressions) > 0 {
		cfg.patterns = expressions
		cfg.files = remaining
	} else {
		if len(remaining) == 0 {
			fs.Usage()
			return cfg, errors.New("missing pattern")
		}
		cfg.patterns = []string{remaining[0]}
		cfg.files = remaining[1:]
	}

	for _, pattern := range cfg.patterns {
		if pattern == "" {
			fs.Usage()
			return cfg, errors.New("empty pattern")
		}
	}

	return cfg, nil
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

func scanReader(r io.Reader, output io.Writer, patterns []string, ignoreCase bool, prefixBlankLine bool) (bool, error) {
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

		if !recordMatches(text, patterns, ignoreCase) {
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

func recordMatches(record string, patterns []string, ignoreCase bool) bool {
	if ignoreCase {
		record = strings.ToLower(record)
	}

	for _, pattern := range patterns {
		if ignoreCase {
			pattern = strings.ToLower(pattern)
		}
		if strings.Contains(record, pattern) {
			return true
		}
	}
	return false
}
