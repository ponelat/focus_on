package tui

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/manifest"
)

func (m Model) enterProjectsList() (tea.Model, tea.Cmd) {
	m.reloadManifest()
	m.projectCursor = 0
	m.screen = screenProjectsList
	return m, nil
}

func (m Model) projectListItems() []string {
	items := make([]string, 0, len(m.man.Projects)+1)
	for _, p := range m.man.Projects {
		label := p.Name
		if label == "" {
			label = p.Slug
		}
		if p.Client == "" {
			label += " (personal, non-billable)"
		} else if b, ok := m.man.EffectiveBilling(p); ok {
			label += fmt.Sprintf(" (%s @ %.2f/%s)", p.Client, b.Rate, b.Client.UnitLabel())
		} else {
			label += fmt.Sprintf(" (%s)", p.Client)
		}
		items = append(items, label)
	}
	items = append(items, addNewLabel)
	return items
}

func (m Model) updateProjectsList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.projectListItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k":
		if m.projectCursor > 0 {
			m.projectCursor--
		}
	case "down", "j":
		if m.projectCursor < len(items)-1 {
			m.projectCursor++
		}
	case "enter":
		if m.projectCursor == len(items)-1 {
			return m.enterProjectClientPicker(false, manifest.Project{})
		}
		p := m.man.Projects[m.projectCursor]
		return m.enterProjectClientPicker(true, p)
	}
	return m, nil
}

// --- Client picker step -----------------------------------------------
//
// A project's client is picked from the existing client list, never typed —
// mistyping a slug would silently create a non-billable project instead of
// erroring. "Personal (no client)" is always the first option.

func (m Model) enterProjectClientPicker(editing bool, p manifest.Project) (tea.Model, tea.Cmd) {
	m.draftProject = p
	if editing {
		m.editingProjectSlug = p.Slug
	} else {
		m.editingProjectSlug = ""
	}
	m.projectClientCursor = 0
	for i, c := range m.man.Clients {
		if c.Slug == p.Client {
			m.projectClientCursor = i + 1
			break
		}
	}
	m.screen = screenProjectClientPicker
	return m, nil
}

func (m Model) projectClientPickerItems() []string {
	items := make([]string, 0, len(m.man.Clients)+1)
	items = append(items, "Personal (no client)")
	for _, c := range m.man.Clients {
		label := c.Name
		if label == "" {
			label = c.Slug
		}
		if c.Rate != 0 {
			label = fmt.Sprintf("%s (%s %.2f/hr)", label, c.Currency, c.Rate)
		}
		items = append(items, label)
	}
	return items
}

func (m Model) updateProjectClientPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.projectClientPickerItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenProjectsList
	case "up", "k":
		if m.projectClientCursor > 0 {
			m.projectClientCursor--
		}
	case "down", "j":
		if m.projectClientCursor < len(items)-1 {
			m.projectClientCursor++
		}
	case "enter":
		clientSlug := ""
		if m.projectClientCursor > 0 {
			clientSlug = m.man.Clients[m.projectClientCursor-1].Slug
		}
		m.draftProject.Client = clientSlug
		editing := m.editingProjectSlug != ""
		m.projectForm = newProjectForm(editing, m.draftProject, items[m.projectClientCursor])
		m.screen = screenProjectForm
	}
	return m, nil
}

func (m Model) viewProjectClientPicker() string {
	return renderList("Client for project", "", m.projectClientPickerItems(), m.projectClientCursor,
		"↑/↓ to move · enter to select · esc to cancel")
}

// --- Name/slug/rate form step -------------------------------------------

// project form field order.
const (
	projectFieldName = iota
	projectFieldSlug
	projectFieldClient // always read-only — set by the picker step, not typed
	projectFieldRate
)

func newProjectForm(editing bool, p manifest.Project, clientDisplay string) form {
	labels := []string{"Name", "Slug", "Client", "Rate override (blank = client's rate)"}
	values := []string{p.Name, p.Slug, clientDisplay, ""}
	if p.Rate != 0 {
		values[projectFieldRate] = strconv.FormatFloat(p.Rate, 'f', -1, 64)
	}
	readOnly := make([]bool, len(labels))
	readOnly[projectFieldSlug] = editing // slug names projects/<slug>/ on disk — immutable once created
	readOnly[projectFieldClient] = true  // always picked, never typed

	title := "Add project"
	if editing {
		title = "Edit project: " + p.Slug
	}
	return newForm(title, labels, values, readOnly)
}

func (m Model) updateProjectForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f, cmd, submitted, cancelled := m.projectForm.update(msg)
	m.projectForm = f
	if cancelled {
		m.screen = screenProjectsList
		return m, nil
	}
	if submitted {
		vals := f.values()
		rate, err := parseOptionalFloat(vals[projectFieldRate])
		if err != nil {
			m.projectForm.err = fmt.Errorf("rate: %v", err)
			return m, cmd
		}
		p := manifest.Project{
			Slug:           vals[projectFieldSlug],
			Name:           vals[projectFieldName],
			Client:         m.draftProject.Client, // from the picker step, not the (display-only) form field
			Rate:           rate,
			LastInvoicedAt: m.draftProject.LastInvoicedAt,
		}
		var opErr error
		if m.editingProjectSlug == "" {
			_, opErr = manifest.AddProject(m.dataDir, p)
		} else {
			opErr = manifest.UpdateProject(m.dataDir, m.editingProjectSlug, p)
		}
		if opErr != nil {
			m.projectForm.err = opErr
			return m, cmd
		}
		return m.enterProjectsList()
	}
	return m, cmd
}

func (m Model) viewProjectsList() string {
	subtitle := ""
	if m.loadErr != nil {
		subtitle = "error loading manifest: " + m.loadErr.Error()
	}
	return renderList("Projects", subtitle, m.projectListItems(), m.projectCursor,
		"↑/↓ to move · enter to select/add · esc back to menu")
}
