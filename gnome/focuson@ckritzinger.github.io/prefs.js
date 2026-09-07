// The settings surface that FocusOn.app put in its popover.
//
// This file runs in a separate GTK process, not inside gnome-shell, which is
// exactly why it exists: the macOS popover could open an NSOpenPanel to pick
// a data directory, and the Shell process has no equivalent — it has no GTK
// and no file chooser. A preferences window does, so the folder picker lives
// here and the in-shell menu links to it.

import Adw from 'gi://Adw';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gtk from 'gi://Gtk';

import {ExtensionPreferences} from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

import * as CSVLogger from './lib/csvLogger.js';

export default class FocusOnPreferences extends ExtensionPreferences {
    fillPreferencesWindow(window) {
        const settings = this.getSettings();

        const page = new Adw.PreferencesPage({
            title: 'FocusOn',
            icon_name: 'view-list-symbolic',
        });
        window.add(page);

        page.add(this._dataGroup(window));
        page.add(this._widgetGroup(settings));
        page.add(this._cliGroup());
    }

    _dataGroup(window) {
        const group = new Adw.PreferencesGroup({
            title: 'Data',
            description: 'Where your logged time and invoices live. ' +
                'The focuson CLI reads this same setting, from the same file — ' +
                'set it on either side and both follow.',
        });

        const row = new Adw.ActionRow({
            title: 'Data directory',
            subtitle: CSVLogger.dataDirectory(),
        });

        const button = new Gtk.Button({
            label: 'Choose…',
            valign: Gtk.Align.CENTER,
        });
        button.connect('clicked', () => this._chooseDataDirectory(window, row));
        row.add_suffix(button);
        row.activatable_widget = button;

        group.add(row);
        return group;
    }

    _chooseDataDirectory(window, row) {
        const applyPath = path => {
            if (!path)
                return;
            CSVLogger.setDataDirectory(path);
            CSVLogger.bootstrapDataDirectoryIfNeeded();
            row.subtitle = path;
        };

        const initial = Gio.File.new_for_path(CSVLogger.dataDirectory());

        // Gtk.FileDialog is GTK 4.10+; every GNOME this extension supports
        // ships something newer. The fallback is kept for the case where it
        // isn't introspectable rather than because a supported version
        // lacks it — a broken picker here would strand the one setting that
        // has no other in-app home.
        if (Gtk.FileDialog) {
            const dialog = new Gtk.FileDialog({
                title: 'Choose FocusOn data directory',
                initial_folder: initial,
                modal: true,
            });
            dialog.select_folder(window, null, (source, result) => {
                try {
                    applyPath(source.select_folder_finish(result)?.get_path());
                } catch {
                    // Dismissed. GTK reports a cancelled chooser as an
                    // error, and a cancel is not a failure worth logging.
                }
            });
            return;
        }

        const chooser = new Gtk.FileChooserNative({
            title: 'Choose FocusOn data directory',
            action: Gtk.FileChooserAction.SELECT_FOLDER,
            transient_for: window,
            modal: true,
        });
        chooser.set_current_folder(initial);
        chooser.connect('response', (self, response) => {
            if (response === Gtk.ResponseType.ACCEPT)
                applyPath(self.get_file()?.get_path());
            self.destroy();
        });
        chooser.show();
    }

    _widgetGroup(settings) {
        const group = new Adw.PreferencesGroup({title: 'Widget'});

        const showWidget = new Adw.SwitchRow({
            title: 'Show the floating widget',
            subtitle: 'Time is tracked either way — the top bar icon stays.',
        });
        settings.bind('show-widget', showWidget, 'active', Gio.SettingsBindFlags.DEFAULT);
        group.add(showWidget);

        const prompt = new Adw.SwitchRow({
            title: 'Ask what you are working on at startup',
            subtitle: 'Only when nothing is being tracked.',
        });
        settings.bind('prompt-on-startup', prompt, 'active', Gio.SettingsBindFlags.DEFAULT);
        group.add(prompt);

        const resetRow = new Adw.ActionRow({
            title: 'Widget position',
            subtitle: 'Drag the widget anywhere; it stays put across restarts.',
        });
        const reset = new Gtk.Button({
            label: 'Reset to corner',
            valign: Gtk.Align.CENTER,
        });
        reset.connect('clicked', () => {
            // -1 is the "never positioned" sentinel the widget falls back
            // on; it watches for it and re-corners itself.
            settings.set_int('widget-x', -1);
            settings.set_int('widget-y', -1);
        });
        resetRow.add_suffix(reset);
        resetRow.activatable_widget = reset;
        group.add(resetRow);

        return group;
    }

    _cliGroup() {
        const group = new Adw.PreferencesGroup({
            title: 'Command line',
            description: 'Invoicing, clients, projects and backups live in the ' +
                'focuson CLI. Run it with no arguments for the interactive menu.',
        });

        const row = new Adw.ActionRow({
            title: 'Daily backup',
            subtitle: GLib.find_program_in_path('focuson') !== null
                ? 'focuson cron install — schedules a systemd user timer that commits and pushes your data directory'
                : 'focuson is not on your PATH yet — see the README for how to install it',
        });
        row.add_prefix(new Gtk.Image({icon_name: 'folder-download-symbolic'}));
        group.add(row);

        return group;
    }
}
