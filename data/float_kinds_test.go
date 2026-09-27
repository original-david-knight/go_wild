package gowild_data

import (
	"context"
	"reflect"
	"testing"
)

// floatRow holds every float kind, plain and pointer. The values are exact in
// float32, so a correct round trip compares equal.
type floatRow struct {
	ID     string   `json:"id"`
	UserID string   `json:"user_id"`
	F32    float32  `json:"f32"`
	F64    float64  `json:"f64"`
	PF32   *float32 `json:"pf32"`
	PF64   *float64 `json:"pf64"`
}

func (floatRow) TableName() string { return "float_rows" }

func sampleFloatRow() floatRow {
	pf32, pf64 := float32(-2.25), 4.125
	return floatRow{ID: "r1", F32: 1.5, F64: -3.75, PF32: &pf32, PF64: &pf64}
}

func TestSqliteRoundTripsEveryFloatKind(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(floatRow{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := sampleFloatRow()
	if err := db.Table(floatRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got floatRow
	if err := db.Table(floatRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

func TestPostgresDialectRoundTripsEveryFloatKind(t *testing.T) {
	pg := newPostgresOverSqlite(t, floatRow{})
	ctx := context.Background()
	in := sampleFloatRow()
	if err := pg.Table(floatRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got floatRow
	if err := pg.Table(floatRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

type (
	label string
	ratio float64
	flag  bool
	blob  []byte
)

// namedRow points at named types, whose kinds match the built-in ones but
// whose pointer types do not.
type namedRow struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	PL     *label `json:"pl"`
	PR     *ratio `json:"pr"`
	PF     *flag  `json:"pf"`
	PB     *blob  `json:"pb"`
}

func (namedRow) TableName() string { return "named_rows" }

func sampleNamedRow() namedRow {
	l, r, f, b := label("open"), ratio(0.5), flag(true), blob("raw")
	return namedRow{ID: "r1", PL: &l, PR: &r, PF: &f, PB: &b}
}

func TestSqliteRoundTripsPointersToNamedTypes(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(namedRow{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := sampleNamedRow()
	if err := db.Table(namedRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got namedRow
	if err := db.Table(namedRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

func TestPostgresDialectRoundTripsPointersToNamedTypes(t *testing.T) {
	pg := newPostgresOverSqlite(t, namedRow{})
	ctx := context.Background()
	in := sampleNamedRow()
	if err := pg.Table(namedRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got namedRow
	if err := pg.Table(namedRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}
