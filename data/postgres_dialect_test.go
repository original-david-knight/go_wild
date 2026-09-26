package gowild_data

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"
)

// pgRow carries the field shapes the Postgres dialect encodes differently:
// BYTEA, JSONB arrays, maps and objects, native booleans and pointers to each.
// Time columns are left out: they need a real TIMESTAMPTZ column and are
// covered by TestPostgresDialectAppliesScannedTimes below.
type pgRow struct {
	ID      string            `json:"id"`
	UserID  string            `json:"user_id"`
	Raw     []byte            `json:"raw"`
	RawPtr  *[]byte           `json:"raw_ptr"`
	Tags    []string          `json:"tags"`
	TagsPtr *[]string         `json:"tags_ptr"`
	Labels  map[string]int    `json:"labels"`
	Home    address           `json:"home"`
	Work    *address          `json:"work"`
	Nick    *string           `json:"nick"`
	Count   *int64            `json:"count"`
	Score   *float64          `json:"score"`
	Ratio   float32           `json:"ratio"`
	Flag    bool              `json:"flag"`
	FlagPtr *bool             `json:"flag_ptr"`
	Extra   map[string]string `json:"extra"`
}

func (pgRow) TableName() string { return "pg_rows" }

// newPostgresOverSqlite builds a PostgresDatabase whose handle is an
// in-memory SQLite connection. SQLite accepts Postgres's $n placeholders and
// its column type names (BYTEA, JSONB, BOOLEAN, DOUBLE PRECISION), so the
// Postgres dialect's encode and decode run through a real driver without a
// Postgres server. AddTable is not used: its migration reads
// information_schema, which SQLite does not have.
func newPostgresOverSqlite(t *testing.T, models ...any) *PostgresDatabase {
	t.Helper()
	lite, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lite.Close() })
	pg := &PostgresDatabase{db: lite.db, tables: map[string]*modelMeta{}}
	for _, m := range models {
		meta, err := getModelMeta(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lite.db.Exec(meta.createTableSQLPostgres()); err != nil {
			t.Fatal(err)
		}
		pg.tables[meta.TableName] = meta
	}
	return pg
}

