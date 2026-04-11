VERSION := $(shell cat VERSION)
BINARY  := harness-factory
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build clean test test-e2e run version

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/harness-factory

clean:
	rm -f $(BINARY)

test:
	cd test && go test -count=1 -v ./...

test-e2e: build
	bash test/test_agent_compliance.sh ./$(BINARY)

run: build
	@echo "harness-factory $(VERSION) — stdin/stdout JSON-RPC"
	@./$(BINARY)

version:
	@cat VERSION
