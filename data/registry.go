// Package gowild_data provides auto-discovery data access for Go packages.
//
// Library packages register their models at init() time, and applications
// call AddAllTables(db) to register all discovered tables.
package gowild_data

import (
	"fmt"
	"reflect"
	"sync"
)

// tableProvider is implemented by types that can add tables to a database.
type tableProvider interface {
	AddTables(db Database) error
}

// tableProviderFunc is a function type that implements tableProvider.
type tableProviderFunc func(db Database) error

func (f tableProviderFunc) AddTables(db Database) error {
	return f(db)
}

// registry holds registered table providers for auto-discovery.
type registry struct {
	mu        sync.RWMutex
	providers []tableProvider
}

// newRegistry creates a new empty registry.
func newRegistry() *registry {
	return &registry{
		providers: make([]tableProvider, 0),
	}
}

// register adds a table provider to the registry.
// Safe for concurrent use. Deduplicates comparable providers (e.g. pointer types).
func (r *registry) register(provider tableProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Deduplicate comparable providers (skip for functions which are not comparable)
	if reflect.TypeOf(provider).Comparable() {
		for _, p := range r.providers {
			if p == provider {
				return
			}
		}
	}
	r.providers = append(r.providers, provider)
}

// registerFunc registers a function as a table provider.
func (r *registry) registerFunc(fn func(db Database) error) {
	r.register(tableProviderFunc(fn))
}

// addAllTables calls AddTables on all registered providers.
func (r *registry) addAllTables(db Database) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i, provider := range r.providers {
		if err := provider.AddTables(db); err != nil {
			return fmt.Errorf("add tables provider %d (%T): %w", i, provider, err)
		}
	}
	return nil
}

// snapshotProviders returns a copy of the registered providers list.
func (r *registry) snapshotProviders() []tableProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]tableProvider, len(r.providers))
	copy(result, r.providers)
	return result
}

// clear removes all registered providers. Useful for testing.
func (r *registry) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = r.providers[:0]
}

// defaultRegistry is the global registry used by RegisterFunc() and AddAllTables().
var defaultRegistry = newRegistry()

// RegisterFunc registers a function as a table provider on the default registry.
//
// Example:
//
//	func init() {
//	    gowild_data.RegisterFunc(func(db gowild_data.Database) error {
//	        return db.AddTable(CalendarEvent{})
//	    })
//	}
func RegisterFunc(fn func(db Database) error) {
	defaultRegistry.registerFunc(fn)
}

// AddAllTables calls AddTables on all providers registered with the default registry.
func AddAllTables(db Database) error {
	return defaultRegistry.addAllTables(db)
}
