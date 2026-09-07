// Port of FocusOn/ActionPopoverView.swift.
//
// The macOS popover becomes a PopupMenu, which is what the top-bar indicator
// wants anyway. The same menu is built twice — once under the indicator,
// once anchored to the floating widget — so it lives in a function that
// fills any menu rather than in a class that owns one.

import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import * as CSVLogger from './csvLogger.js';

function addItem(menu, label, iconName, onActivate) {
    const item = new PopupMenu.PopupImageMenuItem(label, iconName);
    item.connect('activate', () => onActivate());
    menu.addMenuItem(item);
    return item;
}

/**
 * Rebuilds `menu` in place. Called on every open rather than once at
 * construction, because half the items only exist while something is being
 * tracked.
 *
 * @param {PopupMenu.PopupMenuBase} menu
 * @param {object} params
 * @param {import('./taskStore.js').TaskStore} params.store
 * @param {Gio.Settings} params.settings
 * @param {object} params.actions
 */
export function buildActionMenu(menu, {store, settings, actions}) {
    menu.removeAll();

    if (store.isTracking) {
        addItem(menu, 'Complete task', 'object-select-symbolic', actions.onCompleteTask);
        addItem(menu, 'Pause task', 'media-playback-pause-symbolic', actions.onPauseTask);
        addItem(menu, 'Change task', 'edit-undo-symbolic', actions.onChangeTask);

        // Closes the session like "Complete task", but bills the elapsed time
        // to whatever you were really doing. Last in the group so the three
        // items above keep the positions muscle memory expects.
        addItem(menu, 'Actually, I…', 'edit-find-replace-symbolic', actions.onActuallyI);

        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
    }

    addItem(menu, 'Log past session', 'document-open-recent-symbolic', actions.onLogPastSession);
    menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

    // Stands in for the macOS "Launch at login" checkbox, which has no
    // meaning here: a Shell extension is running whenever the shell is. What
    // a GNOME user actually wants to turn off is the floating widget, so
    // that's what this offers.
    const widgetSwitch = new PopupMenu.PopupSwitchMenuItem(
        'Show floating widget', settings.get_boolean('show-widget'));
    widgetSwitch.connect('toggled', (item, state) => {
        settings.set_boolean('show-widget', state);
    });
    menu.addMenuItem(widgetSwitch);

    menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

    // The data directory, shown exactly as the macOS popover showed it. It
    // is not editable from here: the folder picker lives in the extension's
    // preferences window, which is a real GTK process and can therefore show
    // a real file chooser — gnome-shell itself cannot.
    const pathItem = new PopupMenu.PopupMenuItem(
        CSVLogger.dataDirectoryDisplayPath(), {reactive: false});
    pathItem.label.add_style_class_name('focuson-menu-path');
    menu.addMenuItem(pathItem);

    addItem(menu, 'Settings…', 'preferences-system-symbolic', actions.onOpenPreferences);

    menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

    // The macOS "Quit". Disabling the extension is the honest equivalent —
    // it runs the same shutdown path, which closes the open session's row
    // just as applicationWillTerminate did.
    addItem(menu, 'Turn off FocusOn', 'system-shutdown-symbolic', actions.onTurnOff);
}
