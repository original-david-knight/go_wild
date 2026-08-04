package googleauth

import (
	"context"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/tasks/v1"
)

// The per-account service constructors. Each is a thin binding of the official
// client to one account's auto-refreshing token source — deliberately thin,
// because a consumer that needs to police what may be called (lifedash wraps
// these in a permission layer) has to be able to wrap exactly this seam.

// Gmail returns a Gmail service for an account.
func (r *Registry) Gmail(ctx context.Context, account string) (*gmail.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithTokenSource(source))
}

// Calendar returns a Calendar service for an account.
func (r *Registry) Calendar(ctx context.Context, account string) (*calendar.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return calendar.NewService(ctx, option.WithTokenSource(source))
}

// Tasks returns a Tasks service for an account.
func (r *Registry) Tasks(ctx context.Context, account string) (*tasks.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return tasks.NewService(ctx, option.WithTokenSource(source))
}
