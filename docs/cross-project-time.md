# TODO: procrastinating across projects

**Status: not built.** This is a design note, not a spec. The measurements in it
are real; the decisions are not made.

## The gap

`Actually, I…` (see `gnome/README.md`) fixes the case where the timer says
"Writing" and the last forty minutes went somewhere else — as long as
"somewhere else" is in the same project. It re-labels the closing row and the
time moves.

It stops at the project boundary, and that is where the interesting case lives.
You are on the clock for a client, and the forty minutes went to a personal
project. The time should leave the client's log and land in the personal one.

## Why it stops there

A session is two rows sharing one uuid: an opening marker, and a closing row
that gives it a `to`. `internal/tasklog` treats an opening row whose uuid never
gets a `to` — and which is not the file's last row — as an abandoned session,
and the CLI refuses to run at all until a human fixes it by hand.

So the closing row cannot simply be written into the other project's file. Doing
that leaves the original project holding a uuid that never closes, which is
exactly the failure that check exists to catch.

## The shape of the record

Two rows, in two files, written as one action:

```
projects/client-work/task_log.csv
  aaaa-1,"Writing",              09:00,      ,        # you meant to write
  aaaa-1,"Elsewhere → memorious/Fix sync bug", 09:00, 09:40, true

projects/memorious/task_log.csv
  cccc-3,"Fix sync bug",         09:00, 09:40, true   # where it actually went
```

The client's log keeps a truthful account of the session: it was opened for
Writing, and it ended having gone elsewhere. The personal project gets the work
as an ordinary session. The client is not billed for it.

**Write the destination row first, then close the source.** If the second write
fails, you are left with an unclosed session, which the CLI reports loudly on
the next run. The other order loses the time silently.

## Naming

`Procrastination` is the wrong word, for a reason worth stating: it is a
judgement, and it is often untrue. Being pulled into a production incident
produces the identical record. A label you flinch at is a label you stop using,
and then the data is gone — which is the one outcome that makes the feature
pointless.

**Decided: `Elsewhere`**, with the destination after it — `Elsewhere →
memorious/Fix sync bug`. Short, neutral, reads correctly in a log line and in a
summary ("2h 10m elsewhere this week").

Note that this choice is only permanent under Route A below, where the string is
load-bearing. Under Route B it is display text and can change whenever.

## Two routes

### Route A — zero-duration marker. Works today, no CLI change.

Close the source session with `to` equal to `from`, and put the destination in
the task name.

The invoicer already skips any session shorter than a minute as noise, so a
zero-length row is excluded from billing by existing logic, and the shared uuid
keeps the session closed so the integrity check stays quiet. **Verified against
the current CLI**: the client invoice came back with only the real 1.00h of
writing, the forty minutes billed nowhere, and no integrity complaint.

The cost is that the marker row lies about its duration. "How much time did I
lose to other projects this month" is then not answerable without parsing task
names, and any future tool reading the log sees a zero-second session.

### Route B — a sixth column. Honest, but a lockstep change.

Keep the real span on the marker row and add a column naming where it went:

```
uuid,task,from,to,completed,moved_to
```

`moved_to` holds `<project>/<uuid>` of the destination session, empty for every
ordinary row. Invoicing skips any row with it set; a report can total it per
project and per destination, which is the "where does my time actually go"
question worth having.

**The constraint that decides the rollout**: `internal/tasklog` requires exactly
five fields and returns an error otherwise. **Verified**: feeding it a six-column
file fails with `expected 5 fields, got 6` and the CLI reads *nothing* from that
file — not just the new row. So an old `focuson` binary against a new log is
dead, not degraded. The CLI has to be updated first, and every machine sharing
the data directory has to move together.

Worth knowing: the widget parsers are lenient where Go is strict. Both the Swift
and the JavaScript readers accept five *or more* fields, so the two widgets
would tolerate the new column without changes. The CLI is the only gate.

## Open questions

- **Is the link one-way?** `moved_to` answers "where did this go". It does not
  answer "what did this session interrupt". One-way covers the reporting
  described above; the reverse link costs another column.
- **Should the destination row be billable?** It is an ordinary session, so
  today it is, if that project has a client. Probably correct — the work was
  real — but it means an interruption from client A into client B bills client
  B, which you may want to confirm rather than assume.
- **Does the source project still want a duration?** Route A says no, Route B
  says yes. If the answer is "I want to see that this project lost 4 hours to
  other work this month", that rules out Route A.
- **Does upstream want any of this?** Route B changes a documented schema. Worth
  asking before building, since Route A needs nothing from anyone.

## Suggested order

Route A is a couple of hours and answers "did the client get billed correctly",
which is the part that costs money to get wrong. Route B is the one worth having
if the goal is to actually see where the time goes. They are not exclusive:
Route A's rows can be migrated into Route B's shape later, since the destination
is recoverable from the task name.
