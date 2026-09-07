package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/pdfgen"
)

func formatOptionalDate(t *time.Time) string {
	if t == nil {
		return "…"
	}
	return t.Format("2006-01-02")
}

func quantityUnitLabel(inv invoicing.Invoice) string {
	if inv.QuantityUnit == "day" {
		return "days"
	}
	return "hours"
}

func quantityUnitShort(inv invoicing.Invoice) string {
	if inv.QuantityUnit == "day" {
		return "day"
	}
	return "hr"
}

func invoiceTotals(inv invoicing.Invoice) string {
	unit := quantityUnitLabel(inv)
	short := quantityUnitShort(inv)
	incl := ""
	if inv.RateIncludesVAT && inv.VATPercent > 0 {
		incl = " incl VAT"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%.2f %s × %.2f %s/%s%s", inv.TotalHours, unit, inv.Rate, inv.Currency, short, incl))
	if inv.VATPercent > 0 {
		b.WriteString(fmt.Sprintf("\nSubtotal ex VAT  %.2f\nVAT %.2f%%          %.2f\nTotal due        %.2f %s",
			inv.Subtotal, inv.VATPercent, inv.VATAmount, inv.TotalAmount, inv.Currency))
	} else {
		b.WriteString(fmt.Sprintf(" = %.2f %s", inv.TotalAmount, inv.Currency))
	}
	return b.String()
}

// --- Invoices list -------------------------------------------------------

const (
	invoiceActionGenerate = "+ Generate new invoice"
	invoiceActionSetLast  = "+ Set starting number (Harvest continuity)"
	invoiceActionRecon    = "+ Check consistency"
	invoiceActionCount    = 3 // how many of the above precede the real invoice list
)

func (m Model) enterInvoicesList() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.invoiceList, m.invoiceListErr = invoicing.ListLedgers(m.dataDir)
	m.invoiceCursor = 0
	m.screen = screenInvoicesList
	return m, nil
}

func (m Model) invoiceListItems() []string {
	items := []string{invoiceActionGenerate, invoiceActionSetLast, invoiceActionRecon}
	for _, inv := range m.invoiceList {
		label := fmt.Sprintf("%s — %s — %.2f %s (%d item(s))", inv.Number, inv.Project, inv.TotalAmount, inv.Currency, len(inv.LineItems))
		if inv.Placeholder {
			label = fmt.Sprintf("%s — placeholder (%s)", inv.Number, inv.Note)
		}
		items = append(items, label)
	}
	return items
}

func (m Model) updateInvoicesList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.invoiceListItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k":
		if m.invoiceCursor > 0 {
			m.invoiceCursor--
		}
	case "down", "j":
		if m.invoiceCursor < len(items)-1 {
			m.invoiceCursor++
		}
	case "enter":
		switch m.invoiceCursor {
		case 0:
			return m.enterInvoiceProjectPicker()
		case 1:
			m.invoiceSetLastForm = newSetLastForm()
			m.screen = screenInvoiceSetLast
		case 2:
			return m.enterInvoiceRecon()
		default:
			m.invoiceDetail = m.invoiceList[m.invoiceCursor-invoiceActionCount]
			m.screen = screenInvoiceDetail
		}
	}
	return m, nil
}

func (m Model) viewInvoicesList() string {
	subtitle := ""
	if m.invoiceListErr != nil {
		subtitle = "error loading invoices: " + m.invoiceListErr.Error()
	}
	return renderList("Invoices", subtitle, m.invoiceListItems(), m.invoiceCursor,
		"↑/↓ to move · enter to select · esc back to menu")
}

// --- Project picker (which billable project to invoice) ------------------

func (m Model) billableProjectSlugs() []string {
	var slugs []string
	for _, p := range m.man.Projects {
		if p.Billable() {
			slugs = append(slugs, p.Slug)
		}
	}
	return slugs
}

func (m Model) enterInvoiceProjectPicker() (tea.Model, tea.Cmd) {
	m.invoiceProjectSlugs = m.billableProjectSlugs()
	if len(m.invoiceProjectSlugs) == 0 {
		m.invoiceListErr = fmt.Errorf("no billable projects yet — add a client and project first")
		m.screen = screenInvoicesList
		return m, nil
	}
	m.invoiceProjectCursor = 0
	m.screen = screenInvoiceProjectPicker
	return m, nil
}

func (m Model) updateInvoiceProjectPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenInvoicesList
	case "up", "k":
		if m.invoiceProjectCursor > 0 {
			m.invoiceProjectCursor--
		}
	case "down", "j":
		if m.invoiceProjectCursor < len(m.invoiceProjectSlugs)-1 {
			m.invoiceProjectCursor++
		}
	case "enter":
		m.invoicePreviewProject = m.invoiceProjectSlugs[m.invoiceProjectCursor]
		m.invoiceDateBoundsForm = newInvoiceDateBoundsForm()
		m.screen = screenInvoiceDateBounds
	}
	return m, nil
}

