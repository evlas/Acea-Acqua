"""Sensor platform for Acea Acqua."""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorEntityDescription,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import UnitOfVolume
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.dispatcher import async_dispatcher_connect
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import DOMAIN, SIGNAL_UPDATED


@dataclass(frozen=True, kw_only=True)
class AceaSensorDescription(SensorEntityDescription):
    """Describes an Acea Acqua sensor."""

    value_fn: Callable[[dict[str, Any]], Any]


SENSORS: tuple[AceaSensorDescription, ...] = (
    AceaSensorDescription(
        key="daily_average_mc",
        name="Acea Acqua Consumo Medio Giornaliero",
        icon="mdi:water-percent",
        native_unit_of_measurement="m³/d",
        state_class=SensorStateClass.MEASUREMENT,
        suggested_display_precision=4,
        value_fn=lambda d: d.get("daily_average_mc"),
    ),
    AceaSensorDescription(
        key="consumption_mc",
        name="Acea Acqua Ultimo Consumo Fatturato",
        icon="mdi:water",
        device_class=SensorDeviceClass.WATER,
        native_unit_of_measurement=UnitOfVolume.CUBIC_METERS,
        state_class=SensorStateClass.TOTAL,
        suggested_display_precision=2,
        value_fn=lambda d: d.get("consumption_mc"),
    ),
    AceaSensorDescription(
        key="cumulative_sum_mc",
        name="Acea Acqua Consumo Totale Storico",
        icon="mdi:counter",
        device_class=SensorDeviceClass.WATER,
        native_unit_of_measurement=UnitOfVolume.CUBIC_METERS,
        state_class=SensorStateClass.TOTAL_INCREASING,
        suggested_display_precision=3,
        value_fn=lambda d: d.get("cumulative_sum_mc"),
    ),
    AceaSensorDescription(
        key="current_year_consumption_mc",
        name="Acea Acqua Consumo Anno Corrente",
        icon="mdi:water-sync",
        device_class=SensorDeviceClass.WATER,
        native_unit_of_measurement=UnitOfVolume.CUBIC_METERS,
        state_class=SensorStateClass.TOTAL_INCREASING,
        suggested_display_precision=3,
        value_fn=lambda d: d.get("current_year_consumption_mc"),
    ),
    AceaSensorDescription(
        key="jan1_baseline_mc",
        name="Acea Acqua Lettura Inizio Anno",
        icon="mdi:calendar-start",
        device_class=SensorDeviceClass.WATER,
        native_unit_of_measurement=UnitOfVolume.CUBIC_METERS,
        state_class=SensorStateClass.TOTAL,
        suggested_display_precision=3,
        value_fn=lambda d: d.get("jan1_baseline_mc"),
    ),
)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Acea Acqua sensor entities."""
    async_add_entities([AceaAcquaSensor(hass, entry, desc) for desc in SENSORS])


class AceaAcquaSensor(SensorEntity):
    """Sensor exposing the latest imported Acea Acqua bill metrics."""

    entity_description: AceaSensorDescription
    _attr_has_entity_name = False
    _attr_should_poll = False

    def __init__(
        self,
        hass: HomeAssistant,
        entry: ConfigEntry,
        description: AceaSensorDescription,
    ) -> None:
        self.hass = hass
        self.entity_description = description
        self._attr_unique_id = f"{entry.entry_id}_{description.key}"
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, entry.entry_id)},
            name="Acea Acqua",
            manufacturer="Acea Ato 5 / Acea",
            model="Fornitura Idrica (Long-Term Statistics)",
        )

    async def async_added_to_hass(self) -> None:
        """Register dispatcher listener when added to HA."""
        self.async_on_remove(
            async_dispatcher_connect(
                self.hass, SIGNAL_UPDATED, self._handle_updated
            )
        )

    @callback
    def _handle_updated(self) -> None:
        self.async_write_ha_state()

    @property
    def _latest(self) -> dict[str, Any]:
        return self.hass.data.get(DOMAIN, {}).get("latest", {})

    @property
    def native_value(self) -> Any:
        return self.entity_description.value_fn(self._latest)

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        latest = self._latest
        if not latest:
            return {}
        return {
            "statistic_id": latest.get("statistic_id"),
            "current_year": latest.get("current_year"),
            "cumulative_sum_mc": latest.get("cumulative_sum_mc"),
            "jan1_baseline_mc": latest.get("jan1_baseline_mc"),
            "current_year_consumption_mc": latest.get("current_year_consumption_mc"),
            "period_start": latest.get("period_start"),
            "period_end": latest.get("period_end"),
            "days": latest.get("days"),
            "daily_average_liters": latest.get("daily_average_liters"),
            "invoice_number": latest.get("invoice_number"),
            "customer_code": latest.get("customer_code"),
            "issue_date": latest.get("issue_date"),
            "billing_type": latest.get("billing_type"),
        }
