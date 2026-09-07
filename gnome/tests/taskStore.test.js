// Run with: node --test gnome/tests/
//
// The state machine, tested against a fake logger and a fake GSettings. These
// are the rows that end up in someone's billing record, so the assertions are
// about exact row sequences rather than about the store's own fields.

import test from 'node:test';
import assert from 'node:assert/strict';

import {TaskStore} from '../focuson@ckritzinger.github.io/lib/taskStore.js';

/** Just enough Gio.Settings for the four keys the store persists. */
function fakeSettings(initial = {}) {
    const values = {
        'current-task-name': '',
        'current-project-slug': 'personal',
        'current-task-uuid': '',
        'current-task-started-at': 0,
        ...initial,
    };
    return {
        get_string: k => values[k],
        set_string: (k, v) => (values[k] = v),
        get_int64: k => values[k],
        set_int64: (k, v) => (values[k] = v),
        _values: values,
    };
}

function fakeLogger({projects = ['personal'], rows = []} = {}) {
    let n = 0;
    return {
        written: [],
        bootstrapDataDirectoryIfNeeded() {},
        listProjectSlugs: () => projects,
        readAllRows: () => rows,
        newUUID: () => `uuid-${++n}`,
        appendRow(row) {
            this.written.push(row);
        },
    };
}

function trackingStore(taskName = 'Writing') {
    const logger = fakeLogger();
    const store = new TaskStore(fakeSettings(), logger);
    store.loadState();
    store.startTask(taskName, 'personal', false);
    logger.written.length = 0; // drop the opening row; each test asserts what follows
    return {store, logger};
}

test('closeCurrentTaskAs bills the elapsed time to the real task', () => {
    const {store, logger} = trackingStore('Writing');
    const startedAt = store.currentTaskStartedAt;

    store.closeCurrentTaskAs('Fiddling with FocusOn', true);

    assert.equal(logger.written.length, 1, 'exactly one closing row');
    const row = logger.written[0];
    assert.equal(row.task, 'Fiddling with FocusOn', 'the invoice reads the closing row');
    assert.equal(row.completed, true);
    assert.equal(row.from, startedAt, 'the whole elapsed span moves, not part of it');
    assert.ok(row.to !== null && row.to >= startedAt);
});

test('the closing row keeps the session uuid, so the pair still closes', () => {
    // internal/tasklog treats an opening row whose uuid never gets a `to` as
    // an abandoned session and refuses to run. A new uuid here would create
    // exactly that, on every use of the feature.
    const {store, logger} = trackingStore('Writing');
    const uuid = store.currentTaskUUID;

    store.closeCurrentTaskAs('Fiddling with FocusOn', true);

    assert.equal(logger.written[0].uuid, uuid);
});

test('the closing row stays in the project the opening row was written to', () => {
    // A closing row in another project's file would leave the original
    // project holding an unclosed uuid — the dangling-entry case.
    const logger = fakeLogger({projects: ['personal', 'client-work']});
    const store = new TaskStore(fakeSettings(), logger);
    store.loadState();
    store.startTask('Writing', 'client-work', false);
    logger.written.length = 0;

    store.closeCurrentTaskAs('Fiddling with FocusOn', true);

    assert.equal(logger.written[0].project, 'client-work');
});

test('nothing is tracked afterwards, so the next task starts clean', () => {
    const {store} = trackingStore('Writing');
    store.closeCurrentTaskAs('Fiddling with FocusOn', true);

    assert.equal(store.isTracking, false);
    assert.equal(store.currentTaskName, null);
    assert.equal(store.currentTaskUUID, null);
    assert.equal(store.currentTaskStartedAt, null);
});

test('closing as an unfinished task writes completed=false', () => {
    const {store, logger} = trackingStore('Writing');
    store.closeCurrentTaskAs('Fiddling with FocusOn', false);
    assert.equal(logger.written[0].completed, false);
});

test('closeCurrentTaskAs does nothing when nothing is being tracked', () => {
    const logger = fakeLogger();
    const store = new TaskStore(fakeSettings(), logger);
    store.loadState();
    logger.written.length = 0;

    store.closeCurrentTaskAs('Fiddling with FocusOn', true);

    assert.equal(logger.written.length, 0);
});

test('resuming afterwards is a fresh session, not a reuse of the old uuid', () => {
    const {store, logger} = trackingStore('Writing');
    const firstUUID = store.currentTaskUUID;

    store.closeCurrentTaskAs('Fiddling with FocusOn', true);
    store.startTask('Writing', 'personal', false);

    const opened = logger.written[1];
    assert.equal(opened.task, 'Writing');
    assert.equal(opened.to, null, 'an opening row has no end time');
    assert.notEqual(opened.uuid, firstUUID, 'two `to`-filled rows on one uuid would break invoicing');
});

// The existing behaviour this feature sits next to, pinned so the refactor
// that made the logger injectable cannot have changed it.

test('completeCurrentTask still writes the name the session started under', () => {
    const {store, logger} = trackingStore('Writing');
    store.completeCurrentTask();
    assert.equal(logger.written[0].task, 'Writing');
    assert.equal(logger.written[0].completed, true);
});

test('pauseCurrentTask still writes completed=false under the same name', () => {
    const {store, logger} = trackingStore('Writing');
    store.pauseCurrentTask();
    assert.equal(logger.written[0].task, 'Writing');
    assert.equal(logger.written[0].completed, false);
});

test('a screen-lock-free shutdown closes the open session', () => {
    const {store, logger} = trackingStore('Writing');
    store.writeClosingRowOnTerminate();
    assert.equal(logger.written.length, 1);
    assert.equal(logger.written[0].completed, false);
});