func (m Model) viewInvoiceProjectPicker() string {
	return renderList("Invoice which project?", "", m.invoiceProjectSlugs, m.invoiceProjectCursor,
		"↑/↓ to move · enter to continue · esc to cancel")
}

// --- Optional date bounds (blank = everything unbilled) -------------------

const (
	dateBoundsFieldFrom = iota
	dateBoundsFieldTo
)

func newInvoiceDateBoundsForm() form {
	return newForm("Date range (optional — blank means everything unbilled)",
		[]string{"From (YYYY-MM-DD, blank = no lower bound)", "To (YYYY-MM-DD, blank = no upper bound)"},
		[]string{"", ""},
		[]bool{false, false})
}

// parseDateBound parses a YYYY-MM-DD in local time. from=true anchors it to
// the start of that day, from=false to the end — so "--to 2026-09-06"
// includes the whole day, not just its first instant.
func parseDateBound(s string, startOfDay bool) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil, fmt.Errorf("expected YYYY-MM-DD, got %q", s)
	}
	if !startOfDay {
		d = d.Add(24*time.Hour - time.Nanosecond)
	}
	return &d, nil
}

func (m Model) updateInvoiceDateBounds(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.invoiceDateBoundsForm.update(msg)
	m.invoiceDateBoundsForm = f
	if cancelled {
		m.screen = screenInvoiceProjectPicker
		return m, nil
	}
	if submitted {
		vals := f.values()
		from, err := parseDateBound(vals[dateBoundsFieldFrom], true)
		if err != nil {
			m.invoiceDateBoundsForm.err = fmt.Errorf("from: %v", err)
			return m, cmd
		}
		to, err := parseDateBound(vals[dateBoundsFieldTo], false)
		if err != nil {
			m.invoiceDateBoundsForm.err = fmt.Errorf("to: %v", err)
			return m, cmd
		}
		m.invoiceOptions = invoicing.Options{ProjectSlug: m.invoicePreviewProject, From: from, To: to}
		m.invoicePreview, m.invoicePreviewErr = invoicing.Preview(m.dataDir, m.man, m.invoiceOptions)
		m.invoiceCommitErr = nil
		m.screen = screenInvoiceReview
		return m, nil
	}
	return m, cmd
}

// --- Review (the dry-run) + commit ---------------------------------------

func (m Model) updateInvoiceReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenInvoicesList
	case "enter":
		if m.invoicePreviewErr != nil {
			return m, nil // nothing valid to commit
		}
		inv, err := invoicing.Commit(m.dataDir, m.man, m.invoiceOptions)
		if err != nil {
			m.invoiceCommitErr = err
			return m, nil
		}
		// The invoice is already committed at this point — a PDF failure
		// here doesn't undo it (SaveLedger inside Commit already refuses to
		// be called twice for the same number, so there's nothing to roll
		// back to). Surface the error but still return to the list; the PDF
		// can be regenerated later from the ledger since it's just a render
		// of already-frozen data.
		pdfErr := m.renderInvoicePDF(inv)
		next, cmd := m.enterInvoicesList()
		nm := next.(Model)
		if pdfErr != nil {
			nm.invoiceListErr = fmt.Errorf("invoice %s committed, but PDF rendering failed: %w", inv.Number, pdfErr)
		}
		return nm, cmd
	}
	return m, nil
}

func (m Model) renderInvoicePDF(inv invoicing.Invoice) error {
	project, ok := m.man.FindProject(inv.Project)
	if !ok {
		return fmt.Errorf("project %q no longer in manifest", inv.Project)
	}
	client, ok := m.man.FindClient(project.Client)
	if !ok {
		return fmt.Errorf("client %q no longer in manifest", project.Client)
	}
	outPath := filepath.Join(m.dataDir, inv.PDFPath)
	return pdfgen.RenderIn(inv, m.man.Business, client, outPath, m.dataDir)
}

func (m Model) viewInvoiceReview() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Review invoice: "+m.invoicePreviewProject) + "\n")
	if m.invoiceOptions.From != nil || m.invoiceOptions.To != nil {
		b.WriteString(dimStyle.Render(fmt.Sprintf("range: %s → %s", formatOptionalDate(m.invoiceOptions.From), formatOptionalDate(m.invoiceOptions.To))) + "\n")
	}

	if m.invoicePreviewErr != nil {
		b.WriteString(errorStyle.Render(m.invoicePreviewErr.Error()) + "\n\n")
		b.WriteString(dimStyle.Render("esc back"))
		return b.String()
	}

	inv := m.invoicePreview
	if len(inv.LineItems) == 0 {
		b.WriteString("Nothing unbilled for this project.\n\n")
		b.WriteString(dimStyle.Render("esc back"))
		return b.String()
	}

	unit := quantityUnitLabel(inv)
	for _, li := range inv.LineItems {
		b.WriteString(fmt.Sprintf("  %s → %s  %5.2f%s  %s\n",
			li.From.Format("2006-01-02 15:04"), li.To.Format("15:04"), li.Hours, unit[:1], li.Task))
	}
	b.WriteString("\n" + invoiceTotals(inv) + "\n")

	if m.invoiceCommitErr != nil {
		b.WriteString("\n" + errorStyle.Render(m.invoiceCommitErr.Error()) + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("enter to confirm & commit (writes the ledger + PDF, marks these billed) · esc to cancel"))
	return b.String()
}

