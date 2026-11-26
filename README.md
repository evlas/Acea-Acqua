<p align="center">
  <img src="custom_components/acea_acqua/brand/logo.png" alt="Acea Acqua Logo" width="220">
</p>

# Acea Acqua - Home Assistant Integration 💧

[![hacs_badge](https://img.shields.io/badge/HACS-Custom-41BDF5.svg)](https://github.com/hacs/default)
[![GitHub Release](https://img.shields.io/github/v/release/evlas/Acea-Acqua?color=blue&style=flat-square)](https://github.com/evlas/Acea-Acqua/releases)
[![Hassfest](https://github.com/evlas/Acea-Acqua/actions/workflows/hassfest.yml/badge.svg)](https://github.com/evlas/Acea-Acqua/actions/workflows/hassfest.yml)
[![HACS Action](https://github.com/evlas/Acea-Acqua/actions/workflows/hacs.yml/badge.svg)](https://github.com/evlas/Acea-Acqua/actions/workflows/hacs.yml)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
[![Home Assistant](https://img.shields.io/badge/Home%20Assistant-2024.1%2B-41BDF5.svg)](https://www.home-assistant.io/)

🌍 **Language / Lingua**: **English** • [Italiano](README.it.md)

Home Assistant custom integration and pure-Go CLI/Web GUI tool (zero external Go dependencies) to extract billing periods, water consumption (`m³`), and daily averages from **Acea Acqua (Acea Ato5)** PDF bills and load them into Home Assistant's **Recorder Long-Term Statistics** for the **Energy -> Water** dashboard.

## Features

- **Pure-Go PDF Extractor & Parser**: Extracts invoice number, issue date, customer code, reading period (`period_start` / `period_end`), days, billed consumption (`m³`), meter readings, and daily average (`m³/day` & `L/day`) without external libraries or system binaries.
- **Local Web Validation GUI (`-gui`)**: Inspect, edit, and validate parsed bills before sending them to Home Assistant. Automatically remembers connection settings and `statistic_id`.
- **Home Assistant Integration (`acea_acqua`)**:
  - Exposes `acea_acqua.import_consumption` service using `async_add_external_statistics` (or `async_import_statistics` for entity IDs) so historical periods are written directly into the Recorder DB.
  - Serializes imports and awaits Recorder DB commits so consecutive bills remain continuous and idempotent.
  - Provides 5 native sensors under the **Acea Acqua** device:
    - `sensor.acea_acqua_consumo_totale_storico` (`m³`, total cumulative consumption from Recorder Long-Term Statistics)
    - `sensor.acea_acqua_consumo_anno_corrente` (`m³`, calendar-year consumption since January 1st)
    - `sensor.acea_acqua_lettura_inizio_anno` (`m³`, cumulative reading at January 1st 00:00 queried automatically from the Recorder)
    - `sensor.acea_acqua_ultimo_consumo_fatturato` (`m³`, latest billed consumption)
    - `sensor.acea_acqua_consumo_medio_giornaliero` (`m³/d`, latest daily average)

## Installation (Home Assistant)

### HACS (Recommended)

1. Open **HACS** in Home Assistant.
2. Click the three dots in the top-right corner -> **Custom repositories**.
3. Add `https://github.com/evlas/Acea-Acqua` with category **Integration**.
4. Install **Acea Acqua** and restart Home Assistant.
5. Go to **Settings -> Devices & Services -> Add Integration -> Acea Acqua**.

### Manual

1. Copy `custom_components/acea_acqua` into `/config/custom_components/acea_acqua`.
2. Restart Home Assistant.
3. Add **Acea Acqua** from **Settings -> Devices & Services**.

### Energy Dashboard Setup

1. Go to **Settings -> Dashboards -> Energy -> Water consumption -> Add water source**.
2. Select **`Acea Acqua Consumo Idrico`** (`acea_acqua:water_consumption`).

## Go CLI / Web GUI Usage

```bash
make build
# Web GUI (recommended for visual validation):
make gui
# Dry-run CLI:
make dry-run
# Direct CLI send:
HA_URL="http://homeassistant.local:8123" HA_TOKEN="YOUR_TOKEN" ./acea-ha upload/
```
