<p align="center">
  <img src="custom_components/acea_acqua/brand/logo.png" alt="Acea Acqua Logo" width="220">
</p>

# Acea Acqua - Integrazione Home Assistant 💧

[![hacs_badge](https://img.shields.io/badge/HACS-Custom-41BDF5.svg)](https://github.com/hacs/default)
[![GitHub Release](https://img.shields.io/github/v/release/evlas/Acea-Acqua?color=blue&style=flat-square)](https://github.com/evlas/Acea-Acqua/releases)
[![Hassfest](https://github.com/evlas/Acea-Acqua/actions/workflows/hassfest.yml/badge.svg)](https://github.com/evlas/Acea-Acqua/actions/workflows/hassfest.yml)
[![HACS Action](https://github.com/evlas/Acea-Acqua/actions/workflows/hacs.yml/badge.svg)](https://github.com/evlas/Acea-Acqua/actions/workflows/hacs.yml)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Home Assistant](https://img.shields.io/badge/Home%20Assistant-2024.1%2B-41BDF5.svg)](https://www.home-assistant.io/)

🌍 **Language / Lingua**: [English](README.md) • **Italiano**

Integrazione personalizzata per **Home Assistant** e strumento CLI / Web GUI scritto in **Go puro** (zero dipendenze esterne) per estrarre periodo di lettura, consumi (`m³`) e media giornaliera dalle bollette PDF di **Acea Acqua (Acea Ato5)** e caricarli nelle **Long-Term Statistics** del Recorder (utilizzabili nella dashboard **Energia -> Acqua**).

## Funzionalità

- **Estrattore PDF e Parser in Go puro**: estrae numero fattura, data emissione, codice utenza, periodo (`period_start` / `period_end`), giorni, consumo fatturato (`m³`), dettaglio letture contatore e media giornaliera (`m³/gg` e `L/gg`) senza librerie esterne.
- **Web GUI locale di validazione (`-gui`)**: permette di verificare a vista, modificare date/consumi e inviare le bollette selezionate a Home Assistant, salvando automaticamente URL, token e `statistic_id` in `~/.config/acea-ha/config.json`.
- **Integrazione Home Assistant (`acea_acqua`)**:
  - Registra il servizio `acea_acqua.import_consumption` che scrive i dati storici giorno per giorno tramite `async_add_external_statistics` (`acea_acqua:water_consumption`) o `async_import_statistics`.
  - Serializza gli import e attende il commit del Recorder (`async_block_till_done`), garantendo continuità cumulativa e idempotenza.
  - Espone i sensori di riepilogo `sensor.acea_acqua_consumo_medio_giornaliero` (`m³/d`) e `sensor.acea_acqua_ultimo_consumo_fatturato` (`m³`).

## Installazione in Home Assistant

### HACS (Consigliato)

1. Apri **HACS** in Home Assistant.
2. Menu in alto a destra -> **Repository personalizzati**.
3. Aggiungi `https://github.com/evlas/Acea-Acqua` con categoria **Integrazione**.
4. Installa **Acea Acqua** e riavvia Home Assistant.
5. Vai su **Impostazioni -> Dispositivi e servizi -> Aggiungi integrazione -> Acea Acqua**.

### Manuale

1. Copia la cartella [`custom_components/acea_acqua`](custom_components/acea_acqua) dentro `/config/custom_components/acea_acqua` (oppure usa `make install-ha HA_CONFIG=/percorso/config`).
2. Riavvia Home Assistant.
3. Aggiungi **Acea Acqua** da **Impostazioni -> Dispositivi e servizi**.

### Configurazione Dashboard Energia

1. Vai su **Impostazioni -> Dashboard -> Energia -> Consumo di acqua -> Aggiungi fonte d'acqua**.
2. Seleziona **`Acea Acqua Consumo Idrico`** (`acea_acqua:water_consumption`).

## Utilizzo CLI / Web GUI in Go

```bash
make build
# Avvio Web GUI di validazione:
make gui
# Prova in locale senza inviare:
make dry-run
# Invio diretto da CLI:
HA_URL="http://homeassistant.local:8123" HA_TOKEN="IL_TUO_TOKEN" ./acea-ha upload/
```
