.PHONY: build
build:
	go build -o build/sock-vmnet cmd/main.go

test:
	go test -tags unit -v -race -cover ./...

LINT_VERSION:=v1.53.3
install-linter:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(LINT_VERSION)

lint:
	golangci-lint run ./...
