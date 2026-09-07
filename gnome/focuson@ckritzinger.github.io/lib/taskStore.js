// Port of FocusOn/TaskStore.swift.
//
// Same state machine, same invariants, same append-only discipline; only the
// persistence layer differs — GSettings here, UserDefaults there. Kept as a
// plain JS class rather than a GObject: nothing outside needs it to be one,
// and it keeps the extension free of GType registrations that have to
// survive a disable/enable cycle.

import * as CSVLogger from './csvLogger.js';

/** Unix seconds. The CSV stores whole seconds, so there's nothing finer to keep. */
function now() {
    return Math.floor(Date.now() / 1000);
}

export class TaskStore {
    constructor(settings) {
        this._settings = settings;
        this._listeners = new Set();

        this.currentTaskName = null;
        this.currentTaskStartedAt = null;

        /**
         * The UUID shared by the active task's opening row and whatever row
         * eventually closes it (see CSVLogger.appendRow). Generated fresh
         * each time a task starts; never reused across sessions.
         */
        this.currentTaskUUID = null;

        /**
         * The project the *currently active* task belongs to. Persists even
         * after the task completes or pauses, so it doubles as "last used
         * project" — the task picker's default next time it opens.
         */
        this.currentProjectSlug = 'personal';

        /**
         * Source of truth is CSVLogger.listProjectSlugs() (a directory
         * listing of projects/), never manifest.toml — see csvLogger.js.
         */
        this.availableProjects = ['personal'];
    }

    /** Fires whenever anything the UI renders has changed. */
    connect(callback) {
        this._listeners.add(callback);
        return () => this._listeners.delete(callback);
    }

    _emitChanged() {
        for (const cb of this._listeners)
            cb();
    }

    get isTracking() {
        return this.currentTaskName !== null;
    }

    loadState() {
        const name = this._settings.get_string('current-task-name');
        this.currentTaskName = name === '' ? null : name;
        this.currentProjectSlug = this._settings.get_string('current-project-slug') || 'personal';

        CSVLogger.bootstrapDataDirectoryIfNeeded();
        this.refreshAvailableProjects();

        if (this.currentTaskName === null) {
            this._emitChanged();
            return;
        }

        // Whatever was active when the extension was last disabled already
        // got a closing row written by writeClosingRowOnTerminate — or, if
        // the shell died outright, was never closed and stays an orphaned
        // open row for `focuson`'s integrity check to raise. Either way the
        // old uuid's story is already told on disk. "Resuming" the same task
        // here is therefore a brand new session — new uuid, new start time —
        // never a reuse of the old uuid, which would otherwise produce two
        // `to`-filled rows sharing one uuid and break the one-row-per-uuid
        // invariant invoicing depends on.
        const startedAt = now();
        const uuid = CSVLogger.newUUID();
        this.currentTaskStartedAt = startedAt;
        this.currentTaskUUID = uuid;
        this._persistState();
        CSVLogger.appendRow({
            project: this.currentProjectSlug,
            uuid,
            task: this.currentTaskName,
            from: startedAt,
            to: null,
            completed: null,
        });
        this._emitChanged();
    }

    refreshAvailableProjects() {
        const projects = CSVLogger.listProjectSlugs();
        this.availableProjects = projects.length > 0 ? projects : ['personal'];

        // Only ever re-point the "last used project" while nothing is being
        // tracked. Moving it mid-session would send the active task's
        // closing row to a different project's CSV than its opening row —
        // one uuid split across two files, which is both a lost session and
        // exactly the shape the CLI's integrity check reports as abandoned.
        if (!this.isTracking && !this.availableProjects.includes(this.currentProjectSlug))
            this.currentProjectSlug = this.availableProjects[0];

        this._emitChanged();
    }

