package invoicing

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

func setupProject(t *testing.T, rows ...string) (dataDir string, man manifest.Manifest) {
	t.Helper()
	dataDir = filepath.Join(t.TempDir(), "focuson-data")
	if err := manifest.Bootstrap(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.AddClient(dataDir, manifest.Client{Name: "Acme Corp", Currency: "USD", Rate: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.AddProject(dataDir, manifest.Project{Name: "Acme Website", Client: "acme-corp"}); err != nil {
		t.Fatal(err)
	}
	man, err := manifest.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dataDir, "projects", "acme-website", "task_log.csv")
	content := "uuid,task,from,to,completed\n"
	for _, r := range rows {
		content += r + "\n"
	}
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dataDir, man
}

func TestPreviewSumsClosedRowsAtEffectiveRate(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`, // 2h
		`bbb,"Task two",2026-09-02T09:00:00Z,2026-09-02T09:30:00Z,true`, // 0.5h
		`ccc,"Still going",2026-09-03T09:00:00Z,,`,                      // open — excluded
	)

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(inv.LineItems) != 2 {
		t.Fatalf("expected 2 line items (open row excluded), got %d: %+v", len(inv.LineItems), inv.LineItems)
	}
	if inv.TotalHours != 2.5 {
		t.Fatalf("expected 2.5 total hours, got %v", inv.TotalHours)
	}
	if inv.Rate != 100 || inv.Currency != "USD" {
		t.Fatalf("expected rate/currency from client, got rate=%v currency=%v", inv.Rate, inv.Currency)
	}
	if inv.TotalAmount != 250 {
		t.Fatalf("expected total amount 250, got %v", inv.TotalAmount)
	}
	if inv.Number != "" {
		t.Fatalf("Preview must not assign a number, got %q", inv.Number)
	}
}

func TestPreviewHasNoSideEffects(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`,
	)

	if _, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"}); err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if _, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"}); err != nil {
		t.Fatalf("second Preview: %v", err)
	}

	invoices, err := ListLedgers(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(invoices) != 0 {
		t.Fatalf("Preview must not write any ledger file, found %d", len(invoices))
	}
}

func TestCommitExcludesAlreadyBilledRowsRegardlessOfDate(t *testing.T) {
	// The backdated-entry scenario this design exists for: a row invoiced
	// once must never be picked up again, even with no date bound at all.
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-08-01T09:00:00Z,2026-08-01T11:00:00Z,true`,
		`bbb,"Task two",2026-09-01T09:00:00Z,2026-09-01T09:30:00Z,true`,
	)

	first, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	if len(first.LineItems) != 2 {
		t.Fatalf("expected both rows billed on first invoice, got %+v", first.LineItems)
	}

	// A backdated manual entry logged after the first invoice, dated before it.
	logPath := filepath.Join(dataDir, "projects", "acme-website", "task_log.csv")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`ccc,"Forgot to log this",2026-07-01T09:00:00Z,2026-07-01T10:00:00Z,true` + "\n")
	f.Close()

	second, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("second Commit: %v", err)
	}
	if len(second.LineItems) != 1 || second.LineItems[0].UUID != "ccc" {
		t.Fatalf("expected only the new backdated row, got %+v", second.LineItems)
	}
	if second.Number == first.Number {
		t.Fatalf("second invoice must have a different number than the first")
	}
}

func TestCommitRefusesEmptyInvoice(t *testing.T) {
	dataDir, man := setupProject(t)

	if _, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"}); err == nil {
		t.Fatalf("expected an error committing an invoice with nothing to bill")
	}
	invoices, _ := ListLedgers(dataDir)
	if len(invoices) != 0 {
		t.Fatalf("no ledger file should be written for a refused commit, found %d", len(invoices))
	}
}

func TestCommitRefusesNonBillableProject(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "focuson-data")
	if err := manifest.Bootstrap(dataDir); err != nil {
		t.Fatal(err)
	}
	man, _ := manifest.Load(dataDir)

	if _, err := Commit(dataDir, man, Options{ProjectSlug: "personal"}); err == nil {
		t.Fatalf("expected an error invoicing the non-billable personal project")
	}
}

func TestNumberingContinuesAfterPlaceholder(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`,
	)

	if _, err := SetLast(dataDir, 41, "Harvest baseline"); err != nil {
		t.Fatalf("SetLast: %v", err)
	}

	inv, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if inv.Number != "INV-0042" {
		t.Fatalf("expected numbering to continue from the placeholder as INV-0042, got %s", inv.Number)
	}
}

