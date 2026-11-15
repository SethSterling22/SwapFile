# FileSwap CLI — build helpers. Binaries are static (CGO_ENABLED=0), so the
# same file runs on Arch, Debian and any other Linux of that architecture.

# Packages need a version that starts with a digit, so without a v* tag use v0.0.0-<commit>.
VERSION ?= $(shell git describe --tags --match 'v*' --dirty 2>/dev/null || echo v0.0.0-$$(git rev-parse --short HEAD 2>/dev/null || echo dev))
ARCHES  ?= amd64 arm64
LDFLAGS := -s -w -X main.version=$(VERSION)
GO_IMAGE ?= golang:1

.PHONY: build test dist package release docker-build docker-test docker-package docker-release clean

build:                ## Build ./dist/fileswap for this machine
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/fileswap ./cmd/fileswap

test:                 ## Run vet and tests
	go vet ./...
	go test ./...

dist:                 ## Cross-build dist/fileswap-linux-<arch> for $(ARCHES)
	@for a in $(ARCHES); do \
	  echo "building linux/$$a"; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags '$(LDFLAGS)' \
	    -o dist/fileswap-linux-$$a ./cmd/fileswap || exit 1; \
	done

package: dist         ## Build .deb and Arch packages with nfpm (go run, no install needed)
	@mkdir -p dist/pkg
	@for a in $(ARCHES); do \
	  cp dist/fileswap-linux-$$a dist/pkg/fileswap || exit 1; \
	  for f in deb archlinux; do \
	    VERSION=$(patsubst v%,%,$(VERSION)) ARCH=$$a go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.0 \
	      package -f packaging/nfpm.yaml -p $$f -t dist/ || exit 1; \
	  done; \
	done

release: package       ## + tar.gz per arch and SHA256SUMS in dist/release (what GitHub Releases gets)
	@rm -rf dist/release && mkdir -p dist/release
	@for a in $(ARCHES); do \
	  d=dist/tmp/fileswap_$(patsubst v%,%,$(VERSION))_linux_$$a; \
	  mkdir -p $$d && cp dist/fileswap-linux-$$a $$d/fileswap && cp README.md ../LICENSE $$d/ || exit 1; \
	  tar -C dist/tmp -czf dist/release/$$(basename $$d).tar.gz $$(basename $$d) || exit 1; \
	done
	@rm -rf dist/tmp
	@cp dist/*.deb dist/*.pkg.tar.zst dist/release/
	@cd dist/release && sha256sum * > SHA256SUMS && cat SHA256SUMS

# Same targets without a local Go install, using the official Go image.
# Module and build caches live in .cache/ so repeated runs don't download again.
DOCKER_GO = docker run --rm -u $$(id -u):$$(id -g) -e HOME=/tmp \
	-e GOCACHE=/src/fileswap-cli/.cache/build -e GOMODCACHE=/src/fileswap-cli/.cache/mod -e GOPATH=/tmp/go \
	-e FILESWAP_TEST_SERVER --network host \
	-v "$(CURDIR)/..":/src -w /src/fileswap-cli $(GO_IMAGE)

docker-build:         ## make build, inside Docker
	$(DOCKER_GO) make build VERSION=$(VERSION)

docker-test:          ## make test, inside Docker
	$(DOCKER_GO) make test

docker-package:       ## make package, inside Docker
	$(DOCKER_GO) make package VERSION=$(VERSION)

docker-release:       ## make release, inside Docker
	$(DOCKER_GO) make release VERSION=$(VERSION)

clean:
	rm -rf dist
