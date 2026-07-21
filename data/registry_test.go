package gowild_data

import (
	"context"
	"errors"
	"testing"
)

type mockDatabase struct {
	addTableCalls []any
}

func (m *mockDatabase) AddTable(model any) error {
	m.addTableCalls = append(m.addTableCalls, model)
	return nil
}

func (m *mockDatabase) Table(model any) TableDAO           { return nil }
func (m *mockDatabase) ForUser(userID string) UserDatabase { return nil }
func (m *mockDatabase) RunInTransaction(ctx context.Context, fn func(tx Database) error) error {
	return nil
}
func (m *mockDatabase) Close() error { return nil }

type testProvider struct {
	called bool
}

func (p *testProvider) AddTables(db Database) error {
	p.called = true
	return nil
}

func TestRegistry_Register(t *testing.T) {
	r := newRegistry()
	p := &testProvider{}

	r.register(p)

	if len(r.snapshotProviders()) != 1 {
		t.Errorf("expected 1 provider, got %d", len(r.snapshotProviders()))
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	r := newRegistry()
	p := &testProvider{}

	r.register(p)
	r.register(p)

	if len(r.snapshotProviders()) != 1 {
		t.Errorf("expected 1 provider (no duplicates), got %d", len(r.snapshotProviders()))
	}
}

func TestRegistry_RegisterFunc(t *testing.T) {
	r := newRegistry()
	called := false

	r.registerFunc(func(db Database) error {
		called = true
		return nil
	})

	if len(r.snapshotProviders()) != 1 {
		t.Errorf("expected 1 provider, got %d", len(r.snapshotProviders()))
	}

	// Verify it gets called
	if err := r.addAllTables(nil); err != nil {
		t.Fatalf("unexpected error adding tables: %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
}

func TestRegistry_AddAllTables(t *testing.T) {
	r := newRegistry()
	p1 := &testProvider{}
	p2 := &testProvider{}

	r.register(p1)
	r.register(p2)

	db := &mockDatabase{}
	if err := r.addAllTables(db); err != nil {
		t.Fatalf("unexpected error adding tables: %v", err)
	}

	if !p1.called || !p2.called {
		t.Error("expected all providers to be called")
	}
}

func TestRegistry_Clear(t *testing.T) {
	r := newRegistry()
	r.register(&testProvider{})
	r.register(&testProvider{})

	if len(r.snapshotProviders()) != 2 {
		t.Errorf("expected 2 providers, got %d", len(r.snapshotProviders()))
	}

	r.clear()

	if len(r.snapshotProviders()) != 0 {
		t.Errorf("expected 0 providers after clear, got %d", len(r.snapshotProviders()))
	}
}

func TestRegistry_RegisterMultipleFuncs(t *testing.T) {
	r := newRegistry()
	var calls []string

	r.registerFunc(func(db Database) error {
		calls = append(calls, "first")
		return nil
	})
	r.registerFunc(func(db Database) error {
		calls = append(calls, "second")
		return nil
	})
	r.registerFunc(func(db Database) error {
		calls = append(calls, "third")
		return nil
	})

	if len(r.snapshotProviders()) != 3 {
		t.Fatalf("expected 3 func providers, got %d", len(r.snapshotProviders()))
	}

	db := &mockDatabase{}
	if err := r.addAllTables(db); err != nil {
		t.Fatalf("unexpected error adding tables: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	if calls[0] != "first" || calls[1] != "second" || calls[2] != "third" {
		t.Errorf("unexpected call order: %v", calls)
	}
}

func TestRegistry_MixedProviders(t *testing.T) {
	r := newRegistry()
	p := &testProvider{}
	funcCalled := false

	r.register(p)
	r.registerFunc(func(db Database) error {
		funcCalled = true
		return nil
	})
	// Duplicate struct pointer should be deduped
	r.register(p)

	if len(r.snapshotProviders()) != 2 {
		t.Fatalf("expected 2 providers (struct deduped, func kept), got %d", len(r.snapshotProviders()))
	}

	db := &mockDatabase{}
	if err := r.addAllTables(db); err != nil {
		t.Fatalf("unexpected error adding tables: %v", err)
	}

	if !p.called {
		t.Error("expected struct provider to be called")
	}
	if !funcCalled {
		t.Error("expected func provider to be called")
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Clear default registry for clean test
	defaultRegistry.clear()

	p := &testProvider{}
	defaultRegistry.register(p)

	if len(defaultRegistry.snapshotProviders()) != 1 {
		t.Errorf("expected 1 provider in default registry, got %d", len(defaultRegistry.snapshotProviders()))
	}

	// Clean up
	defaultRegistry.clear()
}

func TestRegistry_AddAllTables_ProviderError(t *testing.T) {
	r := newRegistry()
	expectedErr := context.Canceled

	r.registerFunc(func(db Database) error {
		return expectedErr
	})

	err := r.addAllTables(&mockDatabase{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error to include %v, got %v", expectedErr, err)
	}
}
