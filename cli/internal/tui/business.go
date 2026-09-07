package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

// Business is a singleton — no list screen, straight into the edit form.
const (
	businessFieldName = iota
	businessFieldAddress
	businessFieldEmail
	businessFieldRegistrationNumber
	businessFieldVATNote
	businessFieldPaymentTerms
	businessFieldPaymentDetails
	businessFieldLogo
)

func newBusinessForm(b manifest.Business) form {
	labels := []string{
		"Name",
		"Address (\"; \"-separated lines)",
		"Email",
		"Registration number",
		"VAT note (e.g. \"Not registered for VAT\")",
		"Payment terms (e.g. \"Due upon receipt\")",
		"Payment details (\"; \"-separated lines)",
		"Logo path (PNG/JPG, relative to data dir; blank = none)",
	}
	values := []string{
		b.Name,
		b.Address,
		b.Email,
		b.RegistrationNumber,
		b.VATNote,
		b.PaymentTerms,
		b.PaymentDetails,
		b.Logo,
	}
	return newForm("Business info (printed on every invoice)", labels, values, make([]bool, len(labels)))
}

func (m Model) enterBusinessForm() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.businessForm = newBusinessForm(m.man.Business)
	m.screen = screenBusinessForm
	return m, nil
}

func (m Model) updateBusinessForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.businessForm.update(msg)
	m.businessForm = f
	if cancelled {
		m.screen = screenMenu
		return m, nil
	}
	if submitted {
		vals := f.values()
		b := manifest.Business{
			Name:               vals[businessFieldName],
			Address:            vals[businessFieldAddress],
			Email:              vals[businessFieldEmail],
			RegistrationNumber: vals[businessFieldRegistrationNumber],
			VATNote:            vals[businessFieldVATNote],
			PaymentTerms:       vals[businessFieldPaymentTerms],
			PaymentDetails:     vals[businessFieldPaymentDetails],
			Logo:               vals[businessFieldLogo],
		}
		if err := manifest.SetBusiness(m.dataDir, b); err != nil {
			m.businessForm.err = err
			return m, cmd
		}
		m.screen = screenMenu
		return m, nil
	}
	return m, cmd
}
