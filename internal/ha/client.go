package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"acea-acqua/internal/acea"
)

// Client calls Home Assistant's REST API to import Acea Acqua consumption data.
type Client struct {
	BaseURL     string
	Token       string
	StatisticID string
	StatName    string
	HTTP        *http.Client
}

// ImportPayload matches the schema of the Home Assistant service
// acea_acqua.import_consumption.
type ImportPayload struct {
	StatisticID    string  `json:"statistic_id"`
	Name           string  `json:"name"`
	InvoiceNumber  string  `json:"invoice_number,omitempty"`
	CustomerCode   string  `json:"customer_code,omitempty"`
	IssueDate      string  `json:"issue_date,omitempty"`
	BillingType    string  `json:"billing_type,omitempty"`
	PeriodStart    string  `json:"period_start"`
	PeriodEnd      string  `json:"period_end"`
	Days           int     `json:"days"`
	ConsumptionMC  float64 `json:"consumption_mc"`
	DailyAverageMC float64 `json:"daily_average_mc"`
}

// NewClient creates a Home Assistant REST API client.
func NewClient(baseURL, token, statisticID, statName string) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		StatisticID: statisticID,
		StatName:    statName,
		HTTP:        &http.Client{Timeout: 30 * time.Second},
	}
}

// BuildPayloads builds one ImportPayload per consumption period in the bill
// (or a single payload for the whole bill if Periods is empty).
func (c *Client) BuildPayloads(bill *acea.Bill) []ImportPayload {
	if len(bill.Periods) > 1 {
		out := make([]ImportPayload, 0, len(bill.Periods))
		for _, p := range bill.Periods {
			out = append(out, ImportPayload{
				StatisticID:    c.StatisticID,
				Name:           c.StatName,
				InvoiceNumber:  bill.InvoiceNumber,
				CustomerCode:   bill.CustomerCode,
				IssueDate:      bill.IssueDate,
				BillingType:    p.Type,
				PeriodStart:    p.Start,
				PeriodEnd:      p.End,
				Days:           p.Days,
				ConsumptionMC:  p.ConsumptionMC,
				DailyAverageMC: p.DailyAverageMC,
			})
		}
		return out
	}

	return []ImportPayload{{
		StatisticID:    c.StatisticID,
		Name:           c.StatName,
		InvoiceNumber:  bill.InvoiceNumber,
		CustomerCode:   bill.CustomerCode,
		IssueDate:      bill.IssueDate,
		BillingType:    bill.BillingType,
		PeriodStart:    bill.PeriodStart,
		PeriodEnd:      bill.PeriodEnd,
		Days:           bill.Days,
		ConsumptionMC:  bill.ConsumptionMC,
		DailyAverageMC: bill.DailyAverageMC,
	}}
}

// SendBill sends all periods of a parsed bill to Home Assistant via
// POST /api/services/acea_acqua/import_consumption.
func (c *Client) SendBill(ctx context.Context, bill *acea.Bill) error {
	for _, p := range c.BuildPayloads(bill) {
		if err := c.callImportService(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) callImportService(ctx context.Context, payload ImportPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	endpoint := c.BaseURL + "/api/services/acea_acqua/import_consumption"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("home assistant returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
