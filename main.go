package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"acea-acqua/internal/acea"
	"acea-acqua/internal/gui"
	"acea-acqua/internal/ha"
	"acea-acqua/internal/pdf"
)

func main() {
	var (
		haURL       = flag.String("ha-url", os.Getenv("HA_URL"), "Home Assistant base URL (or HA_URL env, e.g. http://homeassistant.local:8123)")
		haToken     = flag.String("ha-token", os.Getenv("HA_TOKEN"), "Home Assistant Long-Lived Access Token (or HA_TOKEN env)")
		statisticID = flag.String("statistic-id", "", "Home Assistant external statistic ID (default acea_acqua:water_consumption)")
		statName    = flag.String("stat-name", "", "Display name for the statistic in Home Assistant (default Acea Acqua Consumo Idrico)")
		dryRun      = flag.Bool("dry-run", false, "Parse bills and print summary/JSON without sending to Home Assistant")
		jsonOut     = flag.Bool("json", false, "Output parsed bills as JSON")
		guiMode     = flag.Bool("gui", false, "Start local Web GUI to visually validate bills before sending")
		guiAddr     = flag.String("addr", "127.0.0.1:8765", "Listen address for Web GUI")
		noBrowser   = flag.Bool("no-browser", false, "Do not open browser automatically when -gui is set")
		timeout     = flag.Duration("timeout", 30*time.Second, "HTTP request timeout")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [pdf-file-or-dir ...]\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if !*guiMode && flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	var bills []*acea.Bill
	if flag.NArg() > 0 {
		pdfPaths, err := collectPDFs(flag.Args())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(pdfPaths) == 0 && !*guiMode {
			fmt.Fprintln(os.Stderr, "error: no PDF files found")
			os.Exit(1)
		}

		seenInvoices := make(map[string]string)
		for _, p := range pdfPaths {
			txt, err := pdf.ExtractText(p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading %s: %v\n", p, err)
				os.Exit(1)
			}
			bill, err := acea.ParseBill(txt, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", p, err)
				os.Exit(1)
			}
			if bill.InvoiceNumber != "" {
				if prev, dup := seenInvoices[bill.InvoiceNumber]; dup {
					fmt.Fprintf(os.Stderr, "skipping duplicate invoice %s (%s already loaded from %s)\n", bill.InvoiceNumber, p, prev)
					continue
				}
				seenInvoices[bill.InvoiceNumber] = p
			}
			bills = append(bills, bill)
		}

		// Sort chronologically by period start so cumulative statistics import cleanly.
		sort.Slice(bills, func(i, j int) bool {
			if bills[i].PeriodStart != bills[j].PeriodStart {
				return bills[i].PeriodStart < bills[j].PeriodStart
			}
			return bills[i].InvoiceNumber < bills[j].InvoiceNumber
		})
	}

	if *guiMode {
		srv := gui.NewServer(gui.Config{
			HAURL:       *haURL,
			HAToken:     *haToken,
			StatisticID: *statisticID,
			StatName:    *statName,
			Bills:       bills,
			Timeout:     *timeout,
		})
		err := srv.ListenAndServe(*guiAddr, func(url string) {
			fmt.Printf("GUI disponibile su: %s (premi Ctrl+C per uscire)\n", url)
			if !*noBrowser {
				gui.OpenBrowser(url)
			}
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error starting GUI server: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(bills); err != nil {
			fmt.Fprintf(os.Stderr, "error encoding json: %v\n", err)
			os.Exit(1)
		}
	} else {
		for _, b := range bills {
			fmt.Printf("Fattura %-16s | Periodo: %s -> %s (%3d gg) | Consumo: %6.2f m³ | Media: %.4f m³/gg (%.1f L/gg)\n",
				b.InvoiceNumber, b.PeriodStart, b.PeriodEnd, b.Days, b.ConsumptionMC, b.DailyAverageMC, b.DailyAverageLiters)
		}
	}

	if *dryRun {
		return
	}

	if strings.TrimSpace(*haURL) == "" || strings.TrimSpace(*haToken) == "" {
		fmt.Fprintln(os.Stderr, "error: -ha-url (or HA_URL) and -ha-token (or HA_TOKEN) are required unless -dry-run is set")
		os.Exit(2)
	}

	if *statisticID == "" {
		*statisticID = "acea_acqua:water_consumption"
	}
	if *statName == "" {
		*statName = "Acea Acqua Consumo Idrico"
	}
	client := ha.NewClient(*haURL, *haToken, *statisticID, *statName)
	client.HTTP.Timeout = *timeout
	ctx := context.Background()

	for _, b := range bills {
		if err := client.SendBill(ctx, b); err != nil {
			fmt.Fprintf(os.Stderr, "error sending invoice %s to Home Assistant: %v\n", b.InvoiceNumber, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Inviata fattura %s (%s -> %s, %.2f m³) a Home Assistant\n",
			b.InvoiceNumber, b.PeriodStart, b.PeriodEnd, b.ConsumptionMC)
	}
}

func collectPDFs(inputs []string) ([]string, error) {
	var out []string
	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			entries, err := os.ReadDir(in)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
					out = append(out, filepath.Join(in, e.Name()))
				}
			}
		} else {
			out = append(out, in)
		}
	}
	sort.Strings(out)
	return out, nil
}
