.DEFAULT_GOAL := help

MODULE := github.com/irfanadwifangga/yt-to-mp3

# Tanpa redirect ke /dev/null: pada Windows, path sh.exe milik Git memuat
# spasi sehingga Make gagal memakainya untuk $(shell ...) dan jatuh ke
# cmd.exe, yang membaca /dev/null sebagai path lalu gagal. Akibatnya
# git describe tidak pernah jalan dan versi selalu ter-stempel "dev".
# Ditetapkan dengan := supaya git dipanggil sekali, bukan setiap referensi.
GIT_VERSION := $(shell git describe --tags --always --dirty || echo dev)
GIT_COMMIT  := $(shell git rev-parse --short HEAD || echo unknown)

VERSION ?= $(GIT_VERSION)
COMMIT  ?= $(GIT_COMMIT)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT)

ifeq ($(OS),Windows_NT)
	BIN := bin/yt-to-mp3.exe
else
	BIN := bin/yt-to-mp3
endif

# Port tetap untuk mode dev. Produksi memakai port acak, tapi proxy Vite
# butuh target yang bisa ditebak.
DEV_PORT := 8799

.PHONY: help
help: ## Tampilkan daftar target
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: install-web
install-web: ## Pasang dependensi frontend
	npm --prefix web install

.PHONY: build-web
build-web: ## Build SPA ke web/dist
	npm --prefix web run build

.PHONY: build
build: build-web ## Build binary rilis lengkap dengan SPA ter-embed
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/app

.PHONY: build-go
build-go: ## Build binary saja, tanpa build ulang SPA
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/app

.PHONY: run
run: build-go ## Jalankan binary hasil build
	./$(BIN)

.PHONY: dev-api
dev-api: ## Jalankan backend di port tetap untuk dev (pasangan dev-web)
	go run ./cmd/app -dev -port $(DEV_PORT) -no-browser

.PHONY: dev-web
dev-web: ## Jalankan Vite dev server (pasangan dev-api)
	npm --prefix web run dev

.PHONY: test
test: ## Jalankan seluruh test Go
	go test ./...

.PHONY: cover
cover: ## Jalankan test dengan laporan coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: vet
vet: ## Jalankan go vet
	go vet ./...

.PHONY: fmt
fmt: ## Rapikan format kode Go
	gofmt -w ./cmd ./internal ./web

.PHONY: fmt-check
fmt-check: ## Gagal bila ada file Go yang belum diformat
	@out=$$(gofmt -l ./cmd ./internal ./web); \
	if [ -n "$$out" ]; then echo "belum diformat:"; echo "$$out"; exit 1; fi

.PHONY: typecheck
typecheck: ## Typecheck frontend
	npm --prefix web run typecheck

.PHONY: check
check: fmt-check vet test ## Jalankan seluruh pemeriksaan Go

.PHONY: tidy
tidy: ## Rapikan go.mod
	go mod tidy

.PHONY: clean
clean: ## Hapus artefak build
	rm -rf bin coverage.out web/dist/assets web/dist/index.html
