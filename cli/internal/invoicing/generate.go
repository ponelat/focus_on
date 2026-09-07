// Part 1 of invoice generation: computing and persisting the structured
// invoice record. PDF rendering (spec_v2.md Phase 8) reads this data but is
// a separate step, not implemented here.
package invoicing

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
)

// Options bounds an invoice run. From/To are optional (nil = unbounded) —
// the normal case is "everything unbilled," per spec_v2.md's Invoice
// Generation Flow.
type Options struct {
	ProjectSlug string
	From        *time.Time
	To          *time.Time
}

// Preview computes an invoice without writing anything — this is both the
// TUI's review step and the CLI's --dry-run. Calling it twice in a row
// yields the same result, since it has no side effects.
func Preview(dataDir string, man manifest.Manifest, opts Options) (Invoice, error) {
	project, ok := man.FindProject(opts.ProjectSlug)
	if !ok {
		return Invoice{}, fmt.Errorf("no project with slug %q", opts.ProjectSlug)
	}
	if !project.Billable() {
		return Invoice{}, fmt.Errorf("project %q has no client — nothing to invoice", opts.ProjectSlug)
	}
	billing, ok := man.EffectiveBilling(project)
	if !ok {
		return Invoice{}, fmt.Errorf("no rate set for project %q or client %q", opts.ProjectSlug, project.Client)
	}
	client := billing.Client
	rate := billing.Rate

	taskLogPath := filepath.Join(dataDir, "projects", opts.ProjectSlug, "task_log.csv")
	rows, err := tasklog.ReadRows(taskLogPath)
	if err != nil {
		return Invoice{}, err
	}
	billed, err := billedUUIDs(dataDir, opts.ProjectSlug)
	if err != nil {
		return Invoice{}, err
	}

	var items []LineItem
	var totalHours float64
	for _, r := range rows {
		if r.To == nil { // only closed sessions have a known duration to bill
			continue
		}
		if billed[r.UUID] {
			continue
		}
		if opts.From != nil && r.To.Before(*opts.From) {
			continue
		}
		if opts.To != nil && r.To.After(*opts.To) {
			continue
		}
		clock := r.To.Sub(r.From)
		if clock < time.Minute {
			// A same-instant (or few-second) start/stop — noise, not real
			// work, and displays as a useless "0.00h" line at 2-decimal
			// precision even when technically nonzero. Left out of the
			// invoice entirely; a UUID this short is worth rounding to
			// nothing either way, so there's no billing harm in it never
			// getting marked as invoiced.
			continue
		}
		qty := clock.Hours()
		if billing.Unit == manifest.RateUnitDay {
			qty = qty / billing.HoursPerDay
		}
		items = append(items, LineItem{UUID: r.UUID, Task: r.Task, From: r.From, To: *r.To, Hours: qty})
		totalHours += qty
	}
	sort.Slice(items, func(i, j int) bool { return items[i].From.Before(items[j].From) })

	subtotal, vatAmount, total := VATBreakdown(totalHours, rate, client.VATPercent, client.RateIncludesVAT)
	inv := Invoice{
		Project:         opts.ProjectSlug,
		Client:          project.Client,
		GeneratedAt:     time.Now(),
		Currency:        client.Currency,
		Rate:            rate,
		RateIncludesVAT: client.RateIncludesVAT,
		QuantityUnit:    billing.Unit,
		VATPercent:      client.VATPercent,
		Subtotal:        subtotal,
		VATAmount:       vatAmount,
		TotalHours:      totalHours,
		TotalAmount:     total,
		LineItems:       items,
	}
	if opts.From != nil {
		inv.PeriodFrom = opts.From.Format(time.RFC3339)
	}
	if opts.To != nil {
		inv.PeriodTo = opts.To.Format(time.RFC3339)
	}
	return inv, nil
}

// Commit runs Preview, then assigns the next invoice number, writes the
// ledger file, and appends invoiced.csv rows for every billed UUID — the
// actual double-billing-prevention write (spec_v2.md, "Double-billing
// prevention") — before bumping the project's scan-skip cursor. Refuses to
// create an empty invoice: nothing unbilled means nothing to commit.
func Commit(dataDir string, man manifest.Manifest, opts Options) (Invoice, error) {
	inv, err := Preview(dataDir, man, opts)
	if err != nil {
		return Invoice{}, err
	}
	if len(inv.LineItems) == 0 {
		return Invoice{}, fmt.Errorf("nothing unbilled for project %q in that range", opts.ProjectSlug)
	}

	client, ok := man.FindClient(inv.Client)
	if !ok {
		return Invoice{}, fmt.Errorf("project %q references unknown client %q", opts.ProjectSlug, inv.Client)
	}
	num := NumberingFor(client)
	next, err := NextInvoiceNumber(dataDir, num)
	if err != nil {
		return Invoice{}, err
	}
	inv.Number = FormatNumber(num, next)
	// Deterministic from the number, so it can be set before the PDF (a
	// separate rendering step — see internal/pdfgen) actually exists yet.
	inv.PDFPath = filepath.Join("invoices", inv.Number+".pdf")

	if err := SaveLedger(dataDir, inv); err != nil {
		return Invoice{}, err
	}

	rows := make([]InvoicedRow, len(inv.LineItems))
	for i, li := range inv.LineItems {
		rows[i] = InvoicedRow{UUID: li.UUID, InvoiceNumber: inv.Number, InvoicedAt: inv.GeneratedAt}
	}
	if err := appendInvoiced(dataDir, opts.ProjectSlug, rows); err != nil {
		return Invoice{}, err
	}

	if err := manifest.SetLastInvoicedAt(dataDir, opts.ProjectSlug, inv.GeneratedAt); err != nil {
		return Invoice{}, err
	}

	return inv, nil
}
