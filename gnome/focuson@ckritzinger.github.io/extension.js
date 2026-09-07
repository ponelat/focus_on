// Port of FocusOn/AppDelegate.swift + FocusOn/TaskTrackerApp.swift.
//
// The macOS app's lifecycle maps onto the extension's almost one for one:
// applicationDidFinishLaunching → enable(), applicationWillTerminate →
// disable(). The one place they diverge is the screen lock, and that's what
// metadata.json's "unlock-dialog" session mode is for — see
// TaskStore.writeClosingRowOnTerminate.

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import * as CSVLogger from './lib/csvLogger.js';
import {TaskStore} from './lib/taskStore.js';
import {FloatingWidget} from './lib/widget.js';
import {buildActionMenu} from './lib/actionMenu.js';
import {openLogPastSession, openTaskSelection} from './lib/dialogs.js';

/** Matches the 0.35s AppDelegate waited before opening its first task picker. */
const STARTUP_PROMPT_DELAY_MS = 700;

export default class FocusOnExtension extends Extension {
    enable() {
        this._settings = this.getSettings();
        this._store = new TaskStore(this._settings);
        this._timeouts = new Set();

        this._addIndicator();

        this._widget = new FloatingWidget({
            store: this._store,
            settings: this._settings,
            onTap: () => this._handleWidgetTap(),
        });

        this._storeUnsubscribe = this._store.connect(() => this._updateIndicator());

        this._showWidgetChangedId = this._settings.connect(
            'changed::show-widget', () => this._syncWidgetVisibility());

        // The lock screen runs in the same process, and this extension keeps
        // running through it (session-modes includes unlock-dialog) so that
        // locking your screen doesn't chop the session in two. The widget
        // itself must still go away — a task name is exactly the sort of
        // thing that should not sit on a locked screen.
        this._sessionModeId = Main.sessionMode.connect(
            'updated', () => this._syncWidgetVisibility());

        // The data directory can be repointed by the CLI's first-run setup or
        // by this extension's own preferences window, both of which are other
        // processes writing the shared config.toml. Watching the file is how
        // the running widget finds out.
        this._watchConfigFile();

        this._store.loadState();
        this._updateIndicator();
        this._syncWidgetVisibility();

        if (!this._store.isTracking && this._settings.get_boolean('prompt-on-startup'))
            this._addTimeout(STARTUP_PROMPT_DELAY_MS, () => this._showTaskSelection(false));
    }

    disable() {
        // The applicationWillTerminate analogue, and the reason this runs
        // before anything is torn down: the open session needs its closing
        // row while the store still knows what it was.
        this._store?.writeClosingRowOnTerminate();

        // Before the timeout set is torn down: closing the menu fires
        // open-state-changed, whose handler schedules one.
        this._closeWidgetMenu(true);

        for (const id of this._timeouts ?? [])
            GLib.Source.remove(id);
        this._timeouts = null;

        if (this._configMonitor) {
            this._configMonitor.cancel();
            this._configMonitor = null;
        }
        if (this._sessionModeId) {
            Main.sessionMode.disconnect(this._sessionModeId);
            this._sessionModeId = 0;
        }
        if (this._showWidgetChangedId) {
            this._settings.disconnect(this._showWidgetChangedId);
            this._showWidgetChangedId = 0;
        }
        if (this._storeUnsubscribe) {
            this._storeUnsubscribe();
            this._storeUnsubscribe = null;
        }

        this._widget?.destroy();
        this._widget = null;

        this._indicator?.destroy();
        this._indicator = null;

        this._store = null;
        this._settings = null;
    }

