// Package pdfgen renders an already-computed invoicing.Invoice to a PDF file.
// This is deliberately separate from internal/invoicing: that package
// computes and persists the structured invoice record with zero rendering
// dependencies (spec_v2.md Phase 7); this package only ever reads that data
// to produce a document (Phase 8). Pure Go, no external binary — go-pdf/fpdf.
package pdfgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

const (
	marginMM  = 15.0
	pageWidth = 210.0 // A4 portrait, mm
	usableW   = pageWidth - 2*marginMM

	colDateW   = 28.0
	colHoursW  = 20.0
	colAmountW = 32.0

	// Letterhead mark, not a banner. Wide wordmarks at 78×16mm dominated
	// the page; 48×10mm sits in the header without shouting.
	logoMaxW = 48.0
	logoMaxH = 10.0
	logoGap  = 2.0
)

// renderer bundles the pdf handle with a UTF-8-to-codepage translator: the
// core Helvetica font only understands a single-byte codepage, not UTF-8, so
// every piece of free text (task names, addresses, names) has to go through
// tr() before it reaches the page or accented characters render as garbage.
type renderer struct {
	pdf     *fpdf.Fpdf
	tr      func(string) string
	dataDir string
}

// Render writes inv as a PDF to outPath. business and client supply the
// header/footer info that isn't itself part of the frozen invoice record
// (the invoice only stores the client's slug and the currency/rate it billed
// at — see spec_v2.md, "Invoice Ledger").
func Render(inv invoicing.Invoice, business manifest.Business, client manifest.Client, outPath string) error {
	return RenderIn(inv, business, client, outPath, "")
}

// RenderIn is Render plus a data directory, used to resolve a relative
// business.Logo path.
func RenderIn(inv invoicing.Invoice, business manifest.Business, client manifest.Client, outPath, dataDir string) error {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginMM, marginMM, marginMM)
	pdf.SetAutoPageBreak(true, marginMM)
	pdf.AddPage()

	r := renderer{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor(""), dataDir: dataDir}

	r.header(inv, business)
	r.billTo(client)
	r.lineItems(inv)
	r.totals(inv)
	r.paymentDetails(business)

	if err := pdf.OutputFileAndClose(outPath); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}
	return nil
}

func (r renderer) cell(w, h float64, txt, align string, fill bool) {
	r.pdf.CellFormat(w, h, r.tr(txt), "", 0, align, fill, 0, "")
}

func (r renderer) cellLn(w, h float64, txt, align string, fill bool) {
	r.pdf.CellFormat(w, h, r.tr(txt), "", 1, align, fill, 0, "")
}

func (r renderer) borderedCell(w, h float64, txt, align string, fill bool, ln int) {
	r.pdf.CellFormat(w, h, r.tr(txt), "1", ln, align, fill, 0, "")
}

