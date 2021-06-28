# Build

.PHONY: go-releasing
go-releasing:
	cd go/tools/releasing && \
	go build -o releasing main.go