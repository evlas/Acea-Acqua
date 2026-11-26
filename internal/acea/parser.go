package acea

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ConsumptionPeriod represents one row in "DETTAGLIO DEI CONSUMI".
type ConsumptionPeriod struct {
	Type           string  `json:"type"`
	Start          string  `json:"start"` // YYYY-MM-DD
	End            string  `json:"end"`   // YYYY-MM-DD
	Days           int     `json:"days"`
	ConsumptionMC  float64 `json:"consumption_mc"`
	DailyAverageMC float64 `json:"daily_average_mc"`
}

// MeterReading represents one row in "PROSPETTO LETTURE E CONSUMI".
type MeterReading struct {
	MeterID     string  `json:"meter_id"`
	FromDate    string  `json:"from_date"`
	ToDate      string  `json:"to_date"`
	ReadingType string  `json:"reading_type"`
	FromValue   float64 `json:"from_value"`
	ToValue     float64 `json:"to_value"`
	Consumption float64 `json:"consumption_mc"`
}

// Bill holds the parsed data from an Acea Acqua PDF bill.
type Bill struct {
	SourceFile         string              `json:"source_file,omitempty"`
	InvoiceNumber      string              `json:"invoice_number"`
	IssueDate          string              `json:"issue_date"` // YYYY-MM-DD
	CustomerCode       string              `json:"customer_code"`
	ContractCode       string              `json:"contract_code"`
	BillingType        string              `json:"billing_type"`
	PeriodStart        string              `json:"period_start"` // YYYY-MM-DD
	PeriodEnd          string              `json:"period_end"`   // YYYY-MM-DD
	Days               int                 `json:"days"`
	ConsumptionMC      float64             `json:"consumption_mc"`
	DailyAverageMC     float64             `json:"daily_average_mc"`
	DailyAverageLiters float64             `json:"daily_average_liters"`
	Periods            []ConsumptionPeriod `json:"periods,omitempty"`
	Readings           []MeterReading      `json:"readings,omitempty"`
}

var (
	reInvoice      = regexp.MustCompile(`Numero Fattura\s+(\d+)`)
	reIssueDate    = regexp.MustCompile(`Emessa il\s+(\d{2}/\d{2}/\d{4})`)
	reCustomer     = regexp.MustCompile(`CODICE UTENZA:\s*(\d+)`)
	reContract     = regexp.MustCompile(`CODICE CONTRATTO:\s*(\d+)`)
	reBillingType  = regexp.MustCompile(`PER CONSUMO A\s+([A-Z]+)`)
	rePeriodRow    = regexp.MustCompile(`(?m)^(Conguaglio|Acconto|Saldo|Stimato)\s+(\d{2}/\d{2}/\d{4})\s+(\d{2}/\d{2}/\d{4})\s+(\d+)\s+(-?\d+(?:[.,]\d+)?)`)
	reReadingRow   = regexp.MustCompile(`(?m)^([A-Z0-9]{6,})\s+(\d{2}/\d{2}/\d{4})\s+(\d{2}/\d{2}/\d{4})\s+(.+?)\s+(\d+(?:[.,]\d+)?)\s+(\d+(?:[.,]\d+)?)\s+\d+(?:[.,]\d+)?\s+(\d+(?:[.,]\d+)?)$`)
	reFallbackRef  = regexp.MustCompile(`Periodo di (?:conguaglio|riferimento)\s+(\d{2}/\d{2}/\d{4})\s*-\s*(\d{2}/\d{2}/\d{4})`)
	reFallbackCons = regexp.MustCompile(`Consumo fatturato\s+(-?\d+(?:[.,]\d+)?)\s*mc`)
)

