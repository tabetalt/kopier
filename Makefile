.PHONY: build
build:
	GOOS=linux GOARCH=amd64 go build -o dist/update-repos main.go