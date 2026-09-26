package gowild_data

import (
	"context"
	"reflect"
	"testing"
)

// intRow holds every integer kind, plain and pointer, so both dialects must
// read each one back as written.
type intRow struct {
	ID     string  `json:"id"`
	UserID string  `json:"user_id"`
	I      int     `json:"i"`
	I8     int8    `json:"i8"`
	I16    int16   `json:"i16"`
	I32    int32   `json:"i32"`
	I64    int64   `json:"i64"`
	U      uint    `json:"u"`
	U8     uint8   `json:"u8"`
	U16    uint16  `json:"u16"`
	U32    uint32  `json:"u32"`
	U64    uint64  `json:"u64"`
	PI     *int    `json:"pi"`
	PI8    *int8   `json:"pi8"`
	PI16   *int16  `json:"pi16"`
	PI32   *int32  `json:"pi32"`
	PI64   *int64  `json:"pi64"`
	PU     *uint   `json:"pu"`
	PU8    *uint8  `json:"pu8"`
	PU16   *uint16 `json:"pu16"`
	PU32   *uint32 `json:"pu32"`
	PU64   *uint64 `json:"pu64"`
}

func (intRow) TableName() string { return "int_rows" }

func sampleIntRow() intRow {
	pi, pi8, pi16, pi32, pi64 := -1, int8(-2), int16(-3), int32(-4), int64(-5)
	pu, pu8, pu16, pu32, pu64 := uint(6), uint8(7), uint16(8), uint32(9), uint64(10)
	return intRow{
		ID: "r1",
		I:  -11, I8: -12, I16: -13, I32: -14, I64: -15,
		U: 16, U8: 17, U16: 18, U32: 19, U64: 20,
		PI: &pi, PI8: &pi8, PI16: &pi16, PI32: &pi32, PI64: &pi64,
		PU: &pu, PU8: &pu8, PU16: &pu16, PU32: &pu32, PU64: &pu64,
	}
}

func TestSqliteRoundTripsEveryIntegerKind(t *testing.T) {
	db, err := NewSqliteDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AddTable(intRow{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := sampleIntRow()
	if err := db.Table(intRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got intRow
	if err := db.Table(intRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}

func TestPostgresDialectRoundTripsEveryIntegerKind(t *testing.T) {
	pg := newPostgresOverSqlite(t, intRow{})
	ctx := context.Background()
	in := sampleIntRow()
	if err := pg.Table(intRow{}).Insert(ctx, &in); err != nil {
		t.Fatal(err)
	}
	var got intRow
	if err := pg.Table(intRow{}).Get(ctx, "r1", &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, in)
	}
}
