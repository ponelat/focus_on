// The parts of the port that are pure computation: timestamp formatting, CSV
// row encoding/decoding, elapsed and relative time.
//
// Kept free of every `gi://` import on purpose. These are the functions whose
// output the Go CLI has to be able to read back, so they're the ones worth
// testing — and with no GNOME libraries in the way they run under plain
// `node --test` (see gnome/tests/), which is the only way to test any of this
// port without a GNOME session in front of you.

const pad = (n, width = 2) => String(Math.abs(n)).padStart(width, '0');

/**
 * RFC 3339 with a numeric local offset — the exact shape Swift's
 * ISO8601DateFormatter(.withInternetDateTime) wrote and Go's
 * time.Parse(time.RFC3339, …) reads.
 *
 * Written by hand rather than taken from GLib.DateTime.format_iso8601(),
 * which emits the minimal "+02" offset form that Go's RFC3339 parser
 * rejects, and rather than from Date.toISOString(), which normalises to UTC.
 * Local time with a real offset is the point: the log records someone's
 * working day, and an invoice line reading 03:00 for work done at 05:00
 * would be wrong in the way that matters.
 *
 * @param {number} unixSeconds
 * @returns {string}
 */
export function formatISO8601(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    // getTimezoneOffset is minutes *behind* UTC, so a positive value means a
    // negative offset. Inverting once here keeps the sign logic readable.
    const offsetMinutes = -d.getTimezoneOffset();
    const sign = offsetMinutes < 0 ? '-' : '+';
    const offset = `${sign}${pad(Math.trunc(Math.abs(offsetMinutes) / 60))}:${pad(Math.abs(offsetMinutes) % 60)}`;
    return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
        `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}${offset}`;
}

/**
 * @param {string} text an RFC 3339 timestamp
 * @returns {?number} unix seconds, or null if it isn't parseable
 */
export function parseISO8601(text) {
    const ms = Date.parse(text);
    return Number.isNaN(ms) ? null : Math.floor(ms / 1000);
}

/**
 * One task_log.csv line, newline included.
 *
 * The task is the only field ever quoted, matching what the macOS widget
 * wrote — every other field is machine-generated and cannot contain a comma
 * or a quote.
 *
 * @param {object} row
 * @param {string} row.uuid
 * @param {string} row.task
 * @param {number} row.from unix seconds
 * @param {?number} row.to unix seconds, or null while the session is open
 * @param {?boolean} row.completed null while the session is open
 * @returns {string}
 */
export function formatRow({uuid, task, from, to, completed}) {
    const escapedTask = String(task).replace(/"/g, '""');
    const toStr = to === null || to === undefined ? '' : formatISO8601(to);
    let completedStr = '';
    if (completed === true)
        completedStr = 'true';
    else if (completed === false)
        completedStr = 'false';
    return `${uuid},"${escapedTask}",${formatISO8601(from)},${toStr},${completedStr}\n`;
}

/**
 * RFC 4180 field splitting, matching CSVLogger.parseRow — a doubled quote
 * inside a quoted field is one literal quote.
 *
 * @param {string} line
 * @returns {string[]}
 */
export function parseCSVLine(line) {
    const fields = [];
    let current = '';
    let inQuotes = false;

    for (let i = 0; i < line.length; i++) {
        const c = line[i];
        if (inQuotes) {
            if (c === '"') {
                if (line[i + 1] === '"') {
                    current += '"';
                    i++;
                } else {
                    inQuotes = false;
                }
            } else {
                current += c;
            }
        } else if (c === '"') {
            inQuotes = true;
        } else if (c === ',') {
            fields.push(current);
            current = '';
        } else {
            current += c;
        }
    }
    fields.push(current);
    return fields;
}

/**
 * @param {string} line one data row of task_log.csv
 * @returns {?object} the row, or null if it can't be parsed
 */
export function parseRow(line) {
    const fields = parseCSVLine(line);
    if (fields.length < 5)
        return null;

    const from = parseISO8601(fields[2]);
    if (from === null)
        return null;

    return {
        uuid: fields[0],
        task: fields[1],
        from,
        to: fields[3] === '' ? null : parseISO8601(fields[3]),
        completed: fields[4] === '' ? null : fields[4] === 'true',
    };
}

/**
 * h:mm:ss past an hour, m:ss below it — the same thresholds WidgetView used.
 *
 * @param {number} totalSeconds
 * @returns {string}
 */
export function formatElapsed(totalSeconds) {
    const total = Math.max(0, Math.floor(totalSeconds));
    const h = Math.trunc(total / 3600);
    const m = Math.trunc((total % 3600) / 60);
    const s = total % 60;
    return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

/**
 * Stands in for RelativeDateTimeFormatter(.abbreviated), which the recent-task
 * list used to date each entry. Deliberately coarse: the list only needs to
 * answer "was this today or last week", and a plain string beats pulling in
 * the shell's translation machinery for six phrases.
 *
 * @param {number} unixSeconds
 * @param {number} [nowSeconds]
 * @returns {string}
 */
export function formatRelative(unixSeconds, nowSeconds = Math.floor(Date.now() / 1000)) {
    const delta = Math.max(0, nowSeconds - unixSeconds);
    if (delta < 60)
        return 'just now';
    if (delta < 3600) {
        const m = Math.trunc(delta / 60);
        return `${m} min ago`;
    }
    if (delta < 86400) {
        const h = Math.trunc(delta / 3600);
        return h === 1 ? '1 hr ago' : `${h} hr ago`;
    }
    const d = Math.trunc(delta / 86400);
    return d === 1 ? 'yesterday' : `${d} days ago`;
}

/**
 * "YYYY-MM-DD HH:MM" in local time — the display and input format for the
 * Log past session dialog. GNOME's Shell toolkit has no date picker
 * equivalent to SwiftUI's DatePicker, and a typed timestamp is both
 * unambiguous and faster than any widget that could be built out of St
 * buttons.
 *
 * @param {number} unixSeconds
 * @returns {string}
 */
export function formatLocalDateTime(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
        `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const LOCAL_DATE_TIME = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})$/;

/**
 * Parses what formatLocalDateTime writes, in local time.
 *
 * Rejects dates that don't exist (2026-02-31) by round-tripping through Date,
 * which would otherwise roll them forward silently and log an hour of work on
 * the wrong day.
 *
 * @param {string} text
 * @returns {?number} unix seconds, or null if invalid
 */
export function parseLocalDateTime(text) {
    const match = LOCAL_DATE_TIME.exec(String(text).trim());
    if (match === null)
        return null;

    const [, year, month, day, hour, minute] = match.map(Number);
    const d = new Date(year, month - 1, day, hour, minute, 0, 0);
    if (d.getFullYear() !== year || d.getMonth() !== month - 1 || d.getDate() !== day ||
        d.getHours() !== hour || d.getMinutes() !== minute)
        return null;

    return Math.floor(d.getTime() / 1000);
}
