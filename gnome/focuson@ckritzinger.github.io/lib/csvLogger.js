// Port of FocusOn/CSVLogger.swift.
//
// Everything here exists to write files the Go CLI can read unchanged: the
// same config.toml, the same projects/<slug>/task_log.csv, the same row
// format. That constraint, not GNOME convention, decides the shape of this
// file — a data directory written by the GNOME widget has to be invoiceable
// by a `focuson` built from the same repo, and a directory a macOS machine
// synced over git has to keep working here.

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';

import {formatRow, parseRow} from './format.js';

const decoder = new TextDecoder('utf-8');
const encoder = new TextEncoder();

/**
 * The Go CLI's own config file. internal/config resolves this exact path via
 * os.UserConfigDir(), which on Linux is $XDG_CONFIG_HOME (or ~/.config) —
 * the same thing GLib.get_user_config_dir() returns. The widget and the CLI
 * deliberately share this one file rather than each keeping their own copy
 * of "which directory": one file, read/written by both sides, means there's
 * no separate copy to fall out of sync in the first place.
 */
export function cliConfigPath() {
    return GLib.build_filenamev([GLib.get_user_config_dir(), 'focuson', 'config.toml']);
}

function readTextFile(path) {
    const file = Gio.File.new_for_path(path);
    try {
        const [ok, contents] = file.load_contents(null);
        if (!ok)
            return null;
        return decoder.decode(contents);
    } catch {
        // Missing file is the normal first-run state, not an error worth
        // propagating — every caller here treats "no file" and "unreadable"
        // the same way.
        return null;
    }
}

/**
 * Reads `data_dir = "..."` out of the CLI's config.toml. Deliberately not a
 * general TOML parser (the project picker follows the same "zero TOML
 * parsing" rule) — just enough to find one flat key, scanning line by line
 * so the CLI can add other keys around it later without breaking this.
 */
function readCLIConfigDataDir() {
    const contents = readTextFile(cliConfigPath());
    if (contents === null)
        return null;

    for (const line of contents.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed.startsWith('data_dir'))
            continue;
        const eq = trimmed.indexOf('=');
        if (eq < 0)
            continue;
        let value = trimmed.slice(eq + 1).trim();
        if (value.length >= 2 && value.startsWith('"') && value.endsWith('"')) {
            value = value.slice(1, -1)
                .replace(/\\"/g, '"')
                .replace(/\\\\/g, '\\');
        }
        return value;
    }
    return null;
}

export function dataDirectory() {
    const stored = readCLIConfigDataDir();
    if (stored)
        return stored;
    return GLib.build_filenamev([GLib.get_home_dir(), 'focuson-data']);
}

/** ~-abbreviated for display, matching the macOS popover's data-directory row. */
export function dataDirectoryDisplayPath() {
    const path = dataDirectory();
    const home = GLib.get_home_dir();
    return path.startsWith(home) ? `~${path.slice(home.length)}` : path;
}

function writeCLIConfig(dataDir) {
    const escaped = dataDir.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
    const path = cliConfigPath();
    const file = Gio.File.new_for_path(path);
    try {
        file.get_parent().make_directory_with_parents(null);
    } catch {
        // Already there — make_directory_with_parents throws EXISTS rather
        // than succeeding quietly.
    }
    file.replace_contents(
        encoder.encode(`data_dir = "${escaped}"\n`),
        null, false, Gio.FileCreateFlags.REPLACE_DESTINATION, null);
}

export function setDataDirectory(path) {
    writeCLIConfig(path);
}

function projectsDirectory() {
    return GLib.build_filenamev([dataDirectory(), 'projects']);
}

/**
 * The widget's only source of truth for "which projects exist" — it never
 * parses manifest.toml (that's the CLI's file). See spec_v2.md, Widget
 * Changes → "Project picker".
 */
export function listProjectSlugs() {
    const dir = Gio.File.new_for_path(projectsDirectory());
    let enumerator;
    try {
        enumerator = dir.enumerate_children(
            'standard::name,standard::type',
            Gio.FileQueryInfoFlags.NONE, null);
    } catch {
        return [];
    }

    const slugs = [];
    let info;
    while ((info = enumerator.next_file(null)) !== null) {
        if (info.get_file_type() !== Gio.FileType.DIRECTORY)
            continue;
        const name = info.get_name();
        if (name.startsWith('.'))
            continue;
        slugs.push(name);
    }
    enumerator.close(null);
    return slugs.sort();
}

