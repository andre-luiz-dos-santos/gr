BINARY ?= gr

.PHONY: all cgo0 clean

all: cgo0

cgo0:
	CGO_ENABLED=0 go build -o $(BINARY) .

clean:
	rm -f $(BINARY)
