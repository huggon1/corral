BINARY := corral
DIST := dist

.PHONY: build test cross-build clean

build:
	go build -trimpath -ldflags="-s -w" -o $(DIST)/$(BINARY) ./cmd/corral

test:
	go test ./... -count=1

cross-build:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o $(DIST)/$(BINARY)-darwin-arm64 ./cmd/corral
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o $(DIST)/$(BINARY)-darwin-amd64 ./cmd/corral
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(DIST)/$(BINARY)-linux-amd64 ./cmd/corral
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o $(DIST)/$(BINARY)-linux-arm64 ./cmd/corral
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o $(DIST)/$(BINARY)-windows-amd64.exe ./cmd/corral

clean:
	go clean
