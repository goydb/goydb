PHONY: build push lint test

test:
	go test -race -v ./...

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint run

build:
	docker build -t goydb/goydb:latest .

push:
	docker push goydb/goydb:latest

public/favicon.ico: media/goydb.png
	convert $< -resize 32x32 $@

media/goydb_no_back_small.png: media/goydb_no_back.png
	convert $< -resize 50% $@
