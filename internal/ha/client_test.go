package ha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"acea-acqua/internal/acea"
)

func TestSendBill(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotPayload ImportPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "secret-token", "acea_acqua:water_consumption", "Acea Acqua")
	bill := &acea.Bill{
		InvoiceNumber:  "3026011000506098",
		CustomerCode:   "250000032726",
		IssueDate:      "2026-10-12",
		BillingType:    "SALDO",
		PeriodStart:    "2026-07-11",
		PeriodEnd:      "2026-10-12",
		Days:           94,
		ConsumptionMC:  29,
		DailyAverageMC: 29.0 / 94.0,
	}

	if err := client.SendBill(context.Background(), bill); err != nil {
		t.Fatalf("SendBill failed: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization=%q, want Bearer secret-token", gotAuth)
	}
	if gotPath != "/api/services/acea_acqua/import_consumption" {
		t.Errorf("Path=%q, want /api/services/acea_acqua/import_consumption", gotPath)
	}
	if gotPayload.PeriodStart != "2026-07-11" || gotPayload.PeriodEnd != "2026-10-12" || gotPayload.ConsumptionMC != 29 {
		t.Errorf("unexpected payload: %+v", gotPayload)
	}
}
