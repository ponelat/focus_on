# TODO: time spent elsewhere

**Status: designed, not built.** Measurements against the current CLI are real.

## The gap

`Actually, I…` re-attributes a session to what you really did, but only within
one project. The case that matters is across projects: on the clock for a
client, forty minutes gone to a personal one. The client should not be billed,
and the client's project should still show that it lost the time.

It stops at the boundary because a session is two rows sharing one uuid, and
`internal/tasklog` treats an opening row whose uuid never gets a `to` as
abandoned — the CLI then refuses to run at all. So the closing row cannot simply
be written into the other project's file.

## The record

Two rows in two files, written as one action:

```
projects/client-work/task_log.csv
  aaaa-1,"Writing",  09:00,      ,     ,
  aaaa-1,"Elsewhere",09:00, 09:40, true,"elsewhere=memorious/cccc-3"

projects/memorious/task_log.csv
  cccc-3,"Fix sync bug",09:00, 09:40, true,
```

The client's log keeps the real duration, so "this project lost 4h to other work
this month" is answerable. Invoicing skips any row with `elsewhere` set, so the
client is not billed. The personal project gets the work as an ordinary session.

**Write the destination row first, then close the source.** If the second write
fails you are left with an unclosed session, which the CLI reports loudly. The
other order loses the time silently.

## Naming

**Decided: `Elsewhere`.** Not "Procrastination" — that is a judgement and it is
often untrue, since being pulled into an incident produces the same record. A
label you flinch at is one you stop using, and then there is no data.

The task name is just `Elsewhere`; the destination lives in the column, not in
the text.

## Schema: one extension column, not one column per feature

Add a sixth column, `meta`, holding space-separated `key=value` pairs:

```
uuid,task,from,to,completed,meta
```

Empty on ordinary rows. First key is `elsewhere=<project>/<uuid>`.

Rules:

- Keys are `[a-z0-9_]+`. Values percent-encode space, `=` and `%`; in practice
  they are slugs, uuids and timestamps, so they never need it.
- **Unknown keys are ignored, never fatal.** This is what lets the widget and
  the CLI ship changes independently. Nothing rewrites existing rows, so
  unknown keys survive by construction.

One column rather than a `key` column plus a `value` column: two columns hold
exactly one attribute per row, so the second feature that wants to annotate the
same row has nowhere to go, and a row cannot be split without breaking the
one-closing-row-per-uuid invariant.

## Rollout: make the reader lenient in the same change

`internal/tasklog` requires *exactly* five fields. **Verified**: a six-column
file fails with `expected 5 fields, got 6` and the CLI then reads nothing at all
from that log — an old binary against a new file is dead, not degraded. Every
machine sharing a data directory has to move together, once.

Make that the last time. Change the check from `!=` to `<`:

```go
if len(fields) < expectedFields {
```

**Verified**: one line, the whole existing suite still passes, and a six-column
file then reads and invoices correctly. After this, any further column is free —
old readers ignore it. Both widget parsers already accept five-or-more, so the
CLI is the only gate.

Ship the leniency change and the `meta` column together, since they cost one
migration between them.

## Open questions

- **Does an interruption from client A into client B bill client B?** The
  destination is an ordinary session, so today it would. Probably right, but
  worth deciding rather than discovering.
- **Does upstream want this?** It changes a documented schema. The leniency
  change is worth proposing on its own merits either way.
