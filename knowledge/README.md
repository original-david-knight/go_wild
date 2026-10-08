# knowledge (`gowild_knowledge`)

A personal knowledge base over `gowild_data`:

- **Items**: imported records pushed by importers under a source
  (`gmail:personal`), keyed by external ID, so re-pushes are idempotent and
  source deletions are mirrored.
- **Facts**: short statements with author, confidence, validity, supersession
  and links to the items they came from and the entities they are about.
- **Notes**: free-form pages.
- **Entities**: people, orgs, projects and places, with `scheme:value`
  aliases that join participants across sources, and merges.

Search runs over one derived table, `kb_search`, which is kept in step with
every write. On PostgreSQL it uses a generated `tsvector` and, when pgvector
is installed, 768-dimension embeddings with an HNSW index. On SQLite (tests,
small deployments) it uses a substring match and an in-process cosine scan.
Keyword and semantic candidates are fused by reciprocal rank and weighted by
kind: the owner's facts rank highest, then agents' facts, entities, notes and
items. "Ranking" below has the rest.

```go
svc := gowild_knowledge.New(gowild_knowledge.WithEmbedder(e)) // e: Embedder, optional
svc.PutSource(ctx, db, gowild_knowledge.Owner, "gmail:personal", gowild_knowledge.SourceInput{})
svc.Ingest(ctx, db, gowild_knowledge.Agent("importer"), "gmail:personal", batch)
svc.CreateFact(ctx, db, gowild_knowledge.Agent("fable"), gowild_knowledge.FactInput{Text: &text})
svc.Search(ctx, db, gowild_knowledge.SearchQuery{Text: "dentist"})
svc.ItemsInRange(ctx, db, gowild_knowledge.ItemRange{SourceID: "slack:acme", Prefix: "C0123/"}) // one conversation's days, newest first
svc.EmbedPending(ctx, db, 50) // on a timer
```

The write fence: the owner changes anything, while an agent changes only what
agents wrote. Only the owner deletes facts, items, entities and sources.

A record's `author_kind` says whose word it is and `author` says who wrote
it. `Owner` writes as `owner`; a caller whose owner speaks through a client
(an assistant he dictates to) passes `Actor{Kind: AuthorOwner, Name:
"mcp:claude"}`, and the record is the owner's with the client as its author.
`AdoptFact(ctx, db, Owner, id)` turns an agent's fact into the owner's the
same way: owner kind, confidence 1, no longer expired, author kept. Only the
owner adopts, and adopting an owner fact changes nothing. The
`kb_facts.verified` column from an earlier version stays in existing databases
and nothing reads or writes it.

`agentic_loop.RetrievalEmbedder` is the Gemini implementation of `Embedder`.
The tests run every case on SQLite and, when `initdb` is on PATH, on a
throwaway PostgreSQL cluster. That cluster runs the pgvector cases when the
extension is installed.

## Reads

The library records which facts get used:

- A `Search` with non-empty text and a set `Reader` writes one `kb_queries`
  row (reader, text, contexts, hits, fact hits) and counts a read on every
  fact among the returned hits.
- `NoteRead(ctx, db, reader, ids...)` counts a read on each fact a known
  reader fetched explicitly, such as an HTTP `GET` of one fact. `GetFact`
  itself records nothing, since callers also use it for their own views.
- A browse (empty text), `ListFacts`, and creating or editing a fact count
  nothing.

A read raises `ReadCount` and stamps `LastReadAt`. It changes neither the
search weight nor `UpdatedAt`. A failed recording is logged and never fails
the search. `ListQueries` lists the log, newest first.

`ExpireUnread(ctx, db, now, window, epoch)` retires agent facts nobody read
within the window, counting from the latest of the fact's creation, its last
read and `epoch`. The epoch restarts every fact's clock at once, for a caller
whose readers began reading after the facts were written; a zero epoch
counts from creation and reads alone. An expired fact leaves search the way a retracted one does, still
lists in `ListFacts` with `expired` set (`FactFilter.Expired` narrows to or
away from them), and comes back on its next explicit read through
`NoteRead`. The owner's facts never expire.

## Ended facts

A fact whose `valid_until` day is over has ended. `valid_until` names the
last day the fact holds and writers store a day as its start, so a fact ends
24 hours after its `valid_until`: a fact about an event holds through the
day of the event. `Ended` is stored on the fact: a write that sets or clears `valid_until` decides it against the
service clock, and `EndElapsed(ctx, db, now)`, run on a timer, ends the facts
whose validity ran out since. An ended fact leaves search, `ListFacts` and
its entity's page the way a superseded one does, still comes back from
`GetFact` and with `IncludeInactive`, and is live again once an edit moves
its `valid_until` into the future or clears it. Ending applies to the
owner's facts too, and ExpireUnread passes over ended facts.

## Ranking

A hit's score is its reciprocal-rank fusion score (k = 60 over each half's
top 60) times its weight: 1.8 for the owner's facts, 1.5 for agents' facts
(both scaled by confidence), 1.3 for entities, 1.2 for notes and 1.0 for
items.

An item's weight also fades with age, measured from the service clock: it
halves its distance to a floor of 0.5 every 30 days (`RecencyHalfLife`,
`RecencyFloor`), so last week's mail outranks last spring's and anything
older than about four months sits at half weight, in the order relevance
gives it. Items dated in the future keep full weight. Facts, notes and
entities do not fade: a birthdate or an address recorded a year ago ranks as
it did the day it was written.

Hits scoring under a quarter of the best hit's score are dropped
(`MinRelativeScore`), so a page can hold fewer hits than its limit: a fact
found by both halves ends the list before year-old mail that matched one
word of the question.

Keyword search requires every term, so it runs on the question without its
relative time words ("this month", "next week", "upcoming", "recent"):
records rarely contain them, and recency already favours what is current.
The semantic half embeds the whole question.
