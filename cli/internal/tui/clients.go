package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

func (m Model) enterClientsList() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.clientCursor = 0
	m.screen = screenClientsList
	return m, nil
}

func (m Model) clientListItems() []string {
	items := make([]string, 0, len(m.man.Clients)+1)
	for _, c := range m.man.Clients {
		label := c.Name
		if label == "" {
			label = c.Slug
		}
		if c.Rate != 0 {
			label = fmt.Sprintf("%s (%s %.2f/%s)", label, c.Currency, c.Rate, c.UnitLabel())
		} else if c.Currency != "" {
			label = fmt.Sprintf("%s (%s)", label, c.Currency)
		}
		if c.InvoicePrefix != "" {
			label += " · " + strings.ToUpper(c.InvoicePrefix) + "-"
		}
		items = append(items, label)
	}
	items = append(items, addNewLabel)
	return items
}

func (m Model) updateClientsList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.clientListItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k":
		if m.clientCursor > 0 {
			m.clientCursor--
		}
	case "down", "j":
		if m.clientCursor < len(items)-1 {
			m.clientCursor++
		}
	case "enter":
		if m.clientCursor == len(items)-1 {
			m.editingClientSlug = ""
			m.clientForm = newClientForm(false, manifest.Client{})
			m.screen = screenClientForm
			return m, nil
		}
		c := m.man.Clients[m.clientCursor]
		m.editingClientSlug = c.Slug
		m.clientForm = newClientForm(true, c)
		m.screen = screenClientForm
	}
	return m, nil
}

// client form field order.
const (
	clientFieldName = iota
	clientFieldSlug
	clientFieldCurrency
	clientFieldRate
	clientFieldRateUnit
	clientFieldHoursPerDay
	clientFieldVATPercent
	clientFieldRateIncludesVAT
	clientFieldVATNumber
	clientFieldInvoicePrefix
	clientFieldInvoiceDigits
	clientFieldAddress
	clientFieldContactEmail
)

func newClientForm(editing bool, c manifest.Client) form {
	labels := []string{
		"Name",
		"Slug",
		"Currency",
		"Rate",
		"Rate unit (hour or day)",
		"Hours per day (day billing; default 8)",
		"VAT % (0 = none)",
		"Rate includes VAT (true/false)",
		"VAT number",
		"Invoice prefix (blank = INV)",
		"Invoice digits (blank = 4, like INV-0042)",
		"Address",
		"Contact email",
	}
	values := []string{c.Name, c.Slug, c.Currency, "", c.RateUnit, "", "", "", c.VATNumber, c.InvoicePrefix, "", c.Address, c.ContactEmail}
	if c.Rate != 0 {
		values[clientFieldRate] = strconv.FormatFloat(c.Rate, 'f', -1, 64)
	}
	if c.HoursPerDay != 0 {
		values[clientFieldHoursPerDay] = strconv.FormatFloat(c.HoursPerDay, 'f', -1, 64)
	}
	if c.VATPercent != 0 {
		values[clientFieldVATPercent] = strconv.FormatFloat(c.VATPercent, 'f', -1, 64)
	}
	if c.RateIncludesVAT {
		values[clientFieldRateIncludesVAT] = "true"
	}
	if c.InvoiceDigits != 0 {
		values[clientFieldInvoiceDigits] = strconv.Itoa(c.InvoiceDigits)
	}
	readOnly := make([]bool, len(labels))
	readOnly[clientFieldSlug] = editing // slug is set once at creation, then immutable

	title := "Add client"
	if editing {
		title = "Edit client: " + c.Slug
	}
	return newForm(title, labels, values, readOnly)
}

func (m Model) updateClientForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.clientForm.update(msg)
	m.clientForm = f
	if cancelled {
		m.screen = screenClientsList
		return m, nil
	}
	if submitted {
		vals := f.values()
		rate, err := parseOptionalFloat(vals[clientFieldRate])
		if err != nil {
			m.clientForm.err = fmt.Errorf("rate: %v", err)
			return m, cmd
		}
		hpd, err := parseOptionalFloat(vals[clientFieldHoursPerDay])
		if err != nil {
			m.clientForm.err = fmt.Errorf("hours per day: %v", err)
			return m, cmd
		}
		vat, err := parseOptionalFloat(vals[clientFieldVATPercent])
		if err != nil {
			m.clientForm.err = fmt.Errorf("VAT %%: %v", err)
			return m, cmd
		}
		digits, err := parseOptionalInt(vals[clientFieldInvoiceDigits])
		if err != nil {
			m.clientForm.err = fmt.Errorf("invoice digits: %v", err)
			return m, cmd
		}
		unit := strings.ToLower(vals[clientFieldRateUnit])
		if unit != "" && unit != manifest.RateUnitHour && unit != manifest.RateUnitDay {
			m.clientForm.err = fmt.Errorf("rate unit: want hour or day, got %q", vals[clientFieldRateUnit])
			return m, cmd
		}
		inclVAT, err := parseOptionalBool(vals[clientFieldRateIncludesVAT])
		if err != nil {
			m.clientForm.err = fmt.Errorf("rate includes VAT: %v", err)
			return m, cmd
		}
		c := manifest.Client{
			Slug:            vals[clientFieldSlug],
			Name:            vals[clientFieldName],
			Currency:        vals[clientFieldCurrency],
			Rate:            rate,
			RateUnit:        unit,
			HoursPerDay:     hpd,
			VATPercent:      vat,
			RateIncludesVAT: inclVAT,
			VATNumber:       vals[clientFieldVATNumber],
			InvoicePrefix:   strings.ToUpper(vals[clientFieldInvoicePrefix]),
			InvoiceDigits:   digits,
			Address:         vals[clientFieldAddress],
			ContactEmail:    vals[clientFieldContactEmail],
		}
		var opErr error
		if m.editingClientSlug == "" {
			_, opErr = manifest.AddClient(m.dataDir, c)
		} else {
			opErr = manifest.UpdateClient(m.dataDir, m.editingClientSlug, c)
		}
		if opErr != nil {
			m.clientForm.err = opErr
			return m, cmd
		}
		return m.enterClientsList()
	}
	return m, cmd
}

func (m Model) viewClientsList() string {
	subtitle := ""
	if m.loadErr != nil {
		subtitle = "error loading manifest: " + m.loadErr.Error()
	}
	return renderList("Clients", subtitle, m.clientListItems(), m.clientCursor,
		"↑/↓ to move · enter to select/add · esc back to menu")
}

func parseOptionalFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

func parseOptionalInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	return n, err
}

func parseOptionalBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "n":
		return false, nil
	case "1", "true", "yes", "y":
		return true, nil
	default:
		return false, fmt.Errorf("want true/false, got %q", s)
	}
}
