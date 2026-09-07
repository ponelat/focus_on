// Command focuson is a small personal invoicing tool that pairs with the
// FocusOn.app time-tracking widget. See spec_v2.md in the repo root.
//
// Running it with no arguments launches the interactive TUI — that's the
// primary interface (spec_v2.md, "Interface: Bubble Tea TUI (primary), flags
// (secondary)"). The flags below exist for scripting/cron use, as thin
// wrappers over the same underlying functions the TUI calls.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ckritzinger/focus_on/cli/internal/config"
	"github.com/ckritzinger/focus_on/cli/internal/cronsetup"
	"github.com/ckritzinger/focus_on/cli/internal/gitsync"
	"github.com/ckritzinger/focus_on/cli/internal/invoicing"
	"github.com/ckritzinger/focus_on/cli/internal/manifest"
	"github.com/ckritzinger/focus_on/cli/internal/pdfgen"
	"github.com/ckritzinger/focus_on/cli/internal/tasklog"
	"github.com/ckritzinger/focus_on/cli/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "focuson:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// "cron install/uninstall" manage a LaunchAgent, not focuson-data — no
	// data directory needed, so they're dispatched before everything else
	// that requires one.
	if len(args) > 0 && args[0] == "cron" {
		return runCron(args[1:])
	}

	cfg, hasCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Nothing to check yet on a genuine first run — there's no data
	// directory until the TUI's setup screen creates one, and none of the
	// flag subcommands make sense before that either.
	if hasCfg && cfg.DataDir != "" {
		if err := checkDataIntegrity(cfg.DataDir); err != nil {
			return err
		}
	}

	if len(args) == 0 {
		m := tui.New(cfg, hasCfg)
		_, err := tea.NewProgram(m).Run()
		return err
	}

	if !hasCfg || cfg.DataDir == "" {
		return fmt.Errorf("no data directory configured yet — run `focuson` with no arguments once to set one up")
	}

	switch args[0] {
	case "sync":
		return runSync(cfg.DataDir)
	case "invoice":
		return runInvoice(cfg.DataDir, args[1:])
	default:
		return fmt.Errorf("unknown command %q (try: sync, invoice generate, invoice set-last, cron install, cron uninstall)", args[0])
	}
}

func runSync(dataDir string) error {
	result, err := gitsync.Commit(dataDir, "")
	if err != nil {
		return err
	}
	if result.Committed {
		fmt.Println("committed")
	} else {
		fmt.Println("nothing to commit")
	}
	switch {
	case result.Pushed:
		fmt.Println("pushed")
	case result.PushError != nil:
		// Not a fatal error for the whole command — see gitsync.Commit's
		// doc comment: a flaky network shouldn't make the daily cron job
		// treat every run as a failure when the local commit (the actual
		// durability guarantee) still succeeded.
		fmt.Fprintln(os.Stderr, "focuson: push failed (commit is still safe locally):", result.PushError)
	}
	return nil
}

func runInvoice(dataDir string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: focuson invoice generate|set-last ...")
	}
	switch args[0] {
	case "generate":
		return runInvoiceGenerate(dataDir, args[1:])
	case "set-last":
		return runInvoiceSetLast(dataDir, args[1:])
	default:
		return fmt.Errorf("unknown invoice command %q (try: generate, set-last)", args[0])
	}
}