// ParseBill extracts Acea Acqua billing period, consumption, and daily average
// from the layout-ordered text of a PDF bill.
func ParseBill(text string, sourceFile string) (*Bill, error) {
	b := &Bill{SourceFile: sourceFile}

	if m := reInvoice.FindStringSubmatch(text); m != nil {
		b.InvoiceNumber = m[1]
	}
	if m := reIssueDate.FindStringSubmatch(text); m != nil {
		if d, err := parseITDate(m[1]); err == nil {
			b.IssueDate = d.Format("2006-01-02")
		}
	}
	if m := reCustomer.FindStringSubmatch(text); m != nil {
		b.CustomerCode = m[1]
	}
	if m := reContract.FindStringSubmatch(text); m != nil {
		b.ContractCode = m[1]
	}
	if m := reBillingType.FindStringSubmatch(text); m != nil {
		b.BillingType = m[1]
	}

	// Parse DETTAGLIO DEI CONSUMI section if present, else scan full text.
	detailSection := text
	if idx := strings.Index(text, "DETTAGLIO DEI CONSUMI"); idx >= 0 {
		detailSection = text[idx:]
		if endIdx := strings.Index(detailSection, "QUADRO DI DETTAGLIO"); endIdx > 0 {
			detailSection = detailSection[:endIdx]
		}
	}

	for _, m := range rePeriodRow.FindAllStringSubmatch(detailSection, -1) {
		startT, err1 := parseITDate(m[2])
		endT, err2 := parseITDate(m[3])
		days, err3 := strconv.Atoi(m[4])
		cons, err4 := parseITFloat(m[5])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || days <= 0 {
			continue
		}
		b.Periods = append(b.Periods, ConsumptionPeriod{
			Type:           m[1],
			Start:          startT.Format("2006-01-02"),
			End:            endT.Format("2006-01-02"),
			Days:           days,
			ConsumptionMC:  cons,
			DailyAverageMC: cons / float64(days),
		})
	}

	// Parse PROSPETTO LETTURE E CONSUMI rows if present.
	if idx := strings.Index(text, "PROSPETTO LETTURE E CONSUMI"); idx >= 0 {
		prospetto := text[idx:]
		if endIdx := strings.Index(prospetto, "DETTAGLIO DEI CONSUMI"); endIdx > 0 {
			prospetto = prospetto[:endIdx]
		}
		for _, m := range reReadingRow.FindAllStringSubmatch(prospetto, -1) {
			fromD, err1 := parseITDate(m[2])
			toD, err2 := parseITDate(m[3])
			fromV, err3 := parseITFloat(m[5])
			toV, err4 := parseITFloat(m[6])
			cons, err5 := parseITFloat(m[7])
			if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
				continue
			}
			b.Readings = append(b.Readings, MeterReading{
				MeterID:     m[1],
				FromDate:    fromD.Format("2006-01-02"),
				ToDate:      toD.Format("2006-01-02"),
				ReadingType: strings.TrimSpace(m[4]),
				FromValue:   fromV,
				ToValue:     toV,
				Consumption: cons,
			})
		}
	}

	if len(b.Periods) > 0 {
		var minStart, maxEnd time.Time
		var totalCons float64
		for i, p := range b.Periods {
			st, _ := time.Parse("2006-01-02", p.Start)
			en, _ := time.Parse("2006-01-02", p.End)
			if i == 0 || st.Before(minStart) {
				minStart = st
			}
			if i == 0 || en.After(maxEnd) {
				maxEnd = en
			}
			totalCons += p.ConsumptionMC
		}
		b.PeriodStart = minStart.Format("2006-01-02")
		b.PeriodEnd = maxEnd.Format("2006-01-02")
		if len(b.Periods) == 1 {
			b.Days = b.Periods[0].Days
		} else {
			b.Days = int(maxEnd.Sub(minStart).Hours()/24) + 1
		}
		b.ConsumptionMC = totalCons
	} else {
		// Fallback to page 1 summary fields
		refM := reFallbackRef.FindStringSubmatch(text)
		consM := reFallbackCons.FindStringSubmatch(text)
		if refM == nil || consM == nil {
			return nil, errors.New("could not find reading period or billed consumption in PDF text")
		}
		st, err1 := parseITDate(refM[1])
		en, err2 := parseITDate(refM[2])
		cons, err3 := parseITFloat(consM[1])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, fmt.Errorf("invalid fallback period/consumption values")
		}
		days := int(en.Sub(st).Hours()/24) + 1
		if days <= 0 {
			return nil, fmt.Errorf("invalid period %s - %s", st.Format("2006-01-02"), en.Format("2006-01-02"))
		}
		b.PeriodStart = st.Format("2006-01-02")
		b.PeriodEnd = en.Format("2006-01-02")
		b.Days = days
		b.ConsumptionMC = cons
	}

	if b.Days <= 0 {
		return nil, errors.New("parsed period has non-positive days")
	}
	b.DailyAverageMC = b.ConsumptionMC / float64(b.Days)
	b.DailyAverageLiters = b.DailyAverageMC * 1000.0
	return b, nil
}

func parseITDate(s string) (time.Time, error) {
	return time.Parse("02/01/2006", strings.TrimSpace(s))
}

func parseITFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "−", "-")
	s = strings.ReplaceAll(s, ",", ".")
	return strconv.ParseFloat(s, 64)
}
