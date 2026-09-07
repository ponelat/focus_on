// Port of FocusOn/FloatingPanel.swift + FocusOn/WidgetView.swift.
//
// The macOS original is a borderless NSPanel at .floating level with
// canJoinAllSpaces + fullScreenAuxiliary — a window that outranks every other
// window and follows you between Spaces. There is no equivalent for a
// *client* on GNOME under Wayland: a Wayland client cannot place its own
// surface, cannot raise itself above others, and Mutter does not implement
// wlr-layer-shell, so the usual "panel/overlay" escape hatch is closed too.
//
// The one place on GNOME that can do all of it is the compositor itself, and
// that's why this port is a Shell extension rather than a GTK app. Here the
// widget is a Clutter actor in the Shell's own chrome, which gets the whole
// list of NSPanel behaviours for free: it draws above every window including
// fullscreen ones, it is visible on every workspace, and it can be dragged
// to an exact pixel because the compositor is doing the positioning.

import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import {formatElapsed} from './format.js';

/** Matches the macOS default panel inset from the screen corner. */
const SCREEN_MARGIN = 20;

/**
 * Pointer movement, in pixels, that turns a click into a drag. WidgetContainerView
 * used "did any drag event arrive at all"; a touchpad makes that far too
 * eager, so this port asks for a real intent to move before it stops
 * counting the gesture as a tap.
 */
const DRAG_THRESHOLD = 8;

const ACTIVE_DOT_OPACITY = 255;
const PULSE_DOT_OPACITY = 102; // 0.4 alpha, as in WidgetView's pulse

export class FloatingWidget {
    /**
     * @param {object} params
     * @param {import('./taskStore.js').TaskStore} params.store
     * @param {Gio.Settings} params.settings
     * @param {() => void} params.onTap
     */
    constructor({store, settings, onTap}) {
        this._store = store;
        this._settings = settings;
        this._onTap = onTap;

        this._grab = null;
        this._dragging = false;
        this._pressCoords = null;
        this._dragOffset = [0, 0];
        this._lastWidth = 0;
        this._tickId = 0;
        this._pulseActive = null;

        this._buildActor();

        Main.layoutManager.addChrome(this.actor, {
            // No struts: this is a floating widget, not a panel, and windows
            // should tile straight underneath it.
            affectsStruts: false,
            // The NSPanel's .fullScreenAuxiliary. LayoutManager hides
            // fullscreen-tracked chrome when a window goes fullscreen, which
            // is exactly when you most want to know what you were supposed
            // to be doing — so this one opts out.
            trackFullscreen: false,
        });

        this._monitorsChangedId = Main.layoutManager.connect(
            'monitors-changed', () => this._restorePosition());

        // The preferences window's "Reset to corner" button writes the -1
        // sentinel. Reacting only to that value is what keeps this from
        // fighting the widget's own drag writes.
        this._positionResetId = this._settings.connect('changed::widget-x', () => {
            if (this._settings.get_int('widget-x') < 0)
                this._applyDefaultPosition();
        });

        // Size isn't known until the first allocation, and both the default
        // corner and the saved-position clamp need it.
        this._allocationId = this.actor.connect('notify::allocation',
            () => this._onAllocationChanged());

        this._unsubscribe = this._store.connect(() => this.refresh());
        this.refresh();
    }

    _buildActor() {
        this.actor = new St.BoxLayout({
            style_class: 'focuson-widget',
            reactive: true,
            track_hover: true,
            can_focus: false,
        });

        this._dot = new St.Widget({
            style_class: 'focuson-dot',
            y_align: Clutter.ActorAlign.CENTER,
        });
        this.actor.add_child(this._dot);

        this._taskLabel = new St.Label({
            style_class: 'focuson-task-label',
            y_align: Clutter.ActorAlign.CENTER,
        });
        this.actor.add_child(this._taskLabel);

        this._elapsedLabel = new St.Label({
            style_class: 'focuson-elapsed-label',
            y_align: Clutter.ActorAlign.CENTER,
        });
        this.actor.add_child(this._elapsedLabel);

        this._chevron = new St.Icon({
            style_class: 'focuson-chevron',
            icon_name: 'pan-down-symbolic',
            y_align: Clutter.ActorAlign.CENTER,
        });
        this.actor.add_child(this._chevron);

        this.actor.connect('button-press-event', (a, event) => this._onPress(event));
        this.actor.connect('motion-event', (a, event) => this._onMotion(event));
        this.actor.connect('button-release-event', () => this._onRelease());
    }

