# TODO

## Miglioramenti prioritari

- [ ] **Proteggere il token della GUI**
  - Evitare che `/api/init` restituisca `ha_token` al browser.
  - Limitare l'ascolto della GUI al loopback o aggiungere autenticazione.
  - Rivedere il salvataggio locale della configurazione per limitare i dati sensibili persistiti.

- [ ] **Validare i statistic ID**
  - Verificare il formato degli ID esterni `dominio:nome`.
  - Verificare il formato degli entity ID `sensor.nome`.
  - Restituire errori chiari per valori non validi prima di chiamare Recorder.

- [ ] **Rinforzare il parser PDF**
  - Segnalare campi mancanti o ambigui invece di interpretarli tacitamente.
  - Gestire periodi duplicati, sovrapposti o con righe parziali.
  - Aggiungere fixture rappresentative di layout Acea diversi.

- [ ] **Migliorare le statistiche cumulative**
  - Preservare il consumo totale quando i valori giornalieri vengono arrotondati.
  - Usare l'ultimo giorno per assorbire eventuali differenze di arrotondamento.
  - Validare la coerenza fra `days`, `period_start` e `period_end`.
  - Gestire esplicitamente reinserimenti e periodi sovrapposti.
  - Dipendenze: validazione degli statistic ID.

- [ ] **Migliorare l'elaborazione batch della CLI**
  - Continuare l'elaborazione dopo un PDF non valido.
  - Aggiungere un riepilogo finale con file riusciti ed errori.

- [ ] **Espandere i test**
  - Coprire periodi negativi, sovrapposti e duplicati.
  - Coprire consumo giornaliero nullo o incoerente.
  - Coprire PDF con layout cambiato e reinserimenti in Home Assistant.
  - Coprire errori HTTP, URL e token non validi.
  - Dipendenze: parser rinforzato, statistiche cumulative e CLI batch.

- [ ] **Rendere sicure le release**
  - Eliminare commit automatici sul branch principale durante il rilascio.
  - Eliminare il `git push --force` sui tag.
  - Aggiornare la versione nel manifest prima di creare il tag.
  - Validare manifest, ZIP e binari prima della pubblicazione.
  - Dipendenze: ampliamento della copertura dei test.
