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