    // MARK: - Drag and tap
    //
    // Ported from WidgetContainerView, which intercepted every mouse event at
    // the NSView level so the SwiftUI subtree never saw one. The same idea
    // here needs a Clutter grab rather than a hit-test override: without one,
    // motion events stop arriving the moment the pointer outruns the actor,
    // and a quick drag drops the widget halfway.

    _onPress(event) {
        if (event.get_button() !== Clutter.BUTTON_PRIMARY)
            return Clutter.EVENT_PROPAGATE;

        const [x, y] = event.get_coords();
        this._pressCoords = [x, y];
        this._dragOffset = [x - this.actor.x, y - this.actor.y];
        this._dragging = false;
        this._grab = global.stage.grab(this.actor);
        return Clutter.EVENT_STOP;
    }

    _onMotion(event) {
        if (this._grab === null)
            return Clutter.EVENT_PROPAGATE;

        const [x, y] = event.get_coords();
        if (!this._dragging) {
            const [px, py] = this._pressCoords;
            if (Math.abs(x - px) < DRAG_THRESHOLD && Math.abs(y - py) < DRAG_THRESHOLD)
                return Clutter.EVENT_STOP;
            this._dragging = true;
        }

        this._moveTo(x - this._dragOffset[0], y - this._dragOffset[1]);
        return Clutter.EVENT_STOP;
    }

    _onRelease() {
        if (this._grab === null)
            return Clutter.EVENT_PROPAGATE;

        this._releaseGrab();

        if (this._dragging)
            this._savePosition();
        else
            this._onTap();

        this._dragging = false;
        return Clutter.EVENT_STOP;
    }

    _releaseGrab() {
        if (this._grab === null)
            return;
        this._grab.dismiss();
        this._grab = null;
    }

    // MARK: - Position
    //
    // Port of FloatingPanel's restorePosition/savePosition/clampToScreen,
    // with one coordinate-system change: AppKit measures from the bottom-left
    // of the screen, Clutter from the top-left of the stage, so "20px up from
    // the bottom edge" is written as workArea.y + workArea.height - height -
    // margin rather than visibleFrame.minY + margin.

    _onAllocationChanged() {
        const width = this.actor.width;
        if (width === this._lastWidth)
            return;

        if (this._lastWidth === 0) {
            this._lastWidth = width;
            this._restorePosition();
            return;
        }

        // Anchor the right edge so the widget doesn't jump sideways as the
        // task name changes length — same reason resizePanel() did it.
        const right = this.actor.x + this._lastWidth;
        this._lastWidth = width;
        this._moveTo(right - width, this.actor.y);
        this._savePosition();
    }

    _restorePosition() {
        const x = this._settings.get_int('widget-x');
        const y = this._settings.get_int('widget-y');
        if (x < 0 && y < 0)
            this._applyDefaultPosition();
        else
            this._moveTo(x, y);
    }

    _applyDefaultPosition() {
        const area = this._workArea();
        this._moveTo(
            area.x + area.width - this.actor.width - SCREEN_MARGIN,
            area.y + area.height - this.actor.height - SCREEN_MARGIN);
    }

