// Run with: node --test gnome/tests/
//
// Everything under test here is the part of the GNOME port that has to agree
// with the Go CLI byte for byte. lib/format.js is deliberately free of
// `gi://` imports so this can run anywhere, which matters: the rest of the
// extension can only be exercised inside a live GNOME session.

import test from 'node:test';
import assert from 'node:assert/strict';

import {
    formatElapsed,
    formatISO8601,
    formatLocalDateTime,
    formatRelative,
    formatRow,
    parseCSVLine,
    parseISO8601,
    parseLocalDateTime,
    parseRow,
} from '../focuson@ckritzinger.github.io/lib/format.js';

test('formatISO8601 writes RFC 3339 with a colon in the offset', () => {
    // Go's time.RFC3339 accepts "Z" or "±hh:mm" and nothing else — notably
    // not GLib's minimal "+02" form, which is why this is hand-rolled.
    const out = formatISO8601(1757246096);
    assert.match(out, /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$/);
});

test('formatISO8601 keeps local wall-clock time, not UTC', () => {
    const seconds = 1757246096;
    const local = new Date(seconds * 1000);
    const out = formatISO8601(seconds);
    assert.equal(Number(out.slice(11, 13)), local.getHours());
    assert.equal(Number(out.slice(14, 16)), local.getMinutes());
});

test('ISO 8601 round-trips through parse', () => {
    for (const seconds of [0, 1, 1757246096, 2000000000]) {
        assert.equal(parseISO8601(formatISO8601(seconds)), seconds);
    }
});

test('parseISO8601 reads timestamps written elsewhere', () => {
    // A row written by the macOS widget, and one written in UTC by anything
    // that syncs the data directory over git.
    // Both spellings are the same instant, which is the point: a data
    // directory synced from another machine must read back identically.
    assert.equal(parseISO8601('2026-09-07T14:34:56+02:00'), 1788784496);
    assert.equal(parseISO8601('2026-09-07T12:34:56Z'), 1788784496);
    assert.equal(parseISO8601('nonsense'), null);
});

test('formatRow matches the documented schema', () => {
    const line = formatRow({
        uuid: 'e1e2c0de-0000-4000-8000-000000000001',
        task: 'Write the port',
        from: 1757246096,
        to: null,
        completed: null,
    });
    const fields = parseCSVLine(line.trimEnd());
    assert.equal(fields.length, 5);
    assert.equal(fields[0], 'e1e2c0de-0000-4000-8000-000000000001');
    assert.equal(fields[1], 'Write the port');
    assert.equal(fields[3], '', 'an open session has no end time');
    assert.equal(fields[4], '', 'an open session has no completed flag');
    assert.ok(line.endsWith('\n'));
});

test('formatRow writes completed as true/false, never empty, once closed', () => {
    const closed = parseCSVLine(formatRow({
        uuid: 'u', task: 't', from: 1, to: 2, completed: false,
    }).trimEnd());
    assert.equal(closed[4], 'false');

    const done = parseCSVLine(formatRow({
        uuid: 'u', task: 't', from: 1, to: 2, completed: true,
    }).trimEnd());
    assert.equal(done[4], 'true');
});

test('a task name containing a comma and quotes survives a round trip', () => {
    // encoding/csv on the Go side will reject or mis-split anything that
    // doesn't double its quotes, and task names are free text.
    const task = 'Fix "the, thing" — properly';
    const line = formatRow({uuid: 'u', task, from: 1757246096, to: 1757249696, completed: true});
    const row = parseRow(line.trimEnd());
    assert.equal(row.task, task);
    assert.equal(row.completed, true);
    assert.equal(row.to, 1757249696);
});

test('parseRow rejects a row with too few fields or a bad timestamp', () => {
    assert.equal(parseRow('u,"t",2026-09-07T12:00:00+02:00'), null);
    assert.equal(parseRow('u,"t",not-a-time,,'), null);
});

test('parseRow reads an open row as to=null, completed=null', () => {
    const row = parseRow('u,"t",2026-09-07T12:00:00+02:00,,');
    assert.equal(row.to, null);
    assert.equal(row.completed, null);
});

test('formatElapsed switches to hours past the hour mark', () => {
    assert.equal(formatElapsed(0), '0:00');
    assert.equal(formatElapsed(59), '0:59');
    assert.equal(formatElapsed(61), '1:01');
    assert.equal(formatElapsed(3599), '59:59');
    assert.equal(formatElapsed(3600), '1:00:00');
    assert.equal(formatElapsed(45296), '12:34:56');
    assert.equal(formatElapsed(-5), '0:00', 'a clock jump must not print a negative timer');
});

test('formatRelative coarsens the way the recent-task list needs', () => {
    const now = 1757246096;
    assert.equal(formatRelative(now, now), 'just now');
    assert.equal(formatRelative(now - 300, now), '5 min ago');
    assert.equal(formatRelative(now - 3600, now), '1 hr ago');
    assert.equal(formatRelative(now - 7200, now), '2 hr ago');
    assert.equal(formatRelative(now - 86400, now), 'yesterday');
    assert.equal(formatRelative(now - 3 * 86400, now), '3 days ago');
});

test('local date-time round-trips through the Log past session fields', () => {
    const seconds = Math.floor(new Date(2026, 8, 7, 14, 30, 0, 0).getTime() / 1000);
    assert.equal(formatLocalDateTime(seconds), '2026-09-07 14:30');
    assert.equal(parseLocalDateTime('2026-09-07 14:30'), seconds);
});

test('parseLocalDateTime rejects a date that does not exist', () => {
    // Date would roll this forward to March 3rd and log the work on the
    // wrong day rather than complaining.
    assert.equal(parseLocalDateTime('2026-02-31 09:00'), null);
    assert.equal(parseLocalDateTime('2026-09-07 25:00'), null);
    assert.equal(parseLocalDateTime('7 Sep 2026'), null);
    assert.equal(parseLocalDateTime(''), null);
});

test('parseLocalDateTime tolerates the ISO T separator and surrounding space', () => {
    const seconds = Math.floor(new Date(2026, 8, 7, 14, 30, 0, 0).getTime() / 1000);
    assert.equal(parseLocalDateTime('  2026-09-07T14:30 '), seconds);
});
