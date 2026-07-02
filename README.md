# Grep Radacct

`gr` is a small Go CLI for searching RADIUS accounting detail files. It scans
blank-line-separated records and prints the full records that match every
supplied fixed-string filter.

## Features

- Search one or more strings with `PATTERN` or repeated `-e` flags.
- Search MAC addresses with `-mac` in hyphen, colon, compact, or Cisco dotted
  format.
- Recursively discover files with `detail-` in the name when no file is given.
- Expand bare file names or globs recursively, such as `detail-*`.
- Sort matching records by `Timestamp` with `-sort`.
- Highlight matches automatically when writing to a terminal.

## Build

```sh
make
```

This builds the `gr` binary in the repository root. To run tests:

```sh
go test ./...
```

## Usage

```sh
gr [flags] PATTERN [FILE ...]
gr [flags] -e PATTERN [-e PATTERN ...] [FILE ...]
gr [flags] -mac MAC [-mac MAC ...] [FILE ...]
```

Examples:

```sh
./gr alice
./gr -e alice -e session-123 detail-20260701
./gr -mac d8:36:5f:d2:d3:7c detail-*
./gr -i -sort -e alice -mac d836.5fd2.d37c
```

Exit codes are `0` for matches, `1` for no matches or read errors, and `2` for
usage errors.