func (r renderer) header(inv invoicing.Invoice, business manifest.Business) {
	pdf := r.pdf
	top := pdf.GetY()
	leftY := r.placeLogo(business, top)

	logoPlaced := leftY > top

	pdf.SetY(leftY)
	pdf.SetX(marginMM)
	// With a logo the mark is the identity; the legal name is a caption,
	// not a second headline. Without a logo the name still has to carry
	// the header, so it stays 14pt bold.
	nameH, bodyH := 6.0, 5.0
	if logoPlaced {
		pdf.SetFont("Helvetica", "", 9)
		nameH, bodyH = 4.5, 4.2
	} else {
		pdf.SetFont("Helvetica", "B", 14)
	}
	pdf.MultiCell(usableW/2, nameH, r.tr(business.Name), "", "L", false)

	pdf.SetFont("Helvetica", "", 9)
	if !logoPlaced {
		pdf.SetFont("Helvetica", "", 10)
	}
	for _, line := range addressLines(business.Address) {
		r.cellLn(usableW/2, bodyH, line, "L", false)
	}
	if business.Email != "" {
		r.cellLn(usableW/2, bodyH, business.Email, "L", false)
	}
	if business.RegistrationNumber != "" {
		r.cellLn(usableW/2, bodyH, "Reg number: "+business.RegistrationNumber, "L", false)
	}
	if business.VATNote != "" {
		r.cellLn(usableW/2, bodyH, business.VATNote, "L", false)
	}
	leftEndY := pdf.GetY()

	pdf.SetXY(marginMM+usableW/2, top)
	pdf.SetFont("Helvetica", "B", 14)
	r.cellLn(usableW/2, 6, "INVOICE", "R", false)

	pdf.SetX(marginMM + usableW/2)
	pdf.SetFont("Helvetica", "", 10)
	r.cellLn(usableW/2, 5, inv.Number, "R", false)

	pdf.SetX(marginMM + usableW/2)
	r.cellLn(usableW/2, 5, "Date: "+inv.GeneratedAt.Format("2006-01-02"), "R", false)

	if business.PaymentTerms != "" {
		pdf.SetX(marginMM + usableW/2)
		r.cellLn(usableW/2, 5, "Due: "+business.PaymentTerms, "R", false)
	}

	if inv.PeriodFrom != "" || inv.PeriodTo != "" {
		pdf.SetX(marginMM + usableW/2)
		r.cellLn(usableW/2, 5, "Period: "+formatDate(inv.PeriodFrom)+" - "+formatDate(inv.PeriodTo), "R", false)
	}
	rightEndY := pdf.GetY()

	// The two columns run independently and rarely end at the same height
	// (business details commonly run longer than the three or four lines of
	// invoice metadata) — continuing from whichever column's cursor happens
	// to be current, instead of the lower of the two, is what let "Bill To"
	// start overlapping the tail of a long business address in practice.
	pdf.SetY(maxFloat(leftEndY, rightEndY))
	pdf.Ln(6)
	drawRule(pdf)
	pdf.Ln(4)
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (r renderer) placeLogo(business manifest.Business, top float64) float64 {
	info, name := r.registerLogo(business)
	if info == nil || name == "" {
		return top
	}
	natW, natH := info.Extent()
	if natW <= 0 || natH <= 0 {
		return top
	}
	w := logoMaxW
	h := w * natH / natW
	if h > logoMaxH {
		h = logoMaxH
		w = h * natW / natH
	}
	r.pdf.Image(name, marginMM, top, w, h, false, "", 0, "")
	return top + h + logoGap
}

func (r renderer) registerLogo(business manifest.Business) (*fpdf.ImageInfoType, string) {
	logo := strings.TrimSpace(business.Logo)
	if logo == "-" {
		return nil, ""
	}
	if logo != "" {
		p := logo
		if !filepath.IsAbs(p) && r.dataDir != "" {
			p = filepath.Join(r.dataDir, p)
		}
		if _, err := os.Stat(p); err != nil {
			return nil, ""
		}
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
		if ext == "jpeg" {
			ext = "jpg"
		}
		info := r.pdf.RegisterImageOptions(p, fpdf.ImageOptions{ImageType: ext, ReadDpi: true})
		if info == nil || r.pdf.Err() {
			r.pdf.ClearError() // don't fail the whole invoice over a bad logo file
			return nil, ""
		}
		return info, p
	}
	return nil, ""
}

func (r renderer) billTo(client manifest.Client) {
	pdf := r.pdf
	pdf.SetFont("Helvetica", "B", 10)
	r.cellLn(usableW, 5, "Bill To", "L", false)

	pdf.SetFont("Helvetica", "", 10)
	name := client.Name
	if name == "" {
		name = client.Slug
	}
	r.cellLn(usableW, 5, name, "L", false)
	for _, line := range addressLines(client.Address) {
		r.cellLn(usableW, 5, line, "L", false)
	}
	if client.ContactEmail != "" {
		r.cellLn(usableW, 5, client.ContactEmail, "L", false)
	}
	if client.VATNumber != "" {
		r.cellLn(usableW, 5, "VAT No: "+client.VATNumber, "L", false)
	}
	pdf.Ln(6)
}

func (r renderer) lineItems(inv invoicing.Invoice) {
	pdf := r.pdf
	taskColW := usableW - colDateW - colHoursW - colAmountW

	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(235, 235, 235)
	r.borderedCell(colDateW, 7, "Date", "L", true, 0)
	r.borderedCell(taskColW, 7, "Task", "L", true, 0)
	r.borderedCell(colHoursW, 7, qtyHeader(inv), "R", true, 0)
	r.borderedCell(colAmountW, 7, "Amount", "R", true, 1)

	pdf.SetFont("Helvetica", "", 9)
	for _, li := range inv.LineItems {
		amount := li.Hours * inv.Rate
		r.borderedCell(colDateW, 6, li.From.Format("2006-01-02"), "L", false, 0)
		r.borderedCell(taskColW, 6, r.truncate(li.Task, taskColW-2), "L", false, 0)
		r.borderedCell(colHoursW, 6, fmt.Sprintf("%.2f", li.Hours), "R", false, 0)
		r.borderedCell(colAmountW, 6, fmt.Sprintf("%.2f", amount), "R", false, 1)
	}
	pdf.Ln(4)
}

func qtyHeader(inv invoicing.Invoice) string {
	if inv.QuantityUnit == "day" {
		return "Days"
	}
	return "Hours"
}

func unitSuffix(inv invoicing.Invoice) string {
	if inv.QuantityUnit == "day" {
		return "day"
	}
	return "hr"
}

func (r renderer) totals(inv invoicing.Invoice) {
	pdf := r.pdf
	labelW := usableW - colAmountW
	unit := unitSuffix(inv)
	rateNote := ""
	if inv.RateIncludesVAT && inv.VATPercent > 0 {
		rateNote = " incl VAT"
	}

	pdf.SetFont("Helvetica", "", 10)
	r.cell(labelW, 6, fmt.Sprintf("Total %s (at %.2f %s/%s%s)", strings.ToLower(qtyHeader(inv)), inv.Rate, inv.Currency, unit, rateNote), "R", false)
	r.cellLn(colAmountW, 6, fmt.Sprintf("%.2f", inv.TotalHours), "R", false)

	if inv.VATPercent > 0 {
		r.cell(labelW, 6, "Subtotal ex VAT", "R", false)
		r.cellLn(colAmountW, 6, fmt.Sprintf("%.2f", inv.Subtotal), "R", false)
		r.cell(labelW, 6, fmt.Sprintf("VAT %.2f%%", inv.VATPercent), "R", false)
		r.cellLn(colAmountW, 6, fmt.Sprintf("%.2f", inv.VATAmount), "R", false)
	}

	pdf.SetFont("Helvetica", "B", 11)
	r.cell(labelW, 8, "Total due", "R", false)
	r.cellLn(colAmountW, 8, fmt.Sprintf("%.2f %s", inv.TotalAmount, inv.Currency), "R", false)
	pdf.Ln(6)
}

func (r renderer) paymentDetails(business manifest.Business) {
	if business.PaymentDetails == "" {
		return
	}
	pdf := r.pdf
	drawRule(pdf)
	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 9)
	r.cellLn(usableW, 5, "Payment details", "L", false)
	pdf.SetFont("Helvetica", "", 9)
	// Split like addressLines rather than one wrapped paragraph: the TUI's
	// form field for this is a single line, so "; "-separated parts (bank
	// name; account holder; account number; ...) is the input convention —
	// see internal/tui's business form.
	for _, line := range addressLines(business.PaymentDetails) {
		r.cellLn(usableW, 5, line, "L", false)
	}
}