    /**
     * Recent incomplete tasks scoped to one project — task names aren't
     * deduplicated across projects, since they live in separate CSV files.
     * Called directly by the task picker as its selected project changes,
     * rather than cached here.
     *
     * @param {string} project project slug
     * @returns {{name: string, startedAt: number}[]} most recent first, at most 10
     */
    recentTasks(project) {
        const rows = CSVLogger.readAllRows(project);

        // Include open rows (no closing time) and paused rows (closed but
        // not completed). Exclude rows explicitly marked completed = true.
        const incomplete = rows.filter(r => r.completed !== true);

        const seen = new Map();
        for (let i = incomplete.length - 1; i >= 0; i--) {
            const row = incomplete[i];
            if (!seen.has(row.task))
                seen.set(row.task, row.from);
        }

        return [...seen.entries()]
            .map(([name, startedAt]) => ({name, startedAt}))
            .sort((a, b) => b.startedAt - a.startedAt)
            .slice(0, 10);
    }

    startTask(name, project, completingPrevious) {
        const at = now();

        if (this.currentTaskName !== null &&
            this.currentTaskStartedAt !== null &&
            this.currentTaskUUID !== null) {
            CSVLogger.appendRow({
                project: this.currentProjectSlug,
                uuid: this.currentTaskUUID,
                task: this.currentTaskName,
                from: this.currentTaskStartedAt,
                to: at,
                completed: !!completingPrevious,
            });
        }

        const uuid = CSVLogger.newUUID();
        this.currentTaskName = name;
        this.currentProjectSlug = project;
        this.currentTaskStartedAt = at;
        this.currentTaskUUID = uuid;
        this._persistState();
        CSVLogger.appendRow({project, uuid, task: name, from: at, to: null, completed: null});
        this._emitChanged();
    }

    completeCurrentTask() {
        this._closeCurrentTask(true);
    }

    pauseCurrentTask() {
        this._closeCurrentTask(false);
    }

    _closeCurrentTask(completed) {
        if (this.currentTaskName === null ||
            this.currentTaskStartedAt === null ||
            this.currentTaskUUID === null)
            return;

        CSVLogger.appendRow({
            project: this.currentProjectSlug,
            uuid: this.currentTaskUUID,
            task: this.currentTaskName,
            from: this.currentTaskStartedAt,
            to: now(),
            completed,
        });

        this.currentTaskName = null;
        this.currentTaskStartedAt = null;
        this.currentTaskUUID = null;
        this._persistState();
        this._emitChanged();
    }

    /**
     * Writes a single fully-formed row directly — for time you forgot to
     * start the timer for. No pairing (both from and to are already known),
     * and no interaction with the currently tracked task: this never touches
     * whatever is being tracked live.
     */
    logPastSession(project, task, from, to, completed) {
        CSVLogger.appendRow({
            project,
            uuid: CSVLogger.newUUID(),
            task,
            from,
            to,
            completed,
        });
    }

    /**
     * The applicationWillTerminate analogue: called from disable(), which
     * the shell runs on logout, on a shell restart, and when the extension
     * is turned off. Not called when the screen locks — that's why
     * metadata.json lists the unlock-dialog session mode. Without it, every
     * screen lock would close the session and every unlock would open a new
     * one, chopping a day's work into fragments.
     */
    writeClosingRowOnTerminate() {
        if (this.currentTaskName === null ||
            this.currentTaskStartedAt === null ||
            this.currentTaskUUID === null)
            return;

        CSVLogger.appendRow({
            project: this.currentProjectSlug,
            uuid: this.currentTaskUUID,
            task: this.currentTaskName,
            from: this.currentTaskStartedAt,
            to: now(),
            completed: false,
        });
    }

    _persistState() {
        this._settings.set_string('current-task-name', this.currentTaskName ?? '');
        this._settings.set_string('current-project-slug', this.currentProjectSlug);
        this._settings.set_string('current-task-uuid', this.currentTaskUUID ?? '');
        this._settings.set_int64('current-task-started-at', this.currentTaskStartedAt ?? 0);
    }
}
