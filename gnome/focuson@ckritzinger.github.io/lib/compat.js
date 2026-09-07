// Small shims over the St API changes between GNOME 45 and 49.
//
// The extension targets that whole range because that's what the distros a
// NixOS user is likely to be on actually ship (nixos-24.05 through unstable),
// and two St APIs moved inside it. Both changes are additive-then-deprecating
// rather than breaking, so feature detection — not a shell-version check —
// is what keeps one code path working everywhere.

import Clutter from 'gi://Clutter';
import St from 'gi://St';

/**
 * St.BoxLayout gained an `orientation` property in GNOME 48 and deprecated
 * `vertical`. Both work on 48/49; only `vertical` works on 45–47.
 *
 * @param {boolean} vertical
 * @param {object} params passed straight to the St.BoxLayout constructor
 */
export function boxLayout(vertical, params = {}) {
    const box = new St.BoxLayout(params);
    if ('orientation' in box) {
        box.orientation = vertical
            ? Clutter.Orientation.VERTICAL
            : Clutter.Orientation.HORIZONTAL;
    } else {
        box.vertical = vertical;
    }
    return box;
}

/**
 * St.ScrollView stopped being an St.Bin in GNOME 46; `add_actor` was the way
 * to fill one before that, `set_child` after. St.Bin has always had
 * set_child, so it is the branch that covers the most versions — but 45's
 * StScrollView is reached through the StBin inheritance, which is exactly
 * the thing that went away, so the fallback has to stay.
 */
export function setScrollChild(scrollView, child) {
    if (typeof scrollView.set_child === 'function')
        scrollView.set_child(child);
    else
        scrollView.add_actor(child);
}