    /**
     * The work area of whichever monitor the widget is mostly on, falling
     * back to the primary. Using the *work* area rather than the monitor
     * rectangle is what keeps the widget clear of the top bar and any dock.
     */
    _workArea() {
        const monitors = Main.layoutManager.monitors;
        const primary = Main.layoutManager.primaryIndex;
        if (monitors.length === 0)
            return {x: 0, y: 0, width: 1920, height: 1080};

        const cx = this.actor.x + this.actor.width / 2;
        const cy = this.actor.y + this.actor.height / 2;
        for (let i = 0; i < monitors.length; i++) {
            const m = monitors[i];
            if (cx >= m.x && cx < m.x + m.width && cy >= m.y && cy < m.y + m.height)
                return Main.layoutManager.getWorkAreaForMonitor(i);
        }
        return Main.layoutManager.getWorkAreaForMonitor(primary);
    }

    /** Clamps into the work area, so an unplugged monitor can't strand the widget offscreen. */
    _moveTo(x, y) {
        const area = this._workArea();
        const maxX = area.x + area.width - this.actor.width;
        const maxY = area.y + area.height - this.actor.height;
        this.actor.set_position(
            Math.round(Math.max(area.x, Math.min(x, maxX))),
            Math.round(Math.max(area.y, Math.min(y, maxY))));
    }

    _savePosition() {
        this._settings.set_int('widget-x', this.actor.x);
        this._settings.set_int('widget-y', this.actor.y);
    }

    // MARK: - Rendering

    refresh() {
        const tracking = this._store.isTracking;

        this._taskLabel.text = tracking ? this._store.currentTaskName : 'No active task';
        this._taskLabel.remove_style_class_name('focuson-task-label-idle');
        if (!tracking)
            this._taskLabel.add_style_class_name('focuson-task-label-idle');

        this._elapsedLabel.visible = tracking;
        this._updateElapsed();
        this._updatePulse(tracking);
        this._updateTicker(tracking);
    }

    _updateTicker(tracking) {
        if (tracking && this._tickId === 0) {
            this._tickId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
                this._updateElapsed();
                return GLib.SOURCE_CONTINUE;
            });
        } else if (!tracking && this._tickId !== 0) {
            GLib.Source.remove(this._tickId);
            this._tickId = 0;
        }
    }

    _updateElapsed() {
        if (!this._store.isTracking)
            return;
        const elapsed = Math.max(0, Math.floor(Date.now() / 1000) - this._store.currentTaskStartedAt);
        this._elapsedLabel.text = formatElapsed(elapsed);
    }

    _updatePulse(tracking) {
        // The store emits a change whenever the project list is re-read,
        // which happens every time a menu opens. Restarting the animation
        // then would make the dot visibly stutter, so only a real change of
        // tracking state touches it.
        if (tracking === this._pulseActive)
            return;
        this._pulseActive = tracking;

        this._dot.remove_all_transitions();

        if (!tracking) {
            this._dot.remove_style_class_name('focuson-dot-active');
            this._dot.opacity = ACTIVE_DOT_OPACITY;
            return;
        }

        this._dot.add_style_class_name('focuson-dot-active');
        this._dot.opacity = ACTIVE_DOT_OPACITY;
        this._dot.ease({
            opacity: PULSE_DOT_OPACITY,
            duration: 1000,
            mode: Clutter.AnimationMode.EASE_IN_OUT_QUAD,
            autoReverse: true,
            repeatCount: -1,
        });
    }

    show() {
        this.actor.show();
    }

    hide() {
        this.actor.hide();
    }

    destroy() {
        this._releaseGrab();

        if (this._tickId !== 0) {
            GLib.Source.remove(this._tickId);
            this._tickId = 0;
        }
        if (this._monitorsChangedId) {
            Main.layoutManager.disconnect(this._monitorsChangedId);
            this._monitorsChangedId = 0;
        }
        if (this._positionResetId) {
            this._settings.disconnect(this._positionResetId);
            this._positionResetId = 0;
        }
        if (this._unsubscribe) {
            this._unsubscribe();
            this._unsubscribe = null;
        }

        this._dot.remove_all_transitions();
        Main.layoutManager.removeChrome(this.actor);
        this.actor.destroy();
        this.actor = null;
    }
}
