BINARY := acea-ha
PDF_DIR ?= upload
ADDR ?= 127.0.0.1:8765

.PHONY: all build build-all test dry-run gui run install-ha clean help

all: build

help:
	@echo "Target disponibili:"
	@echo "  make build                     Compila il binario $(BINARY) per questo OS"
	@echo "  make build-all                 Compila per Linux, macOS (Intel/Apple Silicon) e Windows in dist/"
	@echo "  make test                      Esegue tutti i test Go"
	@echo "  make dry-run [PDF_DIR=upload]  Estrae i dati dai PDF senza inviare a Home Assistant"
	@echo "  make gui [PDF_DIR=upload]      Avvia la Web GUI di validazione su http://$(ADDR)"
	@echo "  make run [PDF_DIR=upload]      Invia le bollette a Home Assistant (usa HA_URL e HA_TOKEN)"
	@echo "  make install-ha HA_CONFIG=...  Copia custom_components/acea_acqua in HA_CONFIG"
	@echo "  make clean                     Rimuove il binario compilato"

build:
	go build -o $(BINARY) .

build-all:
	mkdir -p dist
	GOOS=linux   GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 .
	GOOS=darwin  GOARCH=amd64 go build -o dist/$(BINARY)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 go build -o dist/$(BINARY)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe .

test:
	go test -v ./...

dry-run: build
	./$(BINARY) -dry-run $(PDF_DIR)

gui: build
	./$(BINARY) -gui -addr $(ADDR) $(PDF_DIR)

run: build
	./$(BINARY) $(PDF_DIR)

install-ha:
	@test -n "$(HA_CONFIG)" || (echo "Errore: specifica HA_CONFIG=/percorso/config/homeassistant" >&2; exit 1)
	mkdir -p "$(HA_CONFIG)/custom_components"
	rm -rf "$(HA_CONFIG)/custom_components/acea_acqua"
	cp -r custom_components/acea_acqua "$(HA_CONFIG)/custom_components/"
	@echo "Integrazione copiata in $(HA_CONFIG)/custom_components/acea_acqua. Riavvia Home Assistant."

clean:
	rm -f $(BINARY)
