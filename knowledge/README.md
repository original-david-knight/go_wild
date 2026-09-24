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
kind: verified facts rank highest, then facts, entities, notes and items.

```go
svc := gowild_knowledge.New(gowild_knowledge.WithEmbedder(e)) // e: Embedder, optional
svc.PutSource(ctx, db, gowild_knowledge.Owner, "gmail:personal", gowild_knowledge.SourceInput{})
svc.Ingest(ctx, db, gowild_knowledge.Agent("importer"), "gmail:personal", batch)
svc.CreateFact(ctx, db, gowild_knowledge.Agent("fable"), gowild_knowledge.FactInput{Text: &text})
svc.Search(ctx, db, gowild_knowledge.SearchQuery{Text: "dentist"})
svc.EmbedPending(ctx, db, 50) // on a timer
```

The write fence: the owner changes anything, while an agent changes only what
agents wrote and the owner has not verified. Only the owner deletes facts,
items, entities and sources, and only the owner verifies.

`agentic_loop.RetrievalEmbedder` is the Gemini implementation of `Embedder`.
The tests run every case on SQLite and, when `initdb` is on PATH, on a
throwaway PostgreSQL cluster. That cluster runs the pgvector cases when the
extension is installed.