func TestLedgerIsImmutableOnceWritten(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`,
	)
	inv, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLedger(dataDir, inv); err == nil {
		t.Fatalf("expected SaveLedger to refuse overwriting an existing invoice")
	}
}

func TestSubMinuteLineItemsAreDropped(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Real work",2026-09-01T09:00:00Z,2026-09-01T10:00:00Z,true`,          // 1h
		`bbb,"Same-instant noise",2026-09-01T11:00:00Z,2026-09-01T11:00:00Z,true`, // 0s
		`ccc,"A few seconds",2026-09-06T10:37:00Z,2026-09-06T10:37:07Z,true`,      // 7s
		`ddd,"Exactly one minute",2026-09-06T11:00:00Z,2026-09-06T11:01:00Z,true`, // 60s — kept
	)

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(inv.LineItems) != 2 {
		t.Fatalf("expected the two sub-minute rows dropped, got %+v", inv.LineItems)
	}
	uuids := map[string]bool{inv.LineItems[0].UUID: true, inv.LineItems[1].UUID: true}
	if !uuids["aaa"] || !uuids["ddd"] {
		t.Fatalf("expected aaa (1h) and ddd (exactly 1min) kept, got %+v", inv.LineItems)
	}
}

func TestPerClientPrefixNumberingDoesNotCollide(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Task one",2026-09-01T09:00:00Z,2026-09-01T11:00:00Z,true`,
	)
	if err := manifest.UpdateClient(dataDir, "acme-corp", manifest.Client{
		Name: "Acme Corp", Currency: "USD", Rate: 100, InvoicePrefix: "ACME",
	}); err != nil {
		t.Fatal(err)
	}
	man, _ = manifest.Load(dataDir)

	if _, err := SetLastNumber(dataDir, Numbering{Prefix: "ACME", Digits: 4}, 11, "acme baseline"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetLastNumber(dataDir, Numbering{Prefix: "BETA", Digits: 4}, 7, "other client"); err != nil {
		t.Fatal(err)
	}

	inv, err := Commit(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if inv.Number != "ACME-0012" {
		t.Fatalf("got %s, want ACME-0012 (BETA-0007 must not bump ACME)", inv.Number)
	}
}

func TestDayRateConvertsClockHours(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"Sprint day",2026-09-01T09:00:00Z,2026-09-01T17:00:00Z,true`, // 8h = 1 day
		`bbb,"Half",2026-09-02T09:00:00Z,2026-09-02T13:00:00Z,true`,       // 4h = 0.5 day
	)
	if err := manifest.UpdateClient(dataDir, "acme-corp", manifest.Client{
		Name: "Acme Corp", Currency: "USD", Rate: 800, RateUnit: "day", HoursPerDay: 8,
	}); err != nil {
		t.Fatal(err)
	}
	man, _ = manifest.Load(dataDir)

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.QuantityUnit != "day" {
		t.Fatalf("QuantityUnit = %q, want day", inv.QuantityUnit)
	}
	if inv.TotalHours != 1.5 {
		t.Fatalf("billed days = %v, want 1.5", inv.TotalHours)
	}
	if inv.TotalAmount != 1200 {
		t.Fatalf("amount = %v, want 1.5*800=1200", inv.TotalAmount)
	}
}

func TestVATInclusiveExtractsFromGross(t *testing.T) {
	sub, vat, total := VATBreakdown(10, 115, 15, true)
	if total != 1150 {
		t.Fatalf("total = %v, want 1150", total)
	}
	if sub != 1000 {
		t.Fatalf("subtotal = %v, want 1000", sub)
	}
	if vat != 150 {
		t.Fatalf("vat = %v, want 150", vat)
	}
}

func TestVATExclusiveAddsOnTop(t *testing.T) {
	sub, vat, total := VATBreakdown(10, 100, 15, false)
	if sub != 1000 || vat != 150 || total != 1150 {
		t.Fatalf("got sub=%v vat=%v total=%v", sub, vat, total)
	}
}

func TestDateBoundsFilterOnToTimestamp(t *testing.T) {
	dataDir, man := setupProject(t,
		`aaa,"August",2026-08-15T09:00:00Z,2026-08-15T10:00:00Z,true`,
		`bbb,"September",2026-09-15T09:00:00Z,2026-09-15T10:00:00Z,true`,
	)
	sept1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	inv, err := Preview(dataDir, man, Options{ProjectSlug: "acme-website", From: &sept1})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.LineItems) != 1 || inv.LineItems[0].UUID != "bbb" {
		t.Fatalf("expected only the September row with a --from bound, got %+v", inv.LineItems)
	}
}
