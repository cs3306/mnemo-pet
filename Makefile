.PHONY: help env build test run fake skin docker up down logs install uninstall pack check clean

help:
	@echo "build      compile petd to ./bin/petd"
	@echo "test       go tests (race detector) + python/js syntax checks"
	@echo "env        create .env with a random free port pair (done automatically)"
	@echo "run        run petd locally (data in ./data, plugins/skins from the repo)"
	@echo "fake       run the fake renderer (talks to a local petd)"
	@echo "skin       regenerate the default skin PNGs"
	@echo "docker     build the Docker image"
	@echo "up/down    docker compose up -d --build / down"
	@echo "install    install the GNOME Shell extension (./install.sh)"
	@echo "pack       zip the extension (dist/mnemo-pet@cs3306.zip)"

build:
	cd petd && CGO_ENABLED=0 go build -trimpath -o ../bin/petd ./cmd/petd

test: check
	cd petd && go vet ./... && go test -race -count=1 ./...

check:
	python3 -m py_compile plugins/_sdk/petplugin.py plugins/*/main.py tools/*.py
	@find . -name __pycache__ -prune -exec rm -rf {} +
	@for f in extension/*.js; do cp $$f /tmp/_chk_$$(basename $$f .js).mjs && node --check /tmp/_chk_$$(basename $$f .js).mjs; done; rm -f /tmp/_chk_*.mjs

# Ports (and the rest of the settings) live in .env; it is created on first use
# with a random free port pair. Variables from it are exported to every command.
-include .env
export PET_PORT PET_HTTP_PORT PET_TOKEN

.env:
	python3 tools/mkenv.py

env: .env
	@grep '^PET_.*PORT' .env

run: build .env
	mkdir -p data
	PET_DATA_DIR=./data PET_SKINS_DIR=./skins PET_PLUGINS_DIR=./plugins ./bin/petd

fake:
	python3 tools/fake_render.py

skin:
	python3 tools/gen_default_skin.py

docker:
	docker build -t cs3306/mnemo-pet:latest .

up:
	mkdir -p data
	docker compose up -d --build

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

install:
	./install.sh

uninstall:
	./uninstall.sh

pack:
	mkdir -p dist build_ext/skins
	rm -rf build_ext && mkdir -p build_ext/skins
	cp -r extension/. build_ext/ && cp -r skins/. build_ext/skins/
	cd build_ext && zip -qr ../dist/mnemo-pet@cs3306.zip .
	rm -rf build_ext
	@echo dist/mnemo-pet@cs3306.zip

clean:
	rm -rf bin dist build_ext
