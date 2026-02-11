VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build release clean

build:
	go build -ldflags "$(LDFLAGS)" -o opcode ./cmd/opcode/

release: clean
	@mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/opcode_darwin_arm64/opcode ./cmd/opcode/
	tar -czf dist/opcode_Darwin_arm64.tar.gz -C dist/opcode_darwin_arm64 opcode
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/opcode_darwin_amd64/opcode ./cmd/opcode/
	tar -czf dist/opcode_Darwin_x86_64.tar.gz -C dist/opcode_darwin_amd64 opcode
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/opcode_linux_arm64/opcode ./cmd/opcode/
	tar -czf dist/opcode_Linux_arm64.tar.gz -C dist/opcode_linux_arm64 opcode
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/opcode_linux_amd64/opcode ./cmd/opcode/
	tar -czf dist/opcode_Linux_x86_64.tar.gz -C dist/opcode_linux_amd64 opcode
	@cd dist && shasum -a 256 *.tar.gz > checksums.txt
	@echo "Release artifacts in dist/"

clean:
	rm -rf dist/ opcode
