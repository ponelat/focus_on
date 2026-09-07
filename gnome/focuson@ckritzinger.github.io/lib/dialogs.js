// Ports of FocusOn/TaskSelectionView.swift and
// FocusOn/LogPastSessionView.swift.
//
// The macOS originals are NSPopovers anchored to the widget. A Shell
// extension has no such thing for anything with a text field in it — key
// focus in a Shell popup menu is a fight — so both become ModalDialogs,
// which is what the Shell itself uses whenever it needs typed input. That
// trades "appears next to the widget" for "reliably takes the keyboard",
// which is the right way round for a dialog whose first action is typing.
//
// Both dialogs are composed from plain St widgets rather than subclassing
// ModalDialog, so this extension registers no GObject types of its own and
// has nothing to collide on a disable/enable cycle.

import Clutter from 'gi://Clutter';
import St from 'gi://St';

import * as ModalDialog from 'resource:///org/gnome/shell/ui/modalDialog.js';

import {boxLayout, setScrollChild} from './compat.js';
import {formatElapsed, formatLocalDateTime, formatRelative, parseLocalDateTime} from './format.js';

/**
 * SwiftUI's Picker has no St equivalent, and a popup menu inside a modal
 * dialog is the kind of thing that works until someone has eight projects.
 * A scrolling row of chips shows the choice and the options at once, which
 * is what the picker was really for.
 */
class ProjectChooser {
    /**
     * @param {string[]} projects
     * @param {string} selected
     * @param {(slug: string) => void} onChange
     */
    constructor(projects, selected, onChange) {
        this.selected = projects.includes(selected) ? selected : projects[0];
        this._onChange = onChange;
        this._buttons = new Map();

        const row = boxLayout(false, {style_class: 'focuson-chip-row'});
        for (const slug of projects) {
            const button = new St.Button({
                style_class: 'focuson-chip',
                label: slug,
                can_focus: true,
            });
            button.connect('clicked', () => this._select(slug));
            this._buttons.set(slug, button);
            row.add_child(button);
        }

        this.actor = new St.ScrollView({
            style_class: 'focuson-chip-scroll',
            hscrollbar_policy: St.PolicyType.EXTERNAL,
            vscrollbar_policy: St.PolicyType.NEVER,
            x_expand: true,
        });
        setScrollChild(this.actor, row);
        this._updateChecked();
    }

    _select(slug) {
        if (slug === this.selected)
            return;
        this.selected = slug;
        this._updateChecked();
        this._onChange(slug);
    }

    _updateChecked() {
        for (const [slug, button] of this._buttons) {
            if (slug === this.selected)
                button.add_style_class_name('focuson-chip-selected');
            else
                button.remove_style_class_name('focuson-chip-selected');
        }
    }
}

/** A labelled on/off row standing in for SwiftUI's .toggleStyle(.checkbox). */
class CheckRow {
    constructor(label, checked) {
        this.checked = checked;

        this._icon = new St.Icon({
            style_class: 'focuson-check-icon',
            icon_name: 'object-select-symbolic',
            y_align: Clutter.ActorAlign.CENTER,
        });
        this._icon.opacity = checked ? 255 : 0;

        const content = boxLayout(false, {style_class: 'focuson-check-content'});
        content.add_child(this._icon);
        content.add_child(new St.Label({
            text: label,
            y_align: Clutter.ActorAlign.CENTER,
        }));

        this.actor = new St.Button({
            style_class: 'focuson-check',
            can_focus: true,
            child: content,
            x_align: Clutter.ActorAlign.START,
        });
        this.actor.connect('clicked', () => {
            this.checked = !this.checked;
            this._icon.opacity = this.checked ? 255 : 0;
        });
    }
}

function heading(text) {
    return new St.Label({style_class: 'focuson-dialog-heading', text});
}

function fieldLabel(text) {
    return new St.Label({style_class: 'focuson-field-label', text});
}

/**
 * Port of TaskSelectionView. Pick a project, pick a recent task or type a new
 * one, go.
 *
 * @param {object} params
 * @param {import('./taskStore.js').TaskStore} params.store
 * @param {boolean} params.completingPrevious whether accepting also completes the task being replaced
 * @param {(name: string, project: string, completingPrevious: boolean) => void} params.onSelect
 * @param {string} [params.prefillTask] pre-typed task name, selected so it can be replaced by typing
 */
