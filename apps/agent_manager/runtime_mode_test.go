package main

import (
	"reflect"
	"testing"
)

func TestParseRuntimeFlags(t *testing.T) {
	mode, ctx, cleaned := parseRuntimeFlags("-mode worker -worker-context persistent -debug -no-sandbox")
	if mode != "worker" {
		t.Fatalf("mode = %q, want worker", mode)
	}
	if ctx != "persistent" {
		t.Fatalf("worker context = %q, want persistent", ctx)
	}
	want := []string{"-debug", "-no-sandbox"}
	if !reflect.DeepEqual(cleaned, want) {
		t.Fatalf("cleaned flags = %v, want %v", cleaned, want)
	}
}

