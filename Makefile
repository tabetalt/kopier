.PHONY: build
build:
	GOOS=linux GOARCH=amd64 go build -o dist/update-repos cmd/devops-build-tools/main.go