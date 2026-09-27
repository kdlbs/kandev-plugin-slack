.PHONY: build run test test-go test-go-minimum-host test-package-verifier \
	test-release-version fmt \
	check-format check-sdk-pin vet package package-host verify-package \
	vet-minimum-host verify-package-host package-file clean

BIN := bin/kandev-plugin-slack
VERSION := 0.2.0
STAGE := .build/stage
PKG_OUT := $(notdir $(BIN))-$(VERSION).tar.gz

# Keep this checkout beside the plugin. go.mod uses the same ../kandev path.
KANDEV_BACKEND ?= ../kandev/apps/backend

## Build the plugin binary for the host platform. Installed plugins use a package.
build:
	mkdir -p bin
	go build -o $(BIN) ./server

## Build and run the plugin. A Kandev host normally starts it over go-plugin.
run: build
	./$(BIN)

test: test-go test-package-verifier test-release-version

test-go:
	go test ./server

test-go-minimum-host:
	go test -tags=kandev_min_host ./server

test-package-verifier:
	sh scripts/test-verify-package.sh

test-release-version:
	sh scripts/test-verify-release-version.sh

fmt:
	gofmt -w ./server

check-format:
	@test -z "$$(gofmt -l ./server)" || { echo "gofmt needed:"; gofmt -l ./server; exit 1; }

check-sdk-pin:
	@set -eu; \
		expected="$$(tr -d '\n' < .kandev-sdk-ref)"; \
		printf '%s\n' "SDK pin: $$expected"; \
		case "$$expected" in *[!0-9a-f]*|'') echo "invalid .kandev-sdk-ref" >&2; exit 1;; esac; \
		test "$${#expected}" -eq 40 || { echo "SDK pin must contain 40 hex characters" >&2; exit 1; }; \
		actual="$$(git -C ../kandev rev-parse HEAD)"; \
		test "$$actual" = "$$expected" || { echo "Kandev checkout $$actual differs from SDK pin $$expected" >&2; exit 1; }

vet:
	go vet ./server/...

vet-minimum-host:
	go vet -tags=kandev_min_host ./server/...

## Build every platform declared in manifest.yaml and pack its runtime files.
package:
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server $(STAGE)/ui
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r assets $(STAGE)/assets
	cp README.md $(STAGE)/README.md
	cp -r docs $(STAGE)/docs
	cp slack-app-manifest.yaml $(STAGE)/slack-app-manifest.yaml
	cp ui/bundle.js $(STAGE)/ui/bundle.js
	GOOS=linux   GOARCH=amd64 go build -o $(STAGE)/server/plugin-linux-amd64       ./server
	GOOS=linux   GOARCH=arm64 go build -o $(STAGE)/server/plugin-linux-arm64       ./server
	GOOS=darwin  GOARCH=amd64 go build -o $(STAGE)/server/plugin-darwin-amd64      ./server
	GOOS=darwin  GOARCH=arm64 go build -o $(STAGE)/server/plugin-darwin-arm64      ./server
	GOOS=windows GOARCH=amd64 go build -o $(STAGE)/server/plugin-windows-amd64.exe ./server
	cd $(KANDEV_BACKEND) && go run ./cmd/plugin-pack -dir $(CURDIR)/$(STAGE) -out $(CURDIR)/$(PKG_OUT)
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Package only the current host platform for local installation checks.
package-host:
	rm -rf $(STAGE)
	mkdir -p $(STAGE)/server $(STAGE)/ui
	cp manifest.yaml $(STAGE)/manifest.yaml
	cp -r assets $(STAGE)/assets
	cp README.md $(STAGE)/README.md
	cp -r docs $(STAGE)/docs
	cp slack-app-manifest.yaml $(STAGE)/slack-app-manifest.yaml
	cp ui/bundle.js $(STAGE)/ui/bundle.js
	go build -o $(STAGE)/server/plugin-$$(go env GOOS)-$$(go env GOARCH)$$(go env GOEXE) ./server
	cd $(KANDEV_BACKEND) && go run ./cmd/plugin-pack -dir $(CURDIR)/$(STAGE) -out $(CURDIR)/$(PKG_OUT) -platform-only
	rm -rf $(STAGE)
	@echo "Wrote $(PKG_OUT)"

## Build and verify all five platform binaries, the manifest, assets, and checksums.
verify-package: package
	@set -eu; \
		tmp="$$(mktemp -d)"; \
		trap 'rm -rf "$$tmp"' EXIT; \
		tar -xzf "$(PKG_OUT)" -C "$$tmp"; \
		sh scripts/verify-package.sh "$$tmp" full

## Build and verify the current host package.
verify-package-host: package-host
	@set -eu; \
		tmp="$$(mktemp -d)"; \
		trap 'rm -rf "$$tmp"' EXIT; \
		tar -xzf "$(PKG_OUT)" -C "$$tmp"; \
		sh scripts/verify-package.sh "$$tmp" host "$$(go env GOOS)-$$(go env GOARCH)"

## Print the package path for release checks.
package-file:
	@printf '%s\n' "$(PKG_OUT)"

clean:
	rm -rf bin $(STAGE) kandev-plugin-slack-*.tar.gz