export function openTaskSelection({store, completingPrevious, onSelect, prefillTask = ''}) {
    const dialog = new ModalDialog.ModalDialog({styleClass: 'focuson-dialog'});
    const content = boxLayout(true, {style_class: 'focuson-dialog-content'});
    dialog.contentLayout.add_child(content);

    content.add_child(heading('Select task'));

    const recentBox = boxLayout(true, {style_class: 'focuson-recent-list'});
    const recentScroll = new St.ScrollView({
        style_class: 'focuson-recent-scroll',
        hscrollbar_policy: St.PolicyType.NEVER,
        vscrollbar_policy: St.PolicyType.AUTOMATIC,
        y_expand: true,
    });
    setScrollChild(recentScroll, recentBox);

    const entry = new St.Entry({
        style_class: 'focuson-entry',
        hint_text: 'New task…',
        text: prefillTask,
        can_focus: true,
        x_expand: true,
    });
    if (prefillTask !== '') {
        // Selected, not just present: after "Actually, I…" the overwhelmingly
        // likely next move is Enter to resume what you were supposed to be
        // doing, but typing over it has to stay just as cheap.
        entry.clutter_text.set_selection(0, prefillTask.length);
    }

    const chooser = new ProjectChooser(
        store.availableProjects,
        store.currentProjectSlug,
        () => refreshRecent());

    const accept = name => {
        const trimmed = name.trim();
        if (trimmed === '')
            return;
        dialog.close(global.get_current_time());
        onSelect(trimmed, chooser.selected, completingPrevious);
    };

    function refreshRecent() {
        recentBox.destroy_all_children();
        const recent = store.recentTasks(chooser.selected);

        if (recent.length === 0) {
            recentBox.add_child(new St.Label({
                style_class: 'focuson-empty',
                text: 'No recent tasks',
            }));
            return;
        }

        for (const task of recent) {
            const rowContent = boxLayout(true, {style_class: 'focuson-recent-row-content'});
            rowContent.add_child(new St.Label({
                style_class: 'focuson-recent-name',
                text: task.name,
            }));
            rowContent.add_child(new St.Label({
                style_class: 'focuson-recent-when',
                text: formatRelative(task.startedAt),
            }));

            const button = new St.Button({
                style_class: 'focuson-recent-row',
                can_focus: true,
                x_expand: true,
                child: rowContent,
            });
            button.connect('clicked', () => accept(task.name));
            recentBox.add_child(button);
        }
    }

    const projectRow = boxLayout(false, {style_class: 'focuson-field-row'});
    projectRow.add_child(fieldLabel('Project'));
    projectRow.add_child(chooser.actor);
    content.add_child(projectRow);

    content.add_child(recentScroll);
    content.add_child(entry);

    entry.clutter_text.connect('activate', () => accept(entry.get_text()));
    refreshRecent();

    dialog.setButtons([
        {
            label: 'Cancel',
            key: Clutter.KEY_Escape,
            action: () => dialog.close(global.get_current_time()),
        },
        {
            label: 'Start',
            // js/ui/dialog.js reads buttonInfo['default'] — spelt any other
            // way, Enter does nothing.
            default: true,
            action: () => accept(entry.get_text()),
        },
    ]);

    // Before open(), not after: pushModal() reads _initialKeyFocus as it
    // opens, and a default button claims the focus otherwise — which would
    // leave this dialog's whole point, typing a task name, one Tab away.
    dialog.setInitialKeyFocus(entry.clutter_text);
    dialog.open(global.get_current_time());
    return dialog;
}

/**
 * Port of LogPastSessionView. For time you forgot to start the timer for —
 * writes one fully-formed row and never touches the live task.
 *
 * @param {object} params
 * @param {import('./taskStore.js').TaskStore} params.store
 * @param {(project: string, task: string, from: number, to: number, completed: boolean) => void} params.onSave
 */
export function openLogPastSession({store, onSave}) {
    const dialog = new ModalDialog.ModalDialog({styleClass: 'focuson-dialog'});
    const content = boxLayout(true, {style_class: 'focuson-dialog-content'});
    dialog.contentLayout.add_child(content);

    content.add_child(heading('Log past session'));

    const chooser = new ProjectChooser(
        store.availableProjects,
        store.currentProjectSlug,
        () => {});

    const projectRow = boxLayout(false, {style_class: 'focuson-field-row'});
    projectRow.add_child(fieldLabel('Project'));
    projectRow.add_child(chooser.actor);
    content.add_child(projectRow);

    const taskEntry = new St.Entry({
        style_class: 'focuson-entry',
        hint_text: 'Task…',
        can_focus: true,
        x_expand: true,
    });
    content.add_child(taskEntry);

    // Same defaults as the SwiftUI original: the hour you just finished.
    const nowSeconds = Math.floor(Date.now() / 1000);
    const fromEntry = new St.Entry({
        style_class: 'focuson-entry',
        text: formatLocalDateTime(nowSeconds - 3600),
        can_focus: true,
        x_expand: true,
    });
    const toEntry = new St.Entry({
        style_class: 'focuson-entry',
        text: formatLocalDateTime(nowSeconds),
        can_focus: true,
        x_expand: true,
    });

    for (const [label, widget] of [['From', fromEntry], ['To', toEntry]]) {
        const row = boxLayout(false, {style_class: 'focuson-field-row'});
        row.add_child(fieldLabel(label));
        row.add_child(widget);
        content.add_child(row);
    }

    content.add_child(new St.Label({
        style_class: 'focuson-hint',
        text: 'Times are local, as YYYY-MM-DD HH:MM',
    }));

    const check = new CheckRow('Completed', true);
    content.add_child(check.actor);

    const error = new St.Label({style_class: 'focuson-error', text: ''});
    content.add_child(error);

    const save = () => {
        const task = taskEntry.get_text().trim();
        const from = parseLocalDateTime(fromEntry.get_text());
        const to = parseLocalDateTime(toEntry.get_text());

        // Validated on save rather than live: an St.Entry has no
        // formatter, so a half-typed timestamp is invalid for most of the
        // time it is being edited and complaining about it is just noise.
        if (task === '') {
            error.text = 'Give the session a task name';
            return;
        }
        if (from === null || to === null) {
            error.text = 'Times must read YYYY-MM-DD HH:MM';
            return;
        }
        if (to <= from) {
            error.text = 'To must be after From';
            return;
        }

        dialog.close(global.get_current_time());
        onSave(chooser.selected, task, from, to, check.checked);
    };

    taskEntry.clutter_text.connect('activate', save);
    toEntry.clutter_text.connect('activate', save);

    dialog.setButtons([
        {
            label: 'Cancel',
            key: Clutter.KEY_Escape,
            action: () => dialog.close(global.get_current_time()),
        },
        {
            label: 'Save',
            // js/ui/dialog.js reads buttonInfo['default'] — spelt any other
            // way, Enter does nothing.
            default: true,
            action: save,
        },
    ]);

    dialog.setInitialKeyFocus(taskEntry.clutter_text);
    dialog.open(global.get_current_time());
    return dialog;
}


