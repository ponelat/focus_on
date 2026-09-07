package invoicing

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

// LineItem is one billed task_log.csv row — a frozen snapshot, not a live
// reference back to the CSV (spec_v2.md, "Invoice Ledger").
type LineItem struct {
	UUID  string    `toml:"uuid"`
	Task  string    `toml:"task"`
	From  time.Time `toml:"from"`
	To    time.Time `toml:"to"`
	Hours float64   `toml:"hours"`
}

// Invoice is one invoices/<PREFIX>-<N>.toml file: a frozen historical record
// for humans and PDF rendering. It is never consulted for double-billing
// decisions — invoiced.csv is (see invoiced.go).
type Invoice struct {
	Number          string     `toml:"number"`
	Project         string     `toml:"project,omitempty"`
	Client          string     `toml:"client,omitempty"`
	GeneratedAt     time.Time  `toml:"generated_at"`
	PeriodFrom      string     `toml:"period_from,omitempty"` // RFC3339; blank = unbounded
	PeriodTo        string     `toml:"period_to,omitempty"`
	Currency        string     `toml:"currency,omitempty"`
	Rate            float64    `toml:"rate,omitempty"`
	RateIncludesVAT bool       `toml:"rate_includes_vat,omitempty"`
	QuantityUnit    string     `toml:"quantity_unit,omitempty"` // "hour" or "day"; TotalHours is in this unit
	VATPercent      float64    `toml:"vat_percent,omitempty"`
	Subtotal        float64    `toml:"subtotal,omitempty"` // ex VAT
	VATAmount       float64    `toml:"vat_amount,omitempty"`
	TotalHours      float64    `toml:"total_hours,omitempty"`  // billed qty (hours or days)
	TotalAmount     float64    `toml:"total_amount,omitempty"` // amount due, VAT-inclusive if VAT applies
	PDFPath         string     `toml:"pdf_path,omitempty"`
	Placeholder     bool       `toml:"placeholder"`
	Note            string     `toml:"note,omitempty"`
	LineItems       []LineItem `toml:"line_item,omitempty"`
}

func invoicesDir(dataDir string) string {
	return filepath.Join(dataDir, "invoices")
}

func ledgerPath(dataDir, number string) string {
	return filepath.Join(invoicesDir(dataDir), number+".toml")
}

// SaveLedger writes a new invoice file. Invoices are immutable once created
// (spec_v2.md, Out of Scope: "fixing a bad invoice means deleting the file
// by hand") — refuses to overwrite an existing one.
func SaveLedger(dataDir string, inv Invoice) error {
	if inv.Number == "" {
		return fmt.Errorf("invoice has no number")
	}
	path := ledgerPath(dataDir, inv.Number)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — invoices are never overwritten", path)
	}
	if err := os.MkdirAll(invoicesDir(dataDir), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", invoicesDir(dataDir), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(inv); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return nil
}

// LoadLedger reads one invoice file.
func LoadLedger(path string) (Invoice, error) {
	var inv Invoice
	if _, err := toml.DecodeFile(path, &inv); err != nil {
		return Invoice{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return inv, nil
}

// ListLedgers reads every invoice in the data directory, newest first.
func ListLedgers(dataDir string) ([]Invoice, error) {
	entries, err := os.ReadDir(invoicesDir(dataDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", invoicesDir(dataDir), err)
	}
	var invoices []Invoice
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		inv, err := LoadLedger(filepath.Join(invoicesDir(dataDir), e.Name()))
		if err != nil {
			return nil, err
		}
		invoices = append(invoices, inv)
	}
	sort.Slice(invoices, func(i, j int) bool {
		return invoices[i].GeneratedAt.After(invoices[j].GeneratedAt)
	})
	return invoices, nil
}

const (
	defaultPrefix = "INV"
	defaultDigits = 4
)

// Numbering is one invoice sequence. The default is INV-0042; a client may
// override the prefix (and optionally the pad width) without affecting others.
type Numbering struct {
	Prefix string
	Digits int
}

func DefaultNumbering() Numbering {
	return Numbering{Prefix: defaultPrefix, Digits: defaultDigits}
}

func NumberingFor(c manifest.Client) Numbering {
	n := DefaultNumbering()
	if p := strings.TrimSpace(c.InvoicePrefix); p != "" {
		n.Prefix = strings.ToUpper(p)
	}
	if c.InvoiceDigits > 0 {
		n.Digits = c.InvoiceDigits
	}
	return n
}

func FormatNumber(n Numbering, seq int) string {
	if n.Prefix == "" {
		n.Prefix = defaultPrefix
	}
	n.Prefix = strings.ToUpper(n.Prefix)
	if n.Digits <= 0 {
		n.Digits = defaultDigits
	}
	return fmt.Sprintf("%s-%0*d", n.Prefix, n.Digits, seq)
}

// SplitNumber parses "INV-0042" into ("INV", 42).
func SplitNumber(number string) (prefix string, seq int, err error) {
	i := strings.LastIndex(number, "-")
	if i <= 0 || i == len(number)-1 {
		return "", 0, fmt.Errorf("invoice number %q not PREFIX-N", number)
	}
	seq, err = strconv.Atoi(number[i+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invoice number %q: %w", number, err)
	}
	return number[:i], seq, nil
}

// NextInvoiceNumber is max(existing numbers for this prefix) + 1. Other
// prefixes are ignored, so ACME-0007 does not push a client using INV- to 0008.
func NextInvoiceNumber(dataDir string, num Numbering) (int, error) {
	if num.Prefix == "" {
		num.Prefix = defaultPrefix
	}
	invoices, err := ListLedgers(dataDir)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, inv := range invoices {
		p, seq, err := SplitNumber(inv.Number)
		if err != nil {
			return 0, fmt.Errorf("invoice %q has an unparseable number: %w", inv.Number, err)
		}
		if !strings.EqualFold(p, num.Prefix) {
			continue
		}
		if seq > max {
			max = seq
		}
	}
	return max + 1, nil
}

// SetLast creates a placeholder invoice so the next real invoice continues
// numbering from wherever an old system left off. Uses the legacy INV-000N
// sequence; see SetLastNumber for a per-client prefix.
func SetLast(dataDir string, number int, note string) (Invoice, error) {
	return SetLastNumber(dataDir, DefaultNumbering(), number, note)
}

func SetLastNumber(dataDir string, num Numbering, number int, note string) (Invoice, error) {
	inv := Invoice{
		Number:      FormatNumber(num, number),
		GeneratedAt: time.Now(),
		Placeholder: true,
		Note:        note,
	}
	if err := SaveLedger(dataDir, inv); err != nil {
		return Invoice{}, err
	}
	return inv, nil
}
