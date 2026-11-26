"""Acea Acqua integration for Home Assistant."""

from __future__ import annotations

import asyncio
from datetime import date, datetime, timedelta
import logging
from typing import Any

import voluptuous as vol

from homeassistant.components.recorder import get_instance
from homeassistant.components.recorder.models import StatisticData, StatisticMetaData
from homeassistant.components.recorder.statistics import (
    async_add_external_statistics,
    async_import_statistics,
    statistics_during_period,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import Platform, UnitOfVolume
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.dispatcher import async_dispatcher_send
from homeassistant.helpers.storage import Store
from homeassistant.helpers.typing import ConfigType
from homeassistant.util import dt as dt_util

from .const import (
    DEFAULT_STAT_NAME,
    DEFAULT_STATISTIC_ID,
    DOMAIN,
    SERVICE_IMPORT_CONSUMPTION,
    SIGNAL_UPDATED,
    STORAGE_KEY,
    STORAGE_VERSION,
)

_LOGGER = logging.getLogger(__name__)

CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)
PLATFORMS = [Platform.SENSOR]

SERVICE_SCHEMA = vol.Schema(
    {
        vol.Optional("statistic_id", default=DEFAULT_STATISTIC_ID): cv.string,
        vol.Optional("name", default=DEFAULT_STAT_NAME): cv.string,
        vol.Required("period_start"): cv.date,
        vol.Required("period_end"): cv.date,
        vol.Required("consumption_mc"): vol.All(vol.Coerce(float), vol.Range(min=0)),
        vol.Optional("days"): vol.All(vol.Coerce(int), vol.Range(min=1)),
        vol.Optional("daily_average_mc"): vol.All(vol.Coerce(float), vol.Range(min=0)),
        vol.Optional("invoice_number"): cv.string,
        vol.Optional("customer_code"): cv.string,
        vol.Optional("issue_date"): cv.string,
        vol.Optional("billing_type"): cv.string,
    }
)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """Set up Acea Acqua from YAML (optional) and register services."""
    await _async_ensure_initialized(hass)
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Acea Acqua from a config entry."""
    await _async_ensure_initialized(hass)
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def _async_ensure_initialized(hass: HomeAssistant) -> None:
    """Initialize shared store and register the import_consumption service once."""
    domain_data = hass.data.setdefault(DOMAIN, {})
    if "store" not in domain_data:
        store = Store(hass, STORAGE_VERSION, STORAGE_KEY)
        stored = await store.async_load()
        domain_data["store"] = store
        domain_data["latest"] = stored if isinstance(stored, dict) else {}
        domain_data["lock"] = asyncio.Lock()

    if hass.services.has_service(DOMAIN, SERVICE_IMPORT_CONSUMPTION):
        return

    async def handle_import_consumption(call: ServiceCall) -> None:
        async with domain_data["lock"]:
            await _async_import_bill_period(hass, call.data)

    hass.services.async_register(
        DOMAIN,
        SERVICE_IMPORT_CONSUMPTION,
        handle_import_consumption,
        schema=SERVICE_SCHEMA,
    )


async def _async_import_bill_period(hass: HomeAssistant, data: dict[str, Any]) -> None:
    """Distribute periodic bill consumption across daily Long-Term Statistics."""
    statistic_id: str = data.get("statistic_id", DEFAULT_STATISTIC_ID).strip()
    stat_name: str = data.get("name", DEFAULT_STAT_NAME)
    is_external = ":" in statistic_id
    is_entity = "." in statistic_id
    if not is_external and not is_entity:
        raise ValueError(
            f"statistic_id '{statistic_id}' non valido: usa 'dominio:nome' "
            f"(es. '{DEFAULT_STATISTIC_ID}') oppure un entity_id (es. 'sensor.acea_acqua_ultimo_consumo_fatturato')."
        )
    stat_source = statistic_id.split(":", 1)[0] if is_external else "recorder"
    period_start: date = data["period_start"]
    period_end: date = data["period_end"]
    consumption_mc: float = float(data["consumption_mc"])

    if period_end < period_start:
        raise ValueError(f"period_end ({period_end}) must be >= period_start ({period_start})")

    num_days = int(data.get("days") or ((period_end - period_start).days + 1))
    daily_avg = float(data.get("daily_average_mc") or (consumption_mc / num_days))

    start_utc = _local_midnight_utc(period_start)
    recorder_instance = get_instance(hass)

    # Wait for any previously queued Recorder writes to commit before reading base_sum.
    await recorder_instance.async_block_till_done()

    # Fetch cumulative sum immediately preceding period_start so imports are
    # continuous across bills and idempotent on re-import.
    base_sum = await _async_get_sum_before(hass, statistic_id, start_utc)

    statistics: list[StatisticData] = []
    running_sum = base_sum
    for offset in range(num_days):
        day = period_start + timedelta(days=offset)
        day_utc = _local_midnight_utc(day)
        running_sum += daily_avg
        statistics.append(
            StatisticData(
                start=day_utc,
                state=round(daily_avg, 6),
                sum=round(running_sum, 6),
            )
        )

    metadata = StatisticMetaData(
        has_mean=False,
        has_sum=True,
        name=stat_name,
        source=stat_source,
        statistic_id=statistic_id,
        unit_of_measurement=UnitOfVolume.CUBIC_METERS,
    )

    if is_external:
        async_add_external_statistics(hass, metadata, statistics)
    else:
        async_import_statistics(hass, metadata, statistics)

    await recorder_instance.async_block_till_done()

    latest_payload = {
        "statistic_id": statistic_id,
        "period_start": period_start.isoformat(),
        "period_end": period_end.isoformat(),
        "days": num_days,
        "consumption_mc": round(consumption_mc, 4),
        "daily_average_mc": round(daily_avg, 6),
        "daily_average_liters": round(daily_avg * 1000.0, 2),
        "invoice_number": data.get("invoice_number", ""),
        "customer_code": data.get("customer_code", ""),
        "issue_date": data.get("issue_date", ""),
        "billing_type": data.get("billing_type", ""),
        "cumulative_sum_mc": round(running_sum, 4),
    }

    domain_data = hass.data[DOMAIN]
    domain_data["latest"] = latest_payload
    await domain_data["store"].async_save(latest_payload)
    async_dispatcher_send(hass, SIGNAL_UPDATED)

    _LOGGER.info(
        "Imported Acea Acqua consumption: %s..%s (%d days), %.2f m³ (%.4f m³/day) into %s",
        period_start,
        period_end,
        num_days,
        consumption_mc,
        daily_avg,
        statistic_id,
    )


def _local_midnight_utc(d: date) -> datetime:
    """Return UTC datetime aligned to an exact hour boundary for a local date."""
    local_dt = datetime(d.year, d.month, d.day, 0, 0, 0, tzinfo=dt_util.DEFAULT_TIME_ZONE)
    return dt_util.as_utc(local_dt).replace(minute=0, second=0, microsecond=0)


async def _async_get_sum_before(
    hass: HomeAssistant, statistic_id: str, before_utc: datetime
) -> float:
    """Return the latest cumulative sum strictly before before_utc, or 0.0."""
    epoch = datetime(1970, 1, 1, tzinfo=dt_util.UTC)
    if before_utc <= epoch:
        return 0.0

    stats = await get_instance(hass).async_add_executor_job(
        statistics_during_period,
        hass,
        epoch,
        before_utc,
        {statistic_id},
        "hour",
        None,
        {"sum"},
    )
    rows = stats.get(statistic_id, [])
    before_ts = before_utc.timestamp()
    for row in reversed(rows):
        row_start = row.get("start")
        if isinstance(row_start, datetime) and row_start >= before_utc:
            continue
        if isinstance(row_start, (int, float)) and row_start >= before_ts:
            continue
        if row.get("sum") is not None:
            return float(row["sum"])
    return 0.0
