PHONY: build push lint test test-pouchdb test-build-tags

test:
	go test -json -race -v ./... | gotestfmt

test-pouchdb:
	go test -v -race -run TestPouchDBCompat ./test/pouchdb/

test-build-tags:
	@for tags in "" "nogoja" "notengo" "nosearch" "nojwt" "nototp" "nogoja,nosearch,notengo,nojwt,nototp"; do \
		label=$${tags:-default}; \
		echo "=== build-tags: $$label ==="; \
		go test -tags "$$tags" -json -race -v ./... | gotestfmt || exit 1; \
	done

lint:
	golangci-lint run

build:
	docker build -t goydb/goydb:latest .

push:
	docker push goydb/goydb:latest

public/favicon.ico: media/goydb.png
	convert $< -resize 32x32 $@

media/goydb_no_back_small.png: media/goydb_no_back.png
	convert $< -resize 50% $@
