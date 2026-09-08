# FocusOn

A local-first personal time-tracking and invoicing tool. Two pieces:

- **The widget** — keeps your current task visible and logs work sessions. **FocusOn.app** on macOS; a **GNOME Shell extension** on Linux (see [`gnome/`](gnome/README.md)).
- **`focuson`** — a companion CLI that turns logged time into invoices: clients, projects, rates, PDF generation, and git-backed durability.

A data directory is portable between platforms — sync one over git and the macOS widget, the GNOME widget and the CLI all read and write it interchangeably.

No accounts. No cloud. No subscription. Everything lives in plain CSV/TOML files in a git repo you control.

## Features

**Widget**
- Floating widget that stays above all windows, including full-screen apps; persists across Spaces
- Draggable, position remembered across restarts; menu bar icon for quick access; optional launch at login
- Tracks time per project — pick a project when starting a task, or add a task to whichever project you had going
- **Log past session** — for time you forgot to start the timer for; logs a single backdated entry, independent of whatever's currently active
- On startup, refuses to hand off a task_log.csv with a session that looks abandoned (started, never closed, buried under later entries) rather than silently ignoring the problem — see [Data integrity](#data-integrity)

**CLI (`focuson`)**
- Manage clients, projects (with per-project rate override), and your own business info (address, registration number, VAT note, payment terms, bank details) — all printed on generated invoices
- Generate invoices from logged time: pick a project, optional date range, review the computed line items and total *before* anything is written, then confirm
- Renders each invoice to PDF automatically
- UUID-based double-billing prevention — a session invoiced once never gets billed again, even months later with no date filter
- "Check consistency" — flags anything that looks billed twice or forgotten
- `focuson sync` commits your data directory to git and pushes to `origin` if you've set one up
- `focuson cron install` schedules a daily automatic sync — a LaunchAgent on macOS, a systemd user timer on Linux, a Task Scheduler task on Windows — so you can't forget

## Requirements

- macOS 13.0 (Ventura) or later, **or** GNOME Shell 45–49 (see [`gnome/README.md`](gnome/README.md))
- [Go](https://go.dev) if you want the CLI (the widget alone doesn't need it) — `go build` will fetch the exact toolchain version `cli/go.mod` requires automatically
- `git`, with a remote configured for your data directory if you want off-machine backup (recommended)

## Install

### Linux / GNOME

```bash
./install-gnome.sh
```

On NixOS, use the flake instead — it ships the CLI, the extension, a NixOS module and a Home Manager module. Both routes are covered in [`gnome/README.md`](gnome/README.md).

### macOS

```bash
./install.sh
```

This builds the widget and installs it to `/Applications/FocusOn.app` (relaunching if already running); builds `focuson` and installs it to `~/bin/focuson`; and schedules the daily sync cron job for midnight. If `~/bin` isn't on your `PATH`, the script tells you so — it won't edit your shell profile for you. If `go` isn't installed, the widget still installs and the script skips the CLI/cron steps with a warning.

## First run

The first time you open the widget or run `focuson`, you'll be asked where your data should live (defaults to `~/focuson-data`). Both read/write the same directory and the same config file, so it doesn't matter which one you set up first.

## Widget usage

Click the widget or the menu bar icon to:

- **Complete task** — marks the current task done and prompts for the next one
- **Pause task** — stops the timer without marking it done
- **Change task** — switches task without marking the current one complete
- **Log past session** — backdate a session you forgot to track live
- **Data directory** — shows the current path; click **Change…** to move it

On first launch with no active task, the task picker opens automatically — pick (or type) a task, pick a project, go.

## `focuson` CLI usage

Run `focuson` with no arguments for the interactive menu: **Business**, **Clients**, **Projects**, **Invoices**, **Sync**. Everything below is also available as flags for scripting:

```bash
focuson invoice generate --project acme-website [--from 2026-09-01] [--to 2026-09-30] [--dry-run]
focuson invoice set-last --number 41 --note "Baseline import"
focuson sync
focuson cron install [--time 18:00]
focuson cron uninstall
```

`--dry-run` previews line items and totals without writing anything — the same review step the TUI shows before you confirm a real generate.

## Data directory layout

```
~/focuson-data/                 (a git repo)
├── manifest.toml                # business info, clients, projects/rates
├── projects/
│   └── <project-slug>/
│       ├── task_log.csv         # append-only, widget-owned
│       └── invoiced.csv         # append-only, CLI-owned — which sessions were billed on which invoice
└── invoices/
    ├── INV-0042.toml            # frozen invoice record
    └── INV-0042.pdf
```

`task_log.csv` schema:

```
uuid,task,from,to,completed
```

| Column | Description |
|---|---|
| `uuid` | Generated per session; ties a session's start and end rows together and is how invoicing tracks what's been billed |
| `task` | Free-text task name |
| `from` | ISO 8601 start time with timezone offset |
| `to` | ISO 8601 end time, empty if still active |
| `completed` | `true` / `false`, empty if still active |

Append-only — sessions are never edited or deleted by the widget. A completed session shows up as two rows sharing one `uuid` (an open marker written when it starts, a close row written when it ends); only the closed row counts for billing.

## Data integrity

Every `focuson` run checks each project's `task_log.csv` for a session that was started but never closed *and* isn't simply the most recent thing logged (which is given the benefit of the doubt as still in progress). Anything else unclosed is presumed abandoned — usually a crash — and `focuson` refuses to do anything else until you fix it by hand: give the row a `to`/`completed` value if the time was real, or delete it if it wasn't.

## Backups

`focuson sync` (and the daily cron job) stages, commits, and pushes your data directory. On Linux the job is a systemd user timer with `Persistent=true`, so a laptop that was closed at the scheduled time still syncs when it comes back; its output goes to the journal (`journalctl --user -u focuson-sync.service`) rather than to a log file. Push is best-effort — a failed push (no network, no remote configured) never blocks the local commit, since that's the actual durability guarantee. Set up a remote (a private GitHub repo works well) for real off-machine backup:

```bash
cd ~/focuson-data
git remote add origin git@github.com:you/focuson-data.git
git push -u origin main
```

## Project layout

```
FocusOn/                        # the widget (Swift)
├── TaskTrackerApp.swift        # @main entry point
├── AppDelegate.swift           # Panel, status item, lifecycle
├── FloatingPanel.swift         # NSPanel subclass + drag-handling container view
├── WidgetView.swift            # SwiftUI widget (dot + label + chevron)
├── ActionPopoverView.swift     # Complete / Pause / Change / Log past session / Settings popover
├── TaskSelectionView.swift     # Project picker + recent tasks list + new task input
├── LogPastSessionView.swift    # Backdated manual time entry
├── TaskStore.swift             # State management, CSV coordination
└── CSVLogger.swift             # File I/O, CSV formatting, shared config with the CLI

gnome/                          # the widget on GNOME — a Shell extension (JavaScript)
└── focuson@ckritzinger.github.io/   # see gnome/README.md for the full layout

nix/                            # Nix packages and NixOS / Home Manager modules

cli/                            # the focuson CLI (Go)
├── main.go                     # TUI launch + flag subcommands (sync, invoice, cron)
└── internal/
    ├── config/       # shared config.toml (data directory path)
    ├── manifest/     # manifest.toml: business/clients/projects
    ├── tasklog/      # task_log.csv reader + startup integrity check
    ├── invoicing/    # invoice generation, numbering, recon
    ├── pdfgen/       # PDF rendering
    ├── gitsync/      # commit + push
    ├── cronsetup/    # daily sync job: LaunchAgent (darwin) / systemd timer (linux) / Task Scheduler (windows)
    └── tui/          # Bubble Tea screens
```

See `spec_v2.md` for the full design and implementation history, and [`gnome/README.md`](gnome/README.md) for what the GNOME port changed and why.