/**
 * Creates the data directory and, if nothing is there yet, a minimal
 * manifest.toml (just the "personal" project) plus projects/personal/.
 * Mirrors the CLI's own bootstrap (internal/manifest.Bootstrap) so either
 * side can be first to touch a brand-new data directory. Never overwrites an
 * existing manifest — the widget never writes manifest.toml otherwise.
 */
export function bootstrapDataDirectoryIfNeeded() {
    // If neither side has ever set a directory, dataDirectory() is just an
    // implicit fallback (~/focuson-data), not yet a real, recorded choice.
    // Persist it explicitly so a CLI run afterward sees the same path
    // instead of running its own first-run setup and potentially landing
    // somewhere else.
    if (!GLib.file_test(cliConfigPath(), GLib.FileTest.EXISTS))
        writeCLIConfig(dataDirectory());

    makeDirectory(dataDirectory());

    const manifestPath = GLib.build_filenamev([dataDirectory(), 'manifest.toml']);
    if (!GLib.file_test(manifestPath, GLib.FileTest.EXISTS)) {
        const minimalManifest = [
            '[business]',
            'name = ""',
            'address = ""',
            'email = ""',
            'payment_details = ""',
            '',
            '[[project]]',
            'slug = "personal"',
            'client = ""',
            'name = "Personal"',
            '',
        ].join('\n');
        Gio.File.new_for_path(manifestPath).replace_contents(
            encoder.encode(minimalManifest),
            null, false, Gio.FileCreateFlags.REPLACE_DESTINATION, null);
    }

    makeDirectory(GLib.build_filenamev([projectsDirectory(), 'personal']));
}

function makeDirectory(path) {
    try {
        Gio.File.new_for_path(path).make_directory_with_parents(null);
    } catch {
        // Already exists.
    }
}

// MARK: - Per-project task_log.csv

export function taskLogPath(project) {
    return GLib.build_filenamev([projectsDirectory(), project, 'task_log.csv']);
}

function createFileIfNeeded(project) {
    const path = taskLogPath(project);
    if (GLib.file_test(path, GLib.FileTest.EXISTS))
        return;
    const file = Gio.File.new_for_path(path);
    makeDirectory(file.get_parent().get_path());
    file.replace_contents(
        encoder.encode('uuid,task,from,to,completed\n'),
        null, false, Gio.FileCreateFlags.REPLACE_DESTINATION, null);
}

/** A lowercase RFC 4122 UUID, matching what Swift's UUID().uuidString.lowercased() wrote. */
export function newUUID() {
    return GLib.uuid_string_random().toLowerCase();
}

/**
 * Appends one row. Append-only is the whole contract of this file: sessions
 * are never edited or deleted, and a completed session is two rows sharing
 * one uuid (an open marker written at the start, a close row at the end).
 *
 * @param {object} row
 * @param {string} row.project project slug
 * @param {string} row.uuid session uuid
 * @param {string} row.task free-text task name
 * @param {number} row.from unix seconds
 * @param {?number} row.to unix seconds, or null while the session is open
 * @param {?boolean} row.completed null while the session is open
 */
export function appendRow({project, uuid, task, from, to, completed}) {
    createFileIfNeeded(project);

    const line = formatRow({uuid, task, from, to, completed});

    const file = Gio.File.new_for_path(taskLogPath(project));
    const stream = file.append_to(Gio.FileCreateFlags.NONE, null);
    try {
        stream.write_all(encoder.encode(line), null);
    } finally {
        stream.close(null);
    }
}

/**
 * @typedef {object} TaskRow
 * @property {string} uuid
 * @property {string} task
 * @property {number} from unix seconds
 * @property {?number} to unix seconds, or null
 * @property {?boolean} completed
 */

/** @returns {TaskRow[]} every parseable row; unparseable ones are skipped. */
export function readAllRows(project) {
    const content = readTextFile(taskLogPath(project));
    if (content === null)
        return [];

    const lines = content.split('\n');
    if (lines.length <= 1)
        return [];
    lines.shift(); // header

    const rows = [];
    for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed === '')
            continue;
        const row = parseRow(trimmed);
        if (row !== null)
            rows.push(row);
    }
    return rows;
}
