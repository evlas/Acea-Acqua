package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"acea-acqua/internal/acea"
	"acea-acqua/internal/ha"
)

func TestGUIServerEndpoints(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Mock Home Assistant server
	var received []ha.ImportPayload
	haSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p ha.ImportPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatalf("decode HA payload: %v", err)
		}
		received = append(received, p)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer haSrv.Close()

	srv := NewServer(Config{
		HAURL:       haSrv.URL,
		HAToken:     "tok",
		StatisticID: "acea_acqua:water_consumption",
		StatName:    "Acea Acqua",
		Bills: []*acea.Bill{
			{
				InvoiceNumber:  "3026011000506098",
				PeriodStart:    "2026-07-11",
				PeriodEnd:      "2026-10-12",
				Days:           94,
				ConsumptionMC:  29,
				DailyAverageMC: 29.0 / 94.0,
			},
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. GET /
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "Validazione Bollette") {
		t.Fatalf("unexpected GET / response: %d", resp.StatusCode)
	}

	// 2. GET /api/init
	resp, err = http.Get(ts.URL + "/api/init")
	if err != nil {
		t.Fatalf("GET /api/init: %v", err)
	}
	var initData struct {
		Bills []*acea.Bill `json:"bills"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&initData)
	resp.Body.Close()
	if len(initData.Bills) != 1 || initData.Bills[0].InvoiceNumber != "3026011000506098" {
		t.Fatalf("unexpected /api/init bills: %+v", initData.Bills)
	}

	// 3. POST /api/parse with a real PDF if present
	sample := filepath.Join("../../upload", "250000032726_3026011000506098.pdf")
	if rawPDF, err := os.ReadFile(sample); err == nil {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("files", "bill.pdf")
		_, _ = fw.Write(rawPDF)
		_ = mw.Close()

		resp, err := http.Post(ts.URL+"/api/parse", mw.FormDataContentType(), &buf)
		if err != nil {
			t.Fatalf("POST /api/parse: %v", err)
		}
		var parseRes struct {
			Bills []*acea.Bill `json:"bills"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&parseRes)
		resp.Body.Close()
		if len(parseRes.Bills) != 1 || parseRes.Bills[0].ConsumptionMC != 29 {
			t.Fatalf("unexpected /api/parse result: %+v", parseRes.Bills)
		}
	}

	// 4. POST /api/send
	sendBody, _ := json.Marshal(sendRequest{
		HAURL:       haSrv.URL,
		HAToken:     "tok",
		StatisticID: "acea_acqua:water_consumption",
		StatName:    "Acea Acqua",
		Bills:       initData.Bills,
	})
	resp, err = http.Post(ts.URL+"/api/send", "application/json", bytes.NewReader(sendBody))
	if err != nil {
		t.Fatalf("POST /api/send: %v", err)
	}
	var sendRes struct {
		Results []sendResult `json:"results"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&sendRes)
	resp.Body.Close()
	if len(sendRes.Results) != 1 || !sendRes.Results[0].OK {
		t.Fatalf("unexpected /api/send results: %+v", sendRes.Results)
	}
	if len(received) != 1 || received[0].ConsumptionMC != 29 {
		t.Fatalf("HA mock did not receive expected payload: %+v", received)
	}
}
