package gowild_knowledge

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	data "github.com/original-david-knight/go_wild/data"
)

// Search modes.
const (
	ModeHybrid   = "hybrid"
	ModeKeyword  = "keyword"
	ModeSemantic = "semantic"
)

// Search tuning.
const (
	// DefaultLimit is deliberately small: callers, agents above all, should
	// read a few strong hits and fetch full records only when needed.
	DefaultLimit   = 10
	MaxLimit       = 50
	candidatePool  = 60
	rrfK           = 60.0
	snippetLen     = 240
	queryTimeout   = 4 * time.Second
	embedCharLimit = 8000
)

// Semantic neighbours always exist, so two cuts keep unrelated ones from
// padding results: an absolute floor, and a band below the best neighbour.
// Tuned on gemini-embedding-001 at 768 dimensions, where unrelated text
// scores 0.48-0.59 and related text 0.58-0.70, so a fixed cut alone either
// admits noise or drops short queries' only real match.
var (
	MinSimilarity  = 0.57
	SimilarityBand = 0.03
)

// SearchQuery is one search. An empty Text browses: the filters apply and
// hits come newest first.
type SearchQuery struct {
	Text            string
	Kinds           []string
	Contexts        []string
	SourceID        string
	EntityID        string
	Tag             string
	Since           time.Time
	Until           time.Time
	Limit           int
	Offset          int
	Mode            string
	IncludeInactive bool
}

// SearchHit is one ranked record.
type SearchHit struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Snippet    string    `json:"snippet"`
	SourceID   string    `json:"source_id"`
	Context    string    `json:"context"`
	OccurredAt time.Time `json:"occurred_at"`
	Score      float64   `json:"score"`
	Keyword    bool      `json:"keyword"`
	Semantic   bool      `json:"semantic"`
}

// SearchResult is a page of hits. Semantic reports whether the semantic
// half ran; SemanticError says why it did not when it was wanted.
type SearchResult struct {
	Hits          []SearchHit `json:"hits"`
	Semantic      bool        `json:"semantic"`
	SemanticError string      `json:"semantic_error,omitempty"`
}

type candidate struct {
	SearchHit
	weight float64
	sim    float64
}

