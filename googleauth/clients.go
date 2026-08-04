package googleauth

import (
	"context"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/tasks/v1"
)

// The per-account service constructors. Each is a thin binding of the official
// client to one account's auto-refreshing token source — deliberately thin,
// because a consumer that needs to police what may be called (lifedash wraps
// these in a permission layer) has to be able to wrap exactly this seam.

// Gmail returns a Gmail service for an account. Extra options follow the
// token source, so a test can inject a recording client — note that
// option.WithHTTPClient takes precedence over every other option, including
// the token source, by the library's own contract.
func (r *Registry) Gmail(ctx context.Context, account string, opts ...option.ClientOption) (*gmail.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, withSource(source, opts)...)
}

// Calendar returns a Calendar service for an account.
func (r *Registry) Calendar(ctx context.Context, account string, opts ...option.ClientOption) (*calendar.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return calendar.NewService(ctx, withSource(source, opts)...)
}

// Tasks returns a Tasks service for an account.
func (r *Registry) Tasks(ctx context.Context, account string, opts ...option.ClientOption) (*tasks.Service, error) {
	source, err := r.TokenSource(ctx, account)
	if err != nil {
		return nil, err
	}
	return tasks.NewService(ctx, withSource(source, opts)...)
}

func withSource(source oauth2.TokenSource, opts []option.ClientOption) []option.ClientOption {
	return append([]option.ClientOption{option.WithTokenSource(source)}, opts...)
}