func TestPostgresDialectRoundTripsFieldShapes(t *testing.T) {
	pg := newPostgresOverSqlite(t, pgRow{})
	ctx := context.Background()

	raw := []byte{0, 1, 2}
	tags := []string{"x", "y"}
	nick, count, score, flag := "n", int64(-9), 0.25, true
	in := pgRow{
		ID:      "p1",
		Raw:     []byte{9, 8},
		RawPtr:  &raw,
		Tags:    []string{"a"},
		TagsPtr: &tags,
		Labels:  map[string]int{"k": 3},
		Home:    address{City: "Rome", Zip: "00100"},
		Work:    &address{City: "Milan"},
		Nick:    &nick,
		Count:   &count,
		Score:   &score,
		Ratio:   1.5,
		Flag:    true,
		FlagPtr: &flag,
	}
	if err := pg.Table(pgRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got pgRow
	if err := pg.Table(pgRow{}).Get(ctx, "p1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

func TestPostgresDialectLeavesNullsZero(t *testing.T) {
	pg := newPostgresOverSqlite(t, pgRow{})
	ctx := context.Background()
	if err := pg.Table(pgRow{}).Insert(ctx, &pgRow{ID: "p2"}); err != nil {
		t.Fatal(err)
	}
	var got pgRow
	if err := pg.Table(pgRow{}).Get(ctx, "p2", &got); err != nil {
		t.Fatal(err)
	}
	// Home is a non-nil struct, so it encodes as {"city":"","zip":""} and
	// decodes to its zero value; everything else is SQL NULL.
	if !reflect.DeepEqual(got, pgRow{ID: "p2"}) {
		t.Fatalf("empty row read back as %+v", got)
	}
}

func TestPostgresDialectRejectsCorruptJSON(t *testing.T) {
	cases := []struct{ name, column string }{
		{"slice", "tags"},
		{"map pointer target", "labels"},
		{"struct", "home"},
		{"struct pointer", "work"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg := newPostgresOverSqlite(t, pgRow{})
			if _, err := pg.db.Exec("INSERT INTO pg_rows (id, "+tc.column+") VALUES ('bad', $1)", []byte("{oops")); err != nil {
				t.Fatal(err)
			}
			var got pgRow
			err := pg.Table(pgRow{}).Get(context.Background(), "bad", &got)
			if err == nil || !strings.Contains(err.Error(), "invalid character 'o'") {
				t.Fatalf("Get error = %v, want a JSON syntax error", err)
			}
		})
	}
}

func TestPostgresDialectAppliesScannedTimes(t *testing.T) {
	type timed struct {
		At    time.Time
		AtPtr *time.Time
	}
	when := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	d := postgresDialect{}
	var row timed
	v := reflect.ValueOf(&row).Elem()
	valField := fieldMeta{Name: "At", Type: reflect.TypeOf(time.Time{})}
	ptrField := fieldMeta{Name: "AtPtr", Type: reflect.TypeOf(&when)}

	if _, ok := d.MakeScanDest(valField).(*sql.NullTime); !ok {
		t.Fatalf("time.Time scans into %T, want *sql.NullTime", d.MakeScanDest(valField))
	}
	if _, ok := d.MakeScanDest(ptrField).(*sql.NullTime); !ok {
		t.Fatalf("*time.Time scans into %T, want *sql.NullTime", d.MakeScanDest(ptrField))
	}
	if err := d.ApplyScannedValue(v.Field(0), valField, &sql.NullTime{Time: when, Valid: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyScannedValue(v.Field(1), ptrField, &sql.NullTime{Time: when, Valid: true}); err != nil {
		t.Fatal(err)
	}
	if !row.At.Equal(when) || row.AtPtr == nil || !row.AtPtr.Equal(when) {
		t.Fatalf("scanned times = %v / %v, want %v", row.At, row.AtPtr, when)
	}

	var empty timed
	ev := reflect.ValueOf(&empty).Elem()
	if err := d.ApplyScannedValue(ev.Field(1), ptrField, &sql.NullTime{}); err != nil {
		t.Fatal(err)
	}
	if empty.AtPtr != nil {
		t.Fatalf("a NULL time left AtPtr = %v, want nil", empty.AtPtr)
	}

	// A time delivered as JSON bytes (a JSONB column holding a timestamp)
	// still decodes.
	var viaJSON timed
	jv := reflect.ValueOf(&viaJSON).Elem()
	data := []byte(`"2026-09-01T12:00:00Z"`)
	if err := d.ApplyScannedValue(jv.Field(0), valField, &data); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyScannedValue(jv.Field(1), ptrField, &data); err != nil {
		t.Fatal(err)
	}
	if !viaJSON.At.Equal(when) || viaJSON.AtPtr == nil || !viaJSON.AtPtr.Equal(when) {
		t.Fatalf("JSON times = %v / %v, want %v", viaJSON.At, viaJSON.AtPtr, when)
	}
	bad := []byte(`"noon"`)
	if err := d.ApplyScannedValue(jv.Field(0), valField, &bad); err == nil || !strings.Contains(err.Error(), `cannot parse "noon"`) {
		t.Fatalf("bad JSON time error = %v", err)
	}

	// time.Time encodes as itself for TIMESTAMPTZ, not as JSON.
	if got := d.PrepareValue(reflect.ValueOf(when), valField); got != when {
		t.Fatalf("PrepareValue(time) = %#v, want the time itself", got)
	}
	if got := d.PrepareValue(reflect.Value{}, valField); got != nil {
		t.Fatalf("PrepareValue(invalid) = %#v, want nil", got)
	}
}

func TestPostgresDialectPlaceholdersAreNumbered(t *testing.T) {
	if got := (postgresDialect{}).Placeholder(3); got != "$3" {
		t.Fatalf("Placeholder(3) = %q, want $3", got)
	}
}

func TestPostgresUserScopedTablesFilterByUser(t *testing.T) {
	pg := newPostgresOverSqlite(t, TestUserNoTime{})
	ctx := context.Background()
	alice := pg.ForUser("alice").Table(TestUserNoTime{})
	bob := pg.ForUser("bob").Table(TestUserNoTime{})
	if err := alice.Insert(ctx, &TestUserNoTime{ID: "a1", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	if err := bob.Insert(ctx, &TestUserNoTime{ID: "b1", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	rows, err := alice.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].(*TestUserNoTime).ID != "a1" || rows[0].(*TestUserNoTime).UserID != "alice" {
		t.Fatalf("alice sees %+v, want only a1", rows)
	}
	var got TestUserNoTime
	if err := alice.Get(ctx, "b1", &got); err != sql.ErrNoRows {
		t.Fatalf("alice.Get(bob's row) = %v, want sql.ErrNoRows", err)
	}
	if pg.ForUser("alice").Table(pgRow{}) != nil {
		t.Fatal("ForUser.Table for an unregistered model should be nil")
	}
	if pg.Table(pgRow{}) != nil {
		t.Fatal("Table for an unregistered model should be nil")
	}
}

// TestUserNoTime is TestUser without the time column, which SQLite cannot
// hand back as a TIMESTAMPTZ.
type TestUserNoTime struct {
	ID     string   `json:"id"`
	UserID string   `json:"user_id"`
	Name   string   `json:"name"`
	Age    int      `json:"age"`
	Active bool     `json:"active"`
	Tags   []string `json:"tags"`
}

func TestPostgresTransactionsCommitAndRollBack(t *testing.T) {
	pg := newPostgresOverSqlite(t, TestUserNoTime{})
	ctx := context.Background()

	err := pg.RunInTransaction(ctx, func(tx Database) error {
		if err := tx.AddTable(pgRow{}); err == nil || err.Error() != "cannot add tables within a transaction" {
			t.Errorf("tx.AddTable = %v", err)
		}
		if err := tx.Ping(ctx); err != nil {
			t.Errorf("tx.Ping = %v", err)
		}
		if tx.Table(pgRow{}) != nil || tx.ForUser("u").Table(pgRow{}) != nil {
			t.Error("tx tables for an unregistered model should be nil")
		}
		exec, backend, err := Raw(tx)
		if err != nil || backend != BackendPostgres {
			t.Errorf("Raw(tx) = %v, %v", backend, err)
		}
		if _, ok := exec.(*sql.Tx); !ok {
			t.Errorf("Raw(tx) executor = %T, want the *sql.Tx", exec)
		}
		return tx.RunInTransaction(ctx, func(inner Database) error {
			if err := inner.Table(TestUserNoTime{}).Insert(ctx, &TestUserNoTime{ID: "kept", Name: "K"}); err != nil {
				return err
			}
			if err := inner.ForUser("carol").Table(TestUserNoTime{}).Insert(ctx, &TestUserNoTime{ID: "carols"}); err != nil {
				return err
			}
			return inner.Close()
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	boom := sqlErr("boom")
	err = pg.RunInTransaction(ctx, func(tx Database) error {
		if err := tx.Table(TestUserNoTime{}).Insert(ctx, &TestUserNoTime{ID: "dropped"}); err != nil {
			return err
		}
		return boom
	})
	if err != boom {
		t.Fatalf("RunInTransaction returned %v, want the callback's error", err)
	}

	rows, err := pg.Table(TestUserNoTime{}).Query(ctx, QueryOpts{OrderBy: "id"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.(*TestUserNoTime).ID+"/"+r.(*TestUserNoTime).UserID)
	}
	if strings.Join(ids, ",") != "carols/carol,kept/" {
		t.Fatalf("rows after commit and rollback = %v", ids)
	}

	exec, backend, err := Raw(pg)
	if err != nil || backend != BackendPostgres || exec != pg.db {
		t.Fatalf("Raw(pg) = %v, %v, %v", exec, backend, err)
	}
	if err := pg.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pg.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pg.Ping(ctx); err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("Ping after Close = %v", err)
	}
	if err := pg.RunInTransaction(ctx, func(Database) error { return nil }); err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("RunInTransaction after Close = %v", err)
	}
}

type sqlErr string

func (e sqlErr) Error() string { return string(e) }

func TestNewPostgresDatabaseReportsUnreachableServer(t *testing.T) {
	_, err := NewPostgresDatabase("postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=2")
	if err == nil || !strings.Contains(err.Error(), "failed to ping postgres") {
		t.Fatalf("NewPostgresDatabase on a closed port = %v, want a ping failure", err)
	}
}

func TestRawRejectsUnknownDatabase(t *testing.T) {
	_, _, err := Raw(&mockDatabase{})
	if err == nil || err.Error() != "unsupported database type *gowild_data.mockDatabase" {
		t.Fatalf("Raw(mock) = %v", err)
	}
}

// kindRow has one field of each Go kind the schema mappers distinguish.
type kindRow struct {
	ID  string `json:"id"`
	I   int
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	U   uint
	U8  uint8
	U16 uint16
	U32 uint32
	U64 uint64
	F32 float32
	F64 float64
	B   bool
	BS  []byte
	S   []int
	M   map[string]int
	T   time.Time
	N   address
	P   *int64
	C   complex64
}

func TestCreateTableSQLPostgresMapsEveryKind(t *testing.T) {
	meta, err := getModelMeta(kindRow{})
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE TABLE IF NOT EXISTS kind_rows (
    id TEXT PRIMARY KEY,
    i INTEGER,
    i8 SMALLINT,
    i16 SMALLINT,
    i32 INTEGER,
    i64 BIGINT,
    u INTEGER,
    u8 SMALLINT,
    u16 SMALLINT,
    u32 INTEGER,
    u64 BIGINT,
    f32 REAL,
    f64 DOUBLE PRECISION,
    b BOOLEAN,
    b_s BYTEA,
    s JSONB,
    m JSONB,
    t TIMESTAMPTZ,
    n JSONB,
    p BIGINT,
    c TEXT
)`
	if got := meta.createTableSQLPostgres(); got != want {
		t.Fatalf("createTableSQLPostgres =\n%s\nwant\n%s", got, want)
	}
	wantLite := []string{"TEXT", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "INTEGER", "REAL", "REAL", "INTEGER", "TEXT", "TEXT", "TEXT", "TEXT", "TEXT", "INTEGER", "TEXT"}
	for i, f := range meta.Fields {
		if f.SQLType != wantLite[i] {
			t.Errorf("SQLite type of %s = %s, want %s", f.Name, f.SQLType, wantLite[i])
		}
	}
}