// --- Set starting number (Harvest continuity) -----------------------------

const (
	setLastFieldNumber = iota
	setLastFieldPrefix
	setLastFieldDigits
	setLastFieldNote
)

func newSetLastForm() form {
	return newForm("Set starting invoice number",
		[]string{
			"Last number issued (e.g. 41)",
			"Prefix (blank = INV)",
			"Digits (blank = 4, like INV-0042)",
			"Note",
		},
		[]string{"", "", "", "Baseline import"},
		[]bool{false, false, false, false})
}

func (m Model) updateInvoiceSetLast(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.invoiceSetLastForm.update(msg)
	m.invoiceSetLastForm = f
	if cancelled {
		m.screen = screenInvoicesList
		return m, nil
	}
	if submitted {
		vals := f.values()
		n, err := strconv.Atoi(vals[setLastFieldNumber])
		if err != nil {
			m.invoiceSetLastForm.err = fmt.Errorf("starting number: %v", err)
			return m, cmd
		}
		digits, err := parseOptionalInt(vals[setLastFieldDigits])
		if err != nil {
			m.invoiceSetLastForm.err = fmt.Errorf("digits: %v", err)
			return m, cmd
		}
		num := invoicing.DefaultNumbering()
		if p := strings.TrimSpace(vals[setLastFieldPrefix]); p != "" {
			num.Prefix = strings.ToUpper(p)
		}
		if digits > 0 {
			num.Digits = digits
		}
		if _, err := invoicing.SetLastNumber(m.dataDir, num, n, vals[setLastFieldNote]); err != nil {
			m.invoiceSetLastForm.err = err
			return m, cmd
		}
		return m.enterInvoicesList()
	}
	return m, cmd
}

// --- Read-only detail view for an existing invoice ------------------------

func (m Model) updateInvoiceDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.screen = screenInvoicesList
	}
	return m, nil
}

func (m Model) viewInvoiceDetail() string {
	inv := m.invoiceDetail
	var b strings.Builder
	b.WriteString(titleStyle.Render(inv.Number) + "\n")
	if inv.Placeholder {
		b.WriteString("Placeholder — " + inv.Note + "\n")
		b.WriteString("\n" + dimStyle.Render("any key to go back"))
		return b.String()
	}
	b.WriteString(fmt.Sprintf("Project: %s (client %s)\n", inv.Project, inv.Client))
	b.WriteString(fmt.Sprintf("Generated: %s\n", inv.GeneratedAt.Format("2006-01-02 15:04")))
	if inv.PeriodFrom != "" || inv.PeriodTo != "" {
		b.WriteString(fmt.Sprintf("Period: %s → %s\n", inv.PeriodFrom, inv.PeriodTo))
	}
	b.WriteString(invoiceTotals(inv) + "\n")
	b.WriteString(fmt.Sprintf("%d line item(s)\n", len(inv.LineItems)))
	if inv.PDFPath != "" {
		b.WriteString(fmt.Sprintf("PDF: %s\n", inv.PDFPath))
	}
	b.WriteString("\n" + dimStyle.Render("any key to go back"))
	return b.String()
}

// --- Consistency check (recon) --------------------------------------------

// reconStaleAfter is how old a closed, billable-length, never-invoiced
// session has to be before it's worth flagging — recent work is just "not
// invoiced yet", not a problem. See internal/invoicing.Recon.
const reconStaleAfter = 14 * 24 * time.Hour

func (m Model) enterInvoiceRecon() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.invoiceReconIssues, m.invoiceReconErr = invoicing.Recon(m.dataDir, m.man, reconStaleAfter)
	m.screen = screenInvoiceRecon
	return m, nil
}

func (m Model) updateInvoiceRecon(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.screen = screenInvoicesList
	}
	return m, nil
}

func (m Model) viewInvoiceRecon() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Consistency check") + "\n")

	if m.invoiceReconErr != nil {
		b.WriteString(errorStyle.Render(m.invoiceReconErr.Error()) + "\n\n")
		b.WriteString(dimStyle.Render("any key to go back"))
		return b.String()
	}
	if len(m.invoiceReconIssues) == 0 {
		b.WriteString("No issues found across any billable project.\n\n")
		b.WriteString(dimStyle.Render("any key to go back"))
		return b.String()
	}
	for _, iss := range m.invoiceReconIssues {
		switch iss.Kind {
		case "duplicate":
			b.WriteString(errorStyle.Render(fmt.Sprintf("[%s] DUPLICATE  %s — %s", iss.Project, iss.UUID, iss.Detail)) + "\n")
		case "stale":
			b.WriteString(fmt.Sprintf("[%s] STALE      %s — %q, %s\n", iss.Project, iss.UUID, iss.Task, iss.Detail))
		}
	}
	b.WriteString("\n" + dimStyle.Render("any key to go back"))
	return b.String()
}
