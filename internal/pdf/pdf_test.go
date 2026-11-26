package pdf

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSampleBills(t *testing.T) {
	files, err := filepath.Glob("../../upload/*.pdf")
	if err != nil || len(files) == 0 {
		t.Skip("no sample PDFs in ../../upload")
	}
	for _, f := range files {
		txt, err := ExtractText(f)
		if err != nil {
			t.Fatalf("ExtractText(%s) failed: %v", f, err)
		}
		if !strings.Contains(txt, "DETTAGLIO DEI CONSUMI") {
			t.Errorf("%s: missing DETTAGLIO DEI CONSUMI in extracted text", f)
		}
		if !strings.Contains(txt, "Numero Fattura") {
			lines := strings.Split(txt, "\n")
			for _, l := range lines {
				if strings.Contains(l, "Fattura") || strings.Contains(l, "Conguaglio") || strings.Contains(l, "3026011000341779") {
					t.Logf("LINE: %q", l)
				}
			}
			t.Errorf("%s: missing Numero Fattura in extracted text", f)
		}
	}
}