// Search ranks records by keyword and meaning together. Each half yields up
// to 60 candidates; reciprocal rank fusion combines them and each record's
// weight (facts over notes over raw items, verified over unverified) scales
// the result.
func (s *Service) Search(ctx context.Context, db data.Database, q SearchQuery) (*SearchResult, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	if q.Offset < 0 || q.Offset > 500 {
		return nil, invalidf("offset must be between 0 and 500")
	}
	if q.Mode == "" {
		q.Mode = ModeHybrid
	}
	if q.Mode != ModeHybrid && q.Mode != ModeKeyword && q.Mode != ModeSemantic {
		return nil, invalidf("mode must be hybrid, keyword or semantic")
	}
	for _, k := range q.Kinds {
		if k != KindItem && k != KindFact && k != KindNote && k != KindEntity {
			return nil, invalidf("kind %q must be item, fact, note or entity", k)
		}
	}
	if q.EntityID != "" {
		e, err := resolveEntity(ctx, db, q.EntityID)
		if err != nil {
			return nil, err
		}
		q.EntityID = e.ID
	}
	res := &SearchResult{Hits: []SearchHit{}}
	exec, backend, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	if q.Text == "" {
		hits, err := browse(ctx, exec, backend, q)
		if err != nil {
			return nil, err
		}
		res.Hits = hits
		return res, nil
	}

	var keyword, semantic []candidate
	if q.Mode != ModeSemantic {
		if keyword, err = keywordCandidates(ctx, exec, backend, q); err != nil {
			return nil, err
		}
	}
	if q.Mode != ModeKeyword {
		semantic, err = s.semanticCandidates(ctx, db, q)
		if err != nil {
			res.SemanticError = err.Error()
			if q.Mode == ModeSemantic {
				return res, nil
			}
		} else {
			res.Semantic = true
		}
	}

	fused := map[string]*candidate{}
	var order []string
	add := func(list []candidate, isKeyword bool) {
		for rank, c := range list {
			f, ok := fused[c.ID]
			if !ok {
				cc := c
				cc.Score = 0
				f = &cc
				fused[c.ID] = f
				order = append(order, c.ID)
			}
			f.Score += 1 / (rrfK + float64(rank+1))
			if isKeyword {
				f.Keyword = true
				if c.Snippet != "" {
					f.Snippet = c.Snippet
				}
			} else {
				f.Semantic = true
			}
		}
	}
	add(keyword, true)
	add(semantic, false)
	all := make([]*candidate, 0, len(order))
	for _, id := range order {
		c := fused[id]
		c.Score *= c.weight
		all = append(all, c)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	for i := q.Offset; i < len(all) && len(res.Hits) < q.Limit; i++ {
		res.Hits = append(res.Hits, all[i].SearchHit)
	}
	return res, nil
}

// filterSQL renders the query's filters as a WHERE fragment with "?"
// placeholders.
func filterSQL(backend data.Backend, q SearchQuery) (string, []any) {
	var clauses []string
	var args []any
	if !q.IncludeInactive {
		if backend == data.BackendPostgres {
			clauses = append(clauses, "active")
		} else {
			clauses = append(clauses, "active = 1")
		}
	}
	if len(q.Kinds) > 0 {
		clauses = append(clauses, "kind IN ("+placeholders(len(q.Kinds))+")")
		for _, k := range q.Kinds {
			args = append(args, k)
		}
	}
	if len(q.Contexts) > 0 {
		clauses = append(clauses, "context IN ("+placeholders(len(q.Contexts))+")")
		for _, c := range q.Contexts {
			args = append(args, c)
		}
	}
	if q.SourceID != "" {
		clauses = append(clauses, "source_id = ?")
		args = append(args, q.SourceID)
	}
	if q.EntityID != "" {
		clauses = append(clauses, `(id = ?
			OR id IN (SELECT from_id FROM kb_links WHERE rel = 'about' AND to_id = ?)
			OR id IN (SELECT p.item_id FROM kb_item_participants p JOIN kb_entity_aliases a ON a.id = p.alias WHERE a.entity_id = ?))`)
		args = append(args, q.EntityID, q.EntityID, q.EntityID)
	}
	if q.Tag != "" {
		clauses = append(clauses, "id IN (SELECT from_id FROM kb_links WHERE rel = 'tag' AND to_id = ?)")
		args = append(args, tagID(strings.ToLower(strings.TrimSpace(q.Tag))))
	}
	if !q.Since.IsZero() {
		clauses = append(clauses, "occurred_at >= ?")
		args = append(args, timeArg(backend, q.Since))
	}
	if !q.Until.IsZero() {
		clauses = append(clauses, "occurred_at < ?")
		args = append(args, timeArg(backend, q.Until))
	}
	if len(clauses) == 0 {
		return "TRUE", args
	}
	return strings.Join(clauses, " AND "), args
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

const hitColumns = "id, kind, title, source_id, context, occurred_at, weight"

func scanHit(backend data.Backend, scan func(dest ...any) error, extra ...any) (candidate, error) {
	var c candidate
	var occurred any
	dest := append([]any{&c.ID, &c.Kind, &c.Title, &c.SourceID, &c.Context, &occurred, &c.weight}, extra...)
	if err := scan(dest...); err != nil {
		return c, err
	}
	switch v := occurred.(type) {
	case time.Time:
		c.OccurredAt = v.UTC()
	case string:
		c.OccurredAt, _ = time.Parse(sqliteTime, v)
	case []byte:
		c.OccurredAt, _ = time.Parse(sqliteTime, string(v))
	}
	return c, nil
}

func browse(ctx context.Context, exec data.Executor, backend data.Backend, q SearchQuery) ([]SearchHit, error) {
	where, args := filterSQL(backend, q)
	args = append(args, q.Limit, q.Offset)
	rows, err := exec.QueryContext(ctx, rebind(backend,
		"SELECT "+hitColumns+", substr(body, 1, 600) FROM kb_search WHERE "+where+" ORDER BY occurred_at DESC LIMIT ? OFFSET ?"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var body string
		c, err := scanHit(backend, rows.Scan, &body)
		if err != nil {
			return nil, err
		}
		c.Snippet = leadSnippet(c.Kind, c.Title, body)
		hits = append(hits, c.SearchHit)
	}
	return hits, rows.Err()
}

func keywordCandidates(ctx context.Context, exec data.Executor, backend data.Backend, q SearchQuery) ([]candidate, error) {
	where, args := filterSQL(backend, q)
	if backend == data.BackendPostgres {
		query := `SELECT ` + hitColumns + `, substr(body, 1, 600),
				ts_headline('english', substr(body, 1, 20000), tq,
					'MaxFragments=2, MaxWords=24, MinWords=8, FragmentDelimiter=" … ", StartSel=«, StopSel=»')
			FROM kb_search, websearch_to_tsquery('english', ?) tq
			WHERE tsv @@ tq AND ` + where + `
			ORDER BY ts_rank_cd(tsv, tq) DESC LIMIT ?`
		all := append([]any{q.Text}, args...)
		all = append(all, candidatePool)
		rows, err := exec.QueryContext(ctx, rebind(backend, query), all...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []candidate
		for rows.Next() {
			var body, headline string
			c, err := scanHit(backend, rows.Scan, &body, &headline)
			if err != nil {
				return nil, err
			}
			if strings.Contains(headline, "«") {
				c.Snippet = strings.TrimSpace(headline)
			} else {
				c.Snippet = leadSnippet(c.Kind, c.Title, body)
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	// SQLite: every term must appear; newest first.
	terms := strings.Fields(strings.ToLower(strings.NewReplacer(`"`, " ", "'", " ").Replace(q.Text)))
	if len(terms) == 0 {
		return nil, nil
	}
	var conds []string
	var targs []any
	for _, t := range terms {
		conds = append(conds, "instr(lower(title || ' ' || body), ?) > 0")
		targs = append(targs, t)
	}
	query := "SELECT " + hitColumns + ", body FROM kb_search WHERE " + strings.Join(conds, " AND ") + " AND " + where + " ORDER BY occurred_at DESC LIMIT ?"
	all := append(targs, args...)
	all = append(all, candidatePool)
	rows, err := exec.QueryContext(ctx, query, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var body string
		c, err := scanHit(backend, rows.Scan, &body)
		if err != nil {
			return nil, err
		}
		c.Snippet = termSnippet(c.Kind, c.Title, body, terms[0])
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) semanticCandidates(ctx context.Context, db data.Database, q SearchQuery) ([]candidate, error) {
	if s.embedder == nil {
		return nil, errSemanticOff("no embedder is configured")
	}
	caps := s.vectorCaps(ctx, db)
	if !caps.ok {
		return nil, errSemanticOff("the database has no vector support")
	}
	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	vec, err := s.embedder.EmbedQuery(qctx, q.Text)
	if err != nil {
		return nil, errSemanticOff("embedding the query failed: " + err.Error())
	}
	if len(vec) != EmbeddingDimensions {
		return nil, errSemanticOff("the embedder returned the wrong width")
	}
	normalize(vec)
	exec, backend, err := data.Raw(db)
	if err != nil {
		return nil, err
	}
	where, args := filterSQL(backend, q)
	if backend == data.BackendPostgres {
		return pgVectorCandidates(ctx, exec, caps, where, args, vec)
	}
	rows, err := exec.QueryContext(ctx, "SELECT "+hitColumns+", substr(body, 1, 600), embedding FROM kb_search WHERE embedding IS NOT NULL AND "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var body string
		var blob []byte
		c, err := scanHit(backend, rows.Scan, &body, &blob)
		if err != nil {
			return nil, err
		}
		c.sim = dot(vec, decodeVector(blob))
		if c.sim < MinSimilarity {
			continue
		}
		c.Snippet = leadSnippet(c.Kind, c.Title, body)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].sim > out[j].sim })
	out = withinBand(out)
	if len(out) > candidatePool {
		out = out[:candidatePool]
	}
	return out, nil
}

// withinBand keeps the neighbours within SimilarityBand of the best one;
// list must be sorted by similarity, best first.
func withinBand(list []candidate) []candidate {
	if len(list) == 0 {
		return list
	}
	cut := list[0].sim - SimilarityBand
	for i, c := range list {
		if c.sim < cut {
			return list[:i]
		}
	}
	return list
}

func pgVectorCandidates(ctx context.Context, exec data.Executor, caps vectorCaps, where string, args []any, vec []float32) ([]candidate, error) {
	// Filtered HNSW scans need a wider beam, and on pgvector 0.8+ an
	// iterative scan, so filters do not starve the candidate list. Both are
	// transaction-local settings.
	run := func(q interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	}) ([]candidate, error) {
		all := append([]any{vectorLiteral(vec)}, args...)
		all = append(all, candidatePool)
		rows, err := q.QueryContext(ctx, rebind(data.BackendPostgres, `WITH qv AS (SELECT ?::vector AS v)
			SELECT `+hitColumns+`, substr(body, 1, 600), 1 - (embedding <=> qv.v)
			FROM kb_search, qv
			WHERE embedding IS NOT NULL AND `+where+`
			ORDER BY embedding <=> qv.v LIMIT ?`), all...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []candidate
		for rows.Next() {
			var body string
			var sim float64
			c, err := scanHit(data.BackendPostgres, rows.Scan, &body, &sim)
			if err != nil {
				return nil, err
			}
			c.sim = sim
			if c.sim < MinSimilarity {
				continue
			}
			c.Snippet = leadSnippet(c.Kind, c.Title, body)
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].sim > out[j].sim })
		return withinBand(out), nil
	}
	db, ok := exec.(*sql.DB)
	if !ok {
		return run(exec)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('hnsw.ef_search', '200', true)`); err != nil {
		return nil, err
	}
	if caps.iterative {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('hnsw.iterative_scan', 'relaxed_order', true)`); err != nil {
			return nil, err
		}
	}
	return run(tx)
}

type errSemanticOff string

func (e errSemanticOff) Error() string { return string(e) }

// leadSnippet is the opening of a record: a fact's text, else its body.
func leadSnippet(kind, title, body string) string {
	if kind == KindFact {
		return title
	}
	return clip(strings.Join(strings.Fields(body), " "), snippetLen)
}

// termSnippet is a window of body around the first occurrence of term.
func termSnippet(kind, title, body, term string) string {
	if kind == KindFact {
		return title
	}
	flat := strings.Join(strings.Fields(body), " ")
	i := strings.Index(strings.ToLower(flat), term)
	if i < 0 {
		return clip(flat, snippetLen)
	}
	start := i - snippetLen/3
	if start < 0 {
		start = 0
	}
	for start > 0 && !utf8.RuneStart(flat[start]) {
		start--
	}
	out := clip(flat[start:], snippetLen)
	if start > 0 {
		out = "… " + out
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(truncateRunes(s, n)) + " …"
}
