// Package manifest reads and writes manifest.toml — the CLI-owned config of
// business info, clients, and projects. The widget never touches this file;
// it only ever lists projects/ subdirectories (see spec_v2.md).
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const FileName = "manifest.toml"

type Business struct {
	Name               string `toml:"name"`
	Address            string `toml:"address"`
	Email              string `toml:"email"`
	RegistrationNumber string `toml:"registration_number,omitempty"` // company reg number, if any
	VATNote            string `toml:"vat_note,omitempty"`            // e.g. "Not registered for VAT" or "VAT No: ..." — free text, no jurisdiction assumed
	PaymentTerms       string `toml:"payment_terms,omitempty"`       // e.g. "Due upon receipt", "Net 30" — printed as-is, not computed into a date
	PaymentDetails     string `toml:"payment_details"`
	Logo               string `toml:"logo,omitempty"` // optional PNG/JPG path (relative to the data dir, or absolute). Blank or "-" = no logo.
}

const (
	RateUnitHour       = "hour"
	RateUnitDay        = "day"
	defaultHoursPerDay = 8
)

type Client struct {
	Slug            string  `toml:"slug"`
	Name            string  `toml:"name"`
	Currency        string  `toml:"currency"`
	Rate            float64 `toml:"rate"`
	RateUnit        string  `toml:"rate_unit,omitempty"`     // "hour" (default) or "day"
	HoursPerDay     float64 `toml:"hours_per_day,omitempty"` // clock-hours that count as 1 day; default 8
	VATPercent      float64 `toml:"vat_percent,omitempty"`   // 0 = no VAT on invoices
	RateIncludesVAT bool    `toml:"rate_includes_vat,omitempty"`
	VATNumber       string  `toml:"vat_number,omitempty"`
	InvoicePrefix   string  `toml:"invoice_prefix,omitempty"` // blank => "INV"
	InvoiceDigits   int     `toml:"invoice_digits,omitempty"` // pad width; 0 => 4 (INV-0042)
	Address         string  `toml:"address"`
	ContactEmail    string  `toml:"contact_email"`
}

type Project struct {
	Slug           string  `toml:"slug"`
	Client         string  `toml:"client"` // blank => non-billable
	Name           string  `toml:"name"`
	Rate           float64 `toml:"rate,omitempty"`             // overrides client rate if set
	LastInvoicedAt string  `toml:"last_invoiced_at,omitempty"` // ISO 8601; scan-skip optimization only, never billing truth
}

type Manifest struct {
	Business Business  `toml:"business"`
	Clients  []Client  `toml:"client"`
	Projects []Project `toml:"project"`
}

func manifestPath(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Load reads manifest.toml from the data directory.
func Load(dataDir string) (Manifest, error) {
	var m Manifest
	if _, err := toml.DecodeFile(manifestPath(dataDir), &m); err != nil {
		return Manifest{}, fmt.Errorf("loading manifest: %w", err)
	}
	return m, nil
}

// Save writes manifest.toml to the data directory.
func Save(dataDir string, m Manifest) error {
	f, err := os.Create(manifestPath(dataDir))
	if err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(m); err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	return nil
}

// Exists reports whether a manifest.toml already exists in dataDir.
func Exists(dataDir string) bool {
	_, err := os.Stat(manifestPath(dataDir))
	return err == nil
}

// Bootstrap creates a fresh data directory with a minimal manifest (just the
// "personal" project) if one doesn't exist yet. If a manifest already exists,
// the directory is left untouched. Mirrors the widget's own bootstrap logic
// (spec_v2.md, "Data directory instead of a log file") so either side can be
// the first to touch a brand-new data directory.
func Bootstrap(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	if Exists(dataDir) {
		return nil
	}
	m := Manifest{
		Projects: []Project{
			{Slug: "personal", Client: "", Name: "Personal"},
		},
	}
	if err := Save(dataDir, m); err != nil {
		return err
	}
	// Create the project directory (not the CSV itself — the CLI never
	// writes task_log.csv, only the widget does) so "personal" shows up
	// immediately in the widget's directory-listing project picker.
	if err := os.MkdirAll(filepath.Join(dataDir, "projects", "personal"), 0o755); err != nil {
		return fmt.Errorf("creating personal project directory: %w", err)
	}
	return nil
}

// FindProject returns the project with the given slug, if any.
func (m Manifest) FindProject(slug string) (Project, bool) {
	for _, p := range m.Projects {
		if p.Slug == slug {
			return p, true
		}
	}
	return Project{}, false
}

// FindClient returns the client with the given slug, if any.
func (m Manifest) FindClient(slug string) (Client, bool) {
	for _, c := range m.Clients {
		if c.Slug == slug {
			return c, true
		}
	}
	return Client{}, false
}

// Billable reports whether a project has a client attached.
func (p Project) Billable() bool {
	return p.Client != ""
}

// EffectiveRate returns the project's rate override, falling back to its
// client's rate. A zero rate is "not set", not "free".
func (m Manifest) EffectiveRate(p Project) (float64, bool) {
	b, ok := m.EffectiveBilling(p)
	return b.Rate, ok
}

// Billing is the resolved rate + unit + VAT/numbering for one project.
type Billing struct {
	Client      Client
	Rate        float64
	Unit        string  // RateUnitHour or RateUnitDay
	HoursPerDay float64 // only meaningful when Unit is day
}

func (c Client) BillingUnit() string {
	if strings.EqualFold(c.RateUnit, RateUnitDay) {
		return RateUnitDay
	}
	return RateUnitHour
}

func (c Client) HoursPerDayOrDefault() float64 {
	if c.HoursPerDay > 0 {
		return c.HoursPerDay
	}
	return defaultHoursPerDay
}

func (c Client) UnitLabel() string {
	if c.BillingUnit() == RateUnitDay {
		return "day"
	}
	return "hr"
}

// EffectiveBilling resolves rate (project override, else client) plus the
// client's unit/VAT/numbering. ok is false when there's no non-zero rate.
func (m Manifest) EffectiveBilling(p Project) (Billing, bool) {
	c, ok := m.FindClient(p.Client)
	if !ok {
		return Billing{Unit: RateUnitHour, HoursPerDay: defaultHoursPerDay}, false
	}
	b := Billing{Client: c, Unit: c.BillingUnit(), HoursPerDay: c.HoursPerDayOrDefault(), Rate: c.Rate}
	if p.Rate != 0 {
		b.Rate = p.Rate
	}
	return b, b.Rate != 0
}