func drawRule(pdf *fpdf.Fpdf) {
	y := pdf.GetY()
	pdf.SetDrawColor(180, 180, 180)
	pdf.Line(marginMM, y, marginMM+usableW, y)
}

// addressLines splits free-text address fields on common separators — the
// manifest doesn't constrain how an address is entered (spec_v2.md's example
// uses "; " between parts) — into one PDF line each.
func addressLines(address string) []string {
	if address == "" {
		return nil
	}
	replaced := strings.ReplaceAll(address, "\n", ";")
	var lines []string
	for _, part := range strings.Split(replaced, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func formatDate(rfc3339 string) string {
	if rfc3339 == "" {
		return "?"
	}
	if len(rfc3339) >= 10 {
		return rfc3339[:10]
	}
	return rfc3339
}

// truncate keeps task descriptions from overflowing the fixed-width column —
// good enough for a personal invoicing tool's line items without the added
// complexity of dynamic multi-line row heights. Measures width post-tr()
// since that's what actually gets drawn on the page.
func (r renderer) truncate(s string, maxWidth float64) string {
	translated := r.tr(s)
	if r.pdf.GetStringWidth(translated) <= maxWidth {
		return s
	}
	const ellipsis = "..."
	runes := []rune(s)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := r.tr(string(runes) + ellipsis)
		if r.pdf.GetStringWidth(candidate) <= maxWidth {
			return string(runes) + ellipsis
		}
	}
	return ellipsis
}
