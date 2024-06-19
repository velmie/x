.PHONY: tests linter

tests:
	CGO_ENABLED=0 go test -cover ./...

linter:
	golangci-lint run ./...