    /**
     * Tracked so disable() can't leave a timer firing into a torn-down
     * extension — and inert once disable() has run, since closing a menu on
     * the way out is itself something that asks for a deferred callback.
     */
    _addTimeout(ms, callback) {
        if (this._timeouts === null)
            return 0;
        const id = GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, () => {
            this._timeouts?.delete(id);
            callback();
            return GLib.SOURCE_REMOVE;
        });
        this._timeouts.add(id);
        return id;
    }

    // MARK: - Top bar indicator (the NSStatusItem)

    _addIndicator() {
        this._indicator = new PanelMenu.Button(0.5, 'FocusOn', false);

        this._indicatorIcon = new St.Icon({
            style_class: 'system-status-icon focuson-indicator-icon',
            icon_name: 'view-list-symbolic',
        });
        this._indicator.add_child(this._indicatorIcon);

        this._indicator.menu.connect('open-state-changed', (menu, open) => {
            if (open)
                this._populateActionMenu(menu);
        });

        Main.panel.addToStatusArea(this.uuid, this._indicator);
    }

    _updateIndicator() {
        if (!this._indicatorIcon)
            return;
        const tracking = this._store.isTracking;
        this._indicator.accessible_name = tracking
            ? `FocusOn — ${this._store.currentTaskName}`
            : 'FocusOn — no active task';
        if (tracking)
            this._indicatorIcon.add_style_class_name('focuson-indicator-active');
        else
            this._indicatorIcon.remove_style_class_name('focuson-indicator-active');
    }

    // MARK: - Widget visibility

    _syncWidgetVisibility() {
        if (!this._widget)
            return;
        const visible = this._settings.get_boolean('show-widget') &&
            Main.sessionMode.currentMode === 'user';
        if (visible) {
            this._widget.show();
        } else {
            this._closeWidgetMenu(true);
            this._widget.hide();
        }
    }

    // MARK: - Widget tap → menu or picker (AppDelegate.handleWidgetTap)

    _handleWidgetTap() {
        if (!this._store.isTracking)
            this._showTaskSelection(false);
        else
            this._openWidgetMenu();
    }

    /**
     * The widget's own action menu.
     *
     * Rebuilt on every open rather than kept around, because the arrow has to
     * point at the widget and the widget moves. A PopupMenu fixes its arrow
     * side at construction, so choosing between "opens downward" and "opens
     * upward" means constructing a new one — cheap, and it avoids reaching
     * into BoxPointer internals to flip an existing menu.
     */
    _openWidgetMenu() {
        this._closeWidgetMenu(true);

        const actor = this._widget.actor;
        const area = Main.layoutManager.getWorkAreaForMonitor(Main.layoutManager.primaryIndex);
        const inBottomHalf = actor.y + actor.height / 2 > area.y + area.height / 2;
        const side = inBottomHalf ? St.Side.BOTTOM : St.Side.TOP;

        const menu = new PopupMenu.PopupMenu(actor, 0.5, side);
        Main.layoutManager.uiGroup.add_child(menu.actor);
        menu.actor.hide();

        this._widgetMenuManager = new PopupMenu.PopupMenuManager(actor);
        this._widgetMenuManager.addMenu(menu);
        this._widgetMenu = menu;

        this._populateActionMenu(menu);

        menu.connect('open-state-changed', (m, open) => {
            // Deferred: destroying a menu from inside its own signal
            // handler tears the actor out from under the close animation.
            if (!open)
                this._addTimeout(0, () => this._closeWidgetMenu(false));
        });

        menu.open(true);
    }

    _closeWidgetMenu(closeFirst) {
        if (!this._widgetMenu)
            return;
        const menu = this._widgetMenu;
        this._widgetMenu = null;

        if (closeFirst)
            menu.close(false);
        this._widgetMenuManager?.removeMenu(menu);
        this._widgetMenuManager = null;
        menu.destroy();
    }

    _populateActionMenu(menu) {
        // Projects are refreshed here rather than watched: the CLI can add
        // one at any time, and every path into the menu leads to a dialog
        // that needs the current list.
        this._store.refreshAvailableProjects();

        buildActionMenu(menu, {
            store: this._store,
            settings: this._settings,
            actions: {
                onCompleteTask: () => {
                    this._store.completeCurrentTask();
                    this._showTaskSelection(true);
                },
                onPauseTask: () => this._store.pauseCurrentTask(),
                onChangeTask: () => this._showTaskSelection(false),
                onLogPastSession: () => this._showLogPastSession(),
                onOpenPreferences: () => this.openPreferences(),
                onTurnOff: () => {
                    // Deferred out of the menu's activate handler so the menu
                    // is gone before disable() destroys the actor it belongs to.
                    this._addTimeout(0, () =>
                        Main.extensionManager.disableExtension(this.uuid));
                },
            },
        });
    }

    // MARK: - Dialogs

    _showTaskSelection(completingPrevious) {
        this._closeWidgetMenu(true);
        this._store.refreshAvailableProjects();
        openTaskSelection({
            store: this._store,
            completingPrevious,
            onSelect: (name, project, completing) =>
                this._store.startTask(name, project, completing),
        });
    }

    _showLogPastSession() {
        this._closeWidgetMenu(true);
        this._store.refreshAvailableProjects();
        openLogPastSession({
            store: this._store,
            onSave: (project, task, from, to, completed) =>
                this._store.logPastSession(project, task, from, to, completed),
        });
    }

    // MARK: - Shared config.toml

    _watchConfigFile() {
        try {
            const file = Gio.File.new_for_path(CSVLogger.cliConfigPath());
            this._configMonitor = file.monitor_file(Gio.FileMonitorFlags.NONE, null);
            this._configMonitor.connect('changed', () => {
                CSVLogger.bootstrapDataDirectoryIfNeeded();
                this._store.refreshAvailableProjects();
            });
        } catch (e) {
            // Not fatal: without the monitor, a data-directory change is
            // simply picked up the next time a menu or dialog opens.
            console.warn(`FocusOn: could not watch config.toml: ${e}`);
        }
    }
}
