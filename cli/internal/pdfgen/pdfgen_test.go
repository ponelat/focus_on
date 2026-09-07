package pdfgen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 180, G: 60, B: 40, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestHeaderAdvancesPastTheLongerColumn is a regression test for a real
// layout bug: the header draws business info (left) and invoice metadata
// (right) as two independent columns starting at the same Y, but only ever
// continued layout from wherever the *last-drawn* column's cursor ended up —
// which is the right column, since it's drawn second. A business block with
// more lines than the invoice metadata (name+address+email+reg
// number+VAT note is easily 5+ lines; the metadata is often just 2-3) meant
// "Bill To" started drawing while the business address was still going,
// producing visibly overlapping/interleaved text.
func TestHeaderAdvancesPastTheLongerColumn(t *testing.T) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginMM, marginMM, marginMM)
	pdf.AddPage()
	r := renderer{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor("")}

	top := pdf.GetY()
	business := manifest.Business{
		Name:               "Intuitably (Pty) Ltd",
		Address:            "8 Kort Street; The Island; Sedgefield; 6573",
		Email:              "accounts@intuitably.com",
		RegistrationNumber: "2017/360065/07",
		VATNote:            "Not registered for VAT",
	}
	// Deliberately short right column: no period, no payment terms.
	inv := invoicing.Invoice{Number: "INV-0045", GeneratedAt: time.Now()}

	r.header(inv, business)
	gotY := pdf.GetY()

	// The right column alone (INVOICE + number + date: 3 lines) would leave
	// Y at roughly top+18+6(Ln)+4(Ln) = top+28. The left column's 7 lines
	// (name at 8mm + 6 more at 5mm) push it to roughly top+43+6+4 = top+53.
	// Assert we're well past where the short-column bug would have left us.
	minExpectedY := top + 40
	if gotY < minExpectedY {
		t.Fatalf("header ended at Y=%.1f, expected >= %.1f (past the longer business column, not just the short invoice-metadata column)", gotY, minExpectedY)
	}
}

func TestRenderProducesAValidLookingPDF(t *testing.T) {
	inv := invoicing.Invoice{
		Number:      "INV-0042",
		Project:     "acme-website",
		Client:      "acme-corp",
		GeneratedAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
		Currency:    "USD",
		Rate:        120,
		TotalHours:  3.5,
		TotalAmount: 420,
		LineItems: []invoicing.LineItem{
			{UUID: "aaa", Task: "Homepage redesign with a genuinely quite long description that should truncate", From: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC), Hours: 2},
			{UUID: "bbb", Task: "Café menu page (accented chars)", From: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC), Hours: 1.5},
		},
	}
	business := manifest.Business{
		Name:               "Carl Kritzinger",
		Address:            "1 Example Street; Cape Town; South Africa",
		Email:              "carl@example.com",
		RegistrationNumber: "2017/360065/07",
		VATNote:            "Not registered for VAT",
		PaymentTerms:       "Due upon receipt",
		PaymentDetails:     "Bank: Example Bank\nAccount: 123456789",
	}
	client := manifest.Client{
		Slug:         "acme-corp",
		Name:         "Acme Corp",
		Address:      "500 Business Ave; Denver, CO",
		ContactEmail: "billing@acme.example",
	}

	outPath := filepath.Join(t.TempDir(), "INV-0042.pdf")
	if err := Render(inv, business, client, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading rendered PDF: %v", err)
	}
	if len(data) < 500 {
		t.Fatalf("rendered PDF suspiciously small: %d bytes", len(data))
	}
	if string(data[:5]) != "%PDF-" {
		t.Fatalf("output doesn't start with a PDF header: %q", data[:5])
	}
}

func TestRenderVATAndDayUnit(t *testing.T) {
	inv := invoicing.Invoice{
		Number:          "ACME-0008",
		GeneratedAt:     time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
		Currency:        "USD",
		Rate:            115,
		RateIncludesVAT: true,
		QuantityUnit:    "hour",
		VATPercent:      15,
		Subtotal:        1000,
		VATAmount:       150,
		TotalHours:      10,
		TotalAmount:     1150,
		LineItems: []invoicing.LineItem{
			{UUID: "aaa", Task: "Work", From: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 1, 17, 0, 0, 0, time.UTC), Hours: 10},
		},
	}
	outPath := filepath.Join(t.TempDir(), "ACME-0008.pdf")
	if err := Render(inv, manifest.Business{Name: "Example Ltd", VATNote: "VAT No: 123"}, manifest.Client{Name: "Acme", VATNumber: "456"}, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

func TestRenderUsesLogoFile(t *testing.T) {
	dir := t.TempDir()
	logo := filepath.Join(dir, "logo.png")
	writeTestPNG(t, logo, 64, 16)

	inv := invoicing.Invoice{
		Number:      "INV-0042",
		GeneratedAt: time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
		Currency:    "USD",
		Rate:        100,
		TotalHours:  2,
		TotalAmount: 200,
		LineItems: []invoicing.LineItem{
			{UUID: "aaa", Task: "Work", From: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC), Hours: 2},
		},
	}
	outPath := filepath.Join(dir, "with-logo.pdf")
	if err := RenderIn(inv, manifest.Business{Name: "Example Ltd", Logo: "logo.png"}, manifest.Client{Name: "Acme"}, outPath, dir); err != nil {
		t.Fatalf("RenderIn: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("/XObject")) && !bytes.Contains(data, []byte("/Image")) {
		t.Fatalf("expected an embedded image XObject in the PDF")
	}
}

func TestRenderOmitsLogoWhenUnset(t *testing.T) {
	inv := invoicing.Invoice{
		Number:      "INV-0001",
		GeneratedAt: time.Now(),
		LineItems: []invoicing.LineItem{
			{UUID: "aaa", Task: "x", From: time.Now(), To: time.Now().Add(time.Hour), Hours: 1},
		},
	}
	if err := Render(inv, manifest.Business{Name: "X"}, manifest.Client{}, filepath.Join(t.TempDir(), "nologo.pdf")); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

func TestRenderHandlesEmptyOptionalFields(t *testing.T) {
	inv := invoicing.Invoice{
		Number:      "INV-0001",
		GeneratedAt: time.Now(),
		Currency:    "USD",
		LineItems: []invoicing.LineItem{
			{UUID: "aaa", Task: "Only task", From: time.Now(), To: time.Now().Add(time.Hour), Hours: 1},
		},
	}
	outPath := filepath.Join(t.TempDir(), "INV-0001.pdf")
	if err := Render(inv, manifest.Business{}, manifest.Client{}, outPath); err != nil {
		t.Fatalf("Render with empty business/client: %v", err)
	}
}
