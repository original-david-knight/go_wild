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
items.

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
agents wrote. Only the owner deletes facts, items, entities and sources. The
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

`ExpireUnread(ctx, db, now, window)` retires agent facts nobody read within
the window, counting from the last read, or from creation for a fact never
read. An expired fact leaves search the way a retracted one does, still
lists in `ListFacts` with `expired` set (`FactFilter.Expired` narrows to or
away from them), and comes back on its next explicit read through
`NoteRead`. The owner's facts never expire.
