package gui

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"acea-acqua/internal/acea"
	"acea-acqua/internal/ha"
	"acea-acqua/internal/pdf"
)

//go:embed index.html
var indexHTML []byte

// Config holds initial settings and pre-parsed bills for the Web GUI.
type Config struct {
	HAURL       string
	HAToken     string
	StatisticID string
	StatName    string
	Bills       []*acea.Bill
	Timeout     time.Duration
}

// Server serves the local validation GUI and REST endpoints.
type Server struct {
	cfg        Config
	configPath string
}

type savedConfig struct {
	HAURL       string `json:"ha_url"`
	HAToken     string `json:"ha_token"`
	StatisticID string `json:"statistic_id"`
	StatName    string `json:"stat_name"`
}

// NewServer creates a GUI HTTP server.
func NewServer(cfg Config) *Server {
	cfgPath := defaultConfigPath()
	if saved, err := loadSavedConfig(cfgPath); err == nil {
		if cfg.HAURL == "" {
			cfg.HAURL = saved.HAURL
		}
		if cfg.HAToken == "" {
			cfg.HAToken = saved.HAToken
		}
		if cfg.StatisticID == "" {
			cfg.StatisticID = saved.StatisticID
		}
		if cfg.StatName == "" {
			cfg.StatName = saved.StatName
		}
	}
	if cfg.StatisticID == "" {
		cfg.StatisticID = "acea_acqua:water_consumption"
	}
	if cfg.StatName == "" {
		cfg.StatName = "Acea Acqua Consumo Idrico"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Server{cfg: cfg, configPath: cfgPath}
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "acea-ha", "config.json")
}

func loadSavedConfig(path string) (savedConfig, error) {
	var sc savedConfig
	if path == "" {
		return sc, os.ErrNotExist
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return sc, err
	}
	err = json.Unmarshal(b, &sc)
	return sc, err
}

func (s *Server) saveConfig(sc savedConfig) error {
	s.cfg.HAURL = sc.HAURL
	s.cfg.HAToken = sc.HAToken
	if sc.StatisticID != "" {
		s.cfg.StatisticID = sc.StatisticID
	}
	if sc.StatName != "" {
		s.cfg.StatName = sc.StatName
	}
	if s.configPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, b, 0o600)
}

// Handler returns the HTTP handler for the GUI and API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/init", s.handleInit)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/parse", s.handleParse)
	mux.HandleFunc("/api/send", s.handleSend)
	return mux
}

// ListenAndServe starts the HTTP server on addr (e.g. "127.0.0.1:8765") and
// invokes onReady with the bound URL before blocking.
func (s *Server) ListenAndServe(addr string, onReady func(url string)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String()
	if onReady != nil {
		onReady(url)
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.Serve(ln)
}

// OpenBrowser opens url in the user's default browser (best effort).
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ha_url":       s.cfg.HAURL,
		"ha_token":     s.cfg.HAToken,
		"statistic_id": s.cfg.StatisticID,
		"stat_name":    s.cfg.StatName,
		"bills":        s.cfg.Bills,
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var sc savedConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&sc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON non valido: " + err.Error()})
		return
	}
	if err := s.saveConfig(sc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "richiesta multipart non valida: " + err.Error()})
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "nessun file PDF inviato"})
		return
	}

	var parsed []*acea.Bill
	var errs []string
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 32<<20))
		_ = f.Close()
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		txt, err := pdf.ExtractTextFromBytes(raw)
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		bill, err := acea.ParseBill(txt, fh.Filename)
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		parsed = append(parsed, bill)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"bills":  parsed,
		"errors": errs,
	})
}

type sendRequest struct {
	HAURL       string       `json:"ha_url"`
	HAToken     string       `json:"ha_token"`
	StatisticID string       `json:"statistic_id"`
	StatName    string       `json:"stat_name"`
	Bills       []*acea.Bill `json:"bills"`
}

type sendResult struct {
	InvoiceNumber string `json:"invoice_number"`
	PeriodStart   string `json:"period_start"`
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sendRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON non valido: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.HAURL) == "" || strings.TrimSpace(req.HAToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "URL Home Assistant e Token sono obbligatori"})
		return
	}
	if req.StatisticID == "" {
		req.StatisticID = s.cfg.StatisticID
	}
	if req.StatName == "" {
		req.StatName = s.cfg.StatName
	}

	_ = s.saveConfig(savedConfig{
		HAURL:       strings.TrimSpace(req.HAURL),
		HAToken:     strings.TrimSpace(req.HAToken),
		StatisticID: strings.TrimSpace(req.StatisticID),
		StatName:    strings.TrimSpace(req.StatName),
	})

	sort.Slice(req.Bills, func(i, j int) bool {
		return req.Bills[i].PeriodStart < req.Bills[j].PeriodStart
	})

	client := ha.NewClient(req.HAURL, req.HAToken, req.StatisticID, req.StatName)
	client.HTTP.Timeout = s.cfg.Timeout

	results := make([]sendResult, 0, len(req.Bills))
	for _, b := range req.Bills {
		if b.Days > 0 {
			b.DailyAverageMC = b.ConsumptionMC / float64(b.Days)
			b.DailyAverageLiters = b.DailyAverageMC * 1000.0
		}
		// Reset multi-row sub-periods when user edited the bill summary in GUI
		// so the edited period/consumption is sent directly.
		if len(b.Periods) > 1 {
			b.Periods = nil
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
		err := client.SendBill(ctx, b)
		cancel()

		res := sendResult{
			InvoiceNumber: b.InvoiceNumber,
			PeriodStart:   b.PeriodStart,
			OK:            err == nil,
		}
		if err != nil {
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