func runInvoiceGenerate(dataDir string, args []string) error {
	fs := flag.NewFlagSet("invoice generate", flag.ContinueOnError)
	project := fs.String("project", "", "project slug to invoice (required)")
	from := fs.String("from", "", "only bill sessions closed on/after this date (YYYY-MM-DD)")
	to := fs.String("to", "", "only bill sessions closed on/before this date (YYYY-MM-DD)")
	dryRun := fs.Bool("dry-run", false, "preview only — write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" {
		return fmt.Errorf("--project is required")
	}

	opts := invoicing.Options{ProjectSlug: *project}
	if *from != "" {
		t, err := time.ParseInLocation("2006-01-02", *from, time.Local)
		if err != nil {
			return fmt.Errorf("--from: expected YYYY-MM-DD: %w", err)
		}
		opts.From = &t
	}
	if *to != "" {
		t, err := time.ParseInLocation("2006-01-02", *to, time.Local)
		if err != nil {
			return fmt.Errorf("--to: expected YYYY-MM-DD: %w", err)
		}
		t = t.Add(24*time.Hour - time.Nanosecond)
		opts.To = &t
	}

	man, err := manifest.Load(dataDir)
	if err != nil {
		return err
	}

	if *dryRun {
		inv, err := invoicing.Preview(dataDir, man, opts)
		if err != nil {
			return err
		}
		printInvoicePreview(inv)
		return nil
	}

	inv, err := invoicing.Commit(dataDir, man, opts)
	if err != nil {
		return err
	}
	printInvoicePreview(inv)

	project2, ok := man.FindProject(inv.Project)
	if !ok {
		return fmt.Errorf("committed %s, but project %q vanished before PDF rendering", inv.Number, inv.Project)
	}
	client, ok := man.FindClient(project2.Client)
	if !ok {
		return fmt.Errorf("committed %s, but client %q vanished before PDF rendering", inv.Number, project2.Client)
	}
	if err := renderPDF(dataDir, inv, man.Business, client); err != nil {
		return fmt.Errorf("committed %s, but PDF rendering failed (can be regenerated later from the ledger): %w", inv.Number, err)
	}
	fmt.Printf("committed %s\n", inv.Number)
	return nil
}

func renderPDF(dataDir string, inv invoicing.Invoice, business manifest.Business, client manifest.Client) error {
	return pdfgen.RenderIn(inv, business, client, filepath.Join(dataDir, inv.PDFPath), dataDir)
}

func printInvoicePreview(inv invoicing.Invoice) {
	unit := "h"
	if inv.QuantityUnit == "day" {
		unit = "d"
	}
	for _, li := range inv.LineItems {
		fmt.Printf("  %s -> %s  %5.2f%s  %s\n", li.From.Format("2006-01-02 15:04"), li.To.Format("15:04"), li.Hours, unit, li.Task)
	}
	suffix := "hr"
	qty := "hours"
	if inv.QuantityUnit == "day" {
		suffix = "day"
		qty = "days"
	}
	incl := ""
	if inv.RateIncludesVAT && inv.VATPercent > 0 {
		incl = " incl VAT"
	}
	fmt.Printf("%.2f %s x %.2f %s/%s%s\n", inv.TotalHours, qty, inv.Rate, inv.Currency, suffix, incl)
	if inv.VATPercent > 0 {
		fmt.Printf("subtotal ex VAT %.2f\nVAT %.2f%% %.2f\ntotal due %.2f %s\n", inv.Subtotal, inv.VATPercent, inv.VATAmount, inv.TotalAmount, inv.Currency)
	} else {
		fmt.Printf("total due %.2f %s\n", inv.TotalAmount, inv.Currency)
	}
}

func runInvoiceSetLast(dataDir string, args []string) error {
	fs := flag.NewFlagSet("invoice set-last", flag.ContinueOnError)
	number := fs.Int("number", 0, "last invoice number issued by your old system (required)")
	prefix := fs.String("prefix", "", "per-client prefix; blank = INV")
	digits := fs.Int("digits", 0, "zero-pad width; blank = 4 (INV-0042)")
	note := fs.String("note", "Baseline import", "note stored on the placeholder invoice")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *number <= 0 {
		return fmt.Errorf("--number is required and must be positive")
	}
	num := invoicing.DefaultNumbering()
	if p := *prefix; p != "" {
		num.Prefix = p
	}
	if *digits > 0 {
		num.Digits = *digits
	}
	inv, err := invoicing.SetLastNumber(dataDir, num, *number, *note)
	if err != nil {
		return err
	}
	fmt.Printf("wrote placeholder %s — next real invoice for this prefix will be %d+1\n", inv.Number, *number)
	return nil
}

func runCron(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: focuson cron install [--time HH:MM] | focuson cron uninstall")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("cron install", flag.ContinueOnError)
		at := fs.String("time", "18:00", "daily time to sync, 24h HH:MM local time")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		hour, minute, err := cronsetup.ParseTime(*at)
		if err != nil {
			return err
		}
		path, err := cronsetup.Install(hour, minute)
		if err != nil {
			return err
		}
		fmt.Printf("installed %s, syncing daily at %02d:%02d\n", path, hour, minute)
		return nil
	case "uninstall":
		if err := cronsetup.Uninstall(); err != nil {
			return err
		}
		fmt.Println("uninstalled")
		return nil
	default:
		return fmt.Errorf("unknown cron command %q (try: install, uninstall)", args[0])
	}
}

// checkDataIntegrity refuses to start focuson against a data directory that
// has dangling (never-closed, not-currently-active) task_log.csv entries.
// Invoicing and every other CLI feature trusts a `to`-less row to mean
// exactly one thing — "still being tracked right now" — so a stale one left
// over from a crash needs a human's judgment call (was there really an hour
// of work here, or was this a five-second false start?) before the CLI will
// trust the file again. See internal/tasklog for the exact rule.
func checkDataIntegrity(dataDir string) error {
	dangling, err := tasklog.CheckDataDirectory(dataDir)
	if err != nil {
		return fmt.Errorf("could not read task logs: %w", err)
	}
	if len(dangling) == 0 {
		return nil
	}

	msg := fmt.Sprintf("refusing to start — %d dangling task_log.csv entr", len(dangling))
	if len(dangling) == 1 {
		msg += "y"
	} else {
		msg += "ies"
	}
	msg += " found (started but never closed, and not the most recent thing logged):\n\n"
	for _, d := range dangling {
		msg += fmt.Sprintf("  projects/%s/task_log.csv:%d  %s  %q  started %s, never closed\n",
			d.Project, d.Line, d.UUID, d.Task, d.From)
	}
	msg += "\nEdit the file by hand to fix it — give it a `to` and `completed` value if the\n" +
		"time really was worked, or delete the row if it wasn't — then run focuson again."
	return fmt.Errorf("%s", msg)
}