/**
 * "Actually, I…" — re-attribute the running session to what you were really
 * doing, and close it out.
 *
 * The gap this fills: a timer says "Writing" and has said so for forty
 * minutes, but the forty minutes went on something else. Pausing loses the
 * distinction and completing files it as writing you never did. This asks
 * what actually happened and bills the time to that instead.
 *
 * Deliberately not a project picker. The closing row must land in the same
 * task_log.csv as the row it closes, or the original project keeps an
 * unclosed session that the CLI reports as abandoned — so the project is
 * shown, not offered. See TaskStore.closeCurrentTaskAs.
 *
 * @param {object} params
 * @param {import('./taskStore.js').TaskStore} params.store
 * @param {(actualTask: string, completed: boolean) => void} params.onConfirm
 */
export function openActuallyI({store, onConfirm}) {
    const nominalTask = store.currentTaskName;
    const elapsed = formatElapsed(Math.floor(Date.now() / 1000) - store.currentTaskStartedAt);

    const dialog = new ModalDialog.ModalDialog({styleClass: 'focuson-dialog'});
    const content = boxLayout(true, {style_class: 'focuson-dialog-content'});
    dialog.contentLayout.add_child(content);

    content.add_child(heading('Actually, I…'));

    content.add_child(new St.Label({
        style_class: 'focuson-hint',
        text: `The last ${elapsed} is logged to “${nominalTask}” in ${store.currentProjectSlug}. ` +
            'Say what it really went on and it moves there instead.',
    }));

    const recentBox = boxLayout(true, {style_class: 'focuson-recent-list'});
    const recentScroll = new St.ScrollView({
        style_class: 'focuson-recent-scroll',
        hscrollbar_policy: St.PolicyType.NEVER,
        vscrollbar_policy: St.PolicyType.AUTOMATIC,
        y_expand: true,
    });
    setScrollChild(recentScroll, recentBox);

    const entry = new St.Entry({
        style_class: 'focuson-entry',
        hint_text: 'What you actually did…',
        can_focus: true,
        x_expand: true,
    });

    const check = new CheckRow('Finished with it', true);

    const confirm = name => {
        const trimmed = name.trim();
        if (trimmed === '')
            return;
        dialog.close(global.get_current_time());
        onConfirm(trimmed, check.checked);
    };

    // Procrastination repeats, so the recent list is usually where the answer
    // already is. Scoped to the session's own project, since that is the only
    // file the closing row can go to.
    const recent = store.recentTasks(store.currentProjectSlug)
        .filter(task => task.name !== nominalTask);

    if (recent.length === 0) {
        recentBox.add_child(new St.Label({
            style_class: 'focuson-empty',
            text: 'No other recent tasks',
        }));
    } else {
        for (const task of recent) {
            const rowContent = boxLayout(true, {style_class: 'focuson-recent-row-content'});
            rowContent.add_child(new St.Label({
                style_class: 'focuson-recent-name',
                text: task.name,
            }));
            rowContent.add_child(new St.Label({
                style_class: 'focuson-recent-when',
                text: formatRelative(task.startedAt),
            }));

            const button = new St.Button({
                style_class: 'focuson-recent-row',
                can_focus: true,
                x_expand: true,
                child: rowContent,
            });
            button.connect('clicked', () => confirm(task.name));
            recentBox.add_child(button);
        }
    }

    content.add_child(recentScroll);
    content.add_child(entry);
    content.add_child(check.actor);

    entry.clutter_text.connect('activate', () => confirm(entry.get_text()));

    dialog.setButtons([
        {
            label: 'Cancel',
            key: Clutter.KEY_Escape,
            action: () => dialog.close(global.get_current_time()),
        },
        {
            label: 'Move the time',
            default: true,
            action: () => confirm(entry.get_text()),
        },
    ]);

    dialog.setInitialKeyFocus(entry.clutter_text);
    dialog.open(global.get_current_time());
    return dialog;
}
