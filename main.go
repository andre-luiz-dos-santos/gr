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
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	exitMatch      = 0
	exitNoMatch    = 1
	exitUsageError = 2

	highlightStart = "\x1b[1;31m"
	highlightEnd   = "\x1b[0m"
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
	filters          []searchFilter
	files            []string
	ignoreCase       bool
	sortOutput       bool
	usageHandled     bool
	macFileCandidate string
}

type searchFilter struct {
	alternatives []searchPattern
}

type searchPattern struct {
	text       string
	ignoreCase bool
}

type scanOptions struct {
	prefixBlankLine bool
	highlight       bool
}

type matchedRecord struct {
	text         string
	timestamp    string
	index        int
	hasTimestamp bool
}

type byteRange struct {
	start int
	end   int
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
			cfg.filters = cfg.filters[1:]
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

	highlight := outputIsTerminal(stdout)
	if cfg.sortOutput {
		records, hadSortedReadError := collectSortedMatches(files, stderr, cfg.filters)
		if hadSortedReadError {
			hadReadError = true
		}
		if len(records) > 0 {
			matched = true
		}
		sortMatchedRecords(records)
		if err := printMatchedRecords(records, stdout, cfg.filters, highlight); err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			hadReadError = true
		}
		if !matched || hadReadError {
			return exitNoMatch
		}
		return exitMatch
	}

	printedAny := false
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			hadReadError = true
			continue
		}

		fileMatched, err := scanReader(file, stdout, cfg.filters, scanOptions{
			prefixBlankLine: printedAny,
			highlight:       highlight,
		})
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

func outputIsTerminal(output io.Writer) bool {
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
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
	fs.BoolVar(&cfg.sortOutput, "sort", false, "sort matching records by Timestamp after reading all files")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gr [flags] PATTERN [FILE ...]")
		fmt.Fprintln(fs.Output(), "       gr [flags] -e PATTERN [-e PATTERN ...] [FILE ...]")
		fmt.Fprintln(fs.Output(), "       gr [flags] -mac MAC [-mac MAC ...] [FILE ...]")
		fmt.Fprintln(fs.Output(), "Records must match every supplied pattern; each -mac accepts any supported MAC format.")
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
		cfg.filters = append(cfg.filters, searchFilter{
			alternatives: []searchPattern{{
				text:       pattern,
				ignoreCase: cfg.ignoreCase,
			}},
		})
	}

	for _, mac := range macs {
		variants, err := macVariants(mac)
		if err != nil {
			fs.Usage()
			return cfg, err
		}
		filter := searchFilter{
			alternatives: make([]searchPattern, 0, len(variants)),
		}
		seenMACPattern := make(map[string]bool)
		for _, variant := range variants {
			key := strings.ToLower(variant)
			if seenMACPattern[key] {
				continue
			}
			seenMACPattern[key] = true
			filter.alternatives = append(filter.alternatives, searchPattern{
				text:       variant,
				ignoreCase: true,
			})
		}
		cfg.filters = append(cfg.filters, filter)
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

func scanReader(r io.Reader, output io.Writer, filters []searchFilter, options scanOptions) (bool, error) {
	matched := false
	printedAny := options.prefixBlankLine

	err := scanRecords(r, func(text string) error {
		if !recordMatches(text, filters) {
			return nil
		}
		if printedAny {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		if options.highlight {
			text = highlightMatches(text, filters)
		}
		if _, err := fmt.Fprintln(output, text); err != nil {
			return err
		}
		printedAny = true
		matched = true
		return nil
	})
	return matched, err
}

func scanRecords(r io.Reader, handle func(string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var record []string

	flush := func() error {
		if len(record) == 0 {
			return nil
		}
		text := strings.Join(record, "\n")
		record = record[:0]

		return handle(text)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		record = append(record, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	return nil
}

func collectSortedMatches(files []string, stderr io.Writer, filters []searchFilter) ([]matchedRecord, bool) {
	var records []matchedRecord
	hadReadError := false
	nextIndex := 0

	for _, name := range files {
		fmt.Fprintf(stderr, "Reading %s (matches so far: %d)\n", name, len(records))

		file, err := os.Open(name)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			hadReadError = true
			continue
		}

		fileRecords, err := collectMatchingRecords(file, filters, &nextIndex)
		records = append(records, fileRecords...)
		closeErr := file.Close()
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			hadReadError = true
		}
		if closeErr != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, closeErr)
			hadReadError = true
		}
	}

	return records, hadReadError
}

func collectMatchingRecords(r io.Reader, filters []searchFilter, nextIndex *int) ([]matchedRecord, error) {
	var records []matchedRecord
	err := scanRecords(r, func(text string) error {
		if !recordMatches(text, filters) {
			return nil
		}
		record := matchedRecord{
			text:  text,
			index: *nextIndex,
		}
		record.timestamp, record.hasTimestamp = timestampLine(text)
		records = append(records, record)
		*nextIndex = *nextIndex + 1
		return nil
	})
	return records, err
}

func timestampLine(record string) (string, bool) {
	for _, line := range strings.Split(record, "\n") {
		if strings.HasPrefix(line, "Timestamp") {
			return line, true
		}
	}
	return "", false
}

func sortMatchedRecords(records []matchedRecord) {
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if left.hasTimestamp != right.hasTimestamp {
			return left.hasTimestamp
		}
		if left.hasTimestamp && left.timestamp != right.timestamp {
			return left.timestamp < right.timestamp
		}
		return left.index < right.index
	})
}

