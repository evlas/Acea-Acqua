package acea

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"acea-acqua/internal/pdf"
)

func TestParseBillText(t *testing.T) {
	sample := `
CODICE UTENZA: 1234567890
CODICE CONTRATTO: 9876543210
PER CONSUMO A SALDO
Numero Fattura 3026011000506098
Emessa il 12/10/2026

PROSPETTO LETTURE E CONSUMI
Matricola contatore dal al Tipo lettura Da lettura A lettura Coefficiente Consumo
SETA071225022877 11/07/2026 12/10/2026 Lettura Effettiva 130 159 1 29

DETTAGLIO DEI CONSUMI
Tipo Periodo dal al N°Giorni Consumo Fatturato Restituzione Acconti Consumo Totale
Conguaglio 11/07/2026 12/10/2026 94 29 29
`
	bill, err := ParseBill(sample, "synthetic.pdf")
	if err != nil {
		t.Fatalf("ParseBill failed: %v", err)
	}
	if bill.InvoiceNumber != "3026011000506098" || bill.CustomerCode != "1234567890" {
		t.Errorf("unexpected metadata: %+v", bill)
	}
	if bill.PeriodStart != "2026-07-11" || bill.PeriodEnd != "2026-10-12" || bill.Days != 94 || bill.ConsumptionMC != 29 {
		t.Errorf("unexpected period/consumption: %+v", bill)
	}
	if math.Abs(bill.DailyAverageMC-(29.0/94.0)) > 1e-6 {
		t.Errorf("unexpected daily average: %v", bill.DailyAverageMC)
	}
	if len(bill.Readings) != 1 || bill.Readings[0].MeterID != "SETA071225022877" {
		t.Errorf("unexpected readings: %+v", bill.Readings)
	}
}

func TestParseSampleBills(t *testing.T) {
	if _, err := os.Stat("../../upload"); os.IsNotExist(err) {
		t.Skip("no ../../upload directory")
	}
	expected := map[string]struct {
		invoice string
		start   string
		end     string
		days    int
		mc      float64
	}{
		"250000032726_3025011000338390.pdf": {"3025011000338390", "2025-01-15", "2025-07-10", 177, 58},
		"250000032726_3026011000018840.pdf": {"3026011000018840", "2025-07-11", "2026-01-14", 188, 59},
		"250000032726_3026011000180848.pdf": {"3026011000180848", "2026-01-15", "2026-04-13", 89, 33},
		"250000032726_3026011000341779.pdf": {"3026011000341779", "2026-04-14", "2026-07-10", 88, 29},
		"250000032726_3026011000506098.pdf": {"3026011000506098", "2026-07-11", "2026-10-12", 94, 29},
	}

	for name, exp := range expected {
		path := filepath.Join("../../upload", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Skipf("sample PDF %s not present", path)
		}
		txt, err := pdf.ExtractText(path)
		if err != nil {
			t.Fatalf("%s: ExtractText failed: %v", name, err)
		}
		bill, err := ParseBill(txt, name)
		if err != nil {
			t.Fatalf("%s: ParseBill failed: %v", name, err)
		}
		if bill.InvoiceNumber != exp.invoice {
			t.Errorf("%s: invoice=%q, want %q", name, bill.InvoiceNumber, exp.invoice)
		}
		if bill.PeriodStart != exp.start || bill.PeriodEnd != exp.end {
			t.Errorf("%s: period=%s..%s, want %s..%s", name, bill.PeriodStart, bill.PeriodEnd, exp.start, exp.end)
		}
		if bill.Days != exp.days {
			t.Errorf("%s: days=%d, want %d", name, bill.Days, exp.days)
		}
		if bill.ConsumptionMC != exp.mc {
			t.Errorf("%s: mc=%v, want %v", name, bill.ConsumptionMC, exp.mc)
		}
		wantAvg := exp.mc / float64(exp.days)
		if math.Abs(bill.DailyAverageMC-wantAvg) > 1e-6 {
			t.Errorf("%s: avg=%v, want %v", name, bill.DailyAverageMC, wantAvg)
		}
	}
}