func printMatchedRecords(records []matchedRecord, output io.Writer, filters []searchFilter, highlight bool) error {
	for i, record := range records {
		if i > 0 {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		text := record.text
		if highlight {
			text = highlightMatches(text, filters)
		}
		if _, err := fmt.Fprintln(output, text); err != nil {
			return err
		}
	}
	return nil
}

func highlightMatches(record string, filters []searchFilter) string {
	ranges := matchingRanges(record, flattenFilters(filters))
	if len(ranges) == 0 {
		return record
	}

	var b strings.Builder
	b.Grow(len(record) + len(ranges)*(len(highlightStart)+len(highlightEnd)))
	position := 0
	for _, match := range ranges {
		b.WriteString(record[position:match.start])
		b.WriteString(highlightStart)
		b.WriteString(record[match.start:match.end])
		b.WriteString(highlightEnd)
		position = match.end
	}
	b.WriteString(record[position:])
	return b.String()
}

func matchingRanges(record string, patterns []searchPattern) []byteRange {
	var candidates []byteRange
	var foldedRecord string
	var foldedMap []byteRange

	for _, pattern := range patterns {
		needle := pattern.text
		searchRecord := record
		var searchMap []byteRange

		if pattern.ignoreCase {
			if foldedRecord == "" {
				foldedRecord, foldedMap = foldWithByteMap(record)
			}
			searchRecord = foldedRecord
			searchMap = foldedMap
			needle, _ = foldWithByteMap(pattern.text)
		}
		if needle == "" {
			continue
		}

		offset := 0
		for offset <= len(searchRecord) {
			index := strings.Index(searchRecord[offset:], needle)
			if index < 0 {
				break
			}

			start := offset + index
			end := start + len(needle)
			if searchMap == nil {
				candidates = append(candidates, byteRange{start: start, end: end})
			} else {
				candidates = append(candidates, byteRange{
					start: searchMap[start].start,
					end:   searchMap[end-1].end,
				})
			}
			offset = start + 1
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].start != candidates[j].start {
			return candidates[i].start < candidates[j].start
		}
		return candidates[i].end > candidates[j].end
	})

	merged := candidates[:0]
	position := 0
	for _, candidate := range candidates {
		if candidate.start < position {
			continue
		}
		merged = append(merged, candidate)
		position = candidate.end
	}
	return merged
}

func foldWithByteMap(text string) (string, []byteRange) {
	var folded strings.Builder
	byteMap := make([]byteRange, 0, len(text))
	for start, r := range text {
		_, size := utf8.DecodeRuneInString(text[start:])
		end := start + size
		lower := strings.ToLower(string(r))
		folded.WriteString(lower)
		for i := 0; i < len(lower); i++ {
			byteMap = append(byteMap, byteRange{start: start, end: end})
		}
	}
	return folded.String(), byteMap
}

func recordMatches(record string, filters []searchFilter) bool {
	var lowerRecord string
	for _, filter := range filters {
		matched := false
		for _, pattern := range filter.alternatives {
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
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func flattenFilters(filters []searchFilter) []searchPattern {
	var patterns []searchPattern
	for _, filter := range filters {
		patterns = append(patterns, filter.alternatives...)
	}
	return patterns
}
