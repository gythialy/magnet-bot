package server

import "context"

// initDataUserKey is the context key for the authenticated Mini App user. It
// is an unexported, zero-size type on purpose: no other package can construct
// the same key, so a value stored under it cannot collide with, or be
// overridden by, context data written elsewhere in the request path.
type initDataUserKey struct{}

// withInitDataUser attaches the authenticated identity to a request context.
// It is unexported because only the middleware may establish identity:
// handlers are expected to read it through UserFromContext.
func withInitDataUser(ctx context.Context, user InitDataUser) context.Context {
	return context.WithValue(ctx, initDataUserKey{}, user)
}

// UserFromContext returns the identity injected by NewInitDataMiddleware. The
// boolean is false when the request did not pass the middleware (or when a
// different, unrelated context is inspected), which lets handlers answer 401
// instead of silently operating on a zero-valued id.
func UserFromContext(ctx context.Context) (InitDataUser, bool) {
	// context.Context is an interface; calling Value on a nil one panics, and
	// a handler that passes a nil context is not worth crashing the process
	// over when "no user" is a meaningful answer.
	if ctx == nil {
		return InitDataUser{}, false
	}

	user, ok := ctx.Value(initDataUserKey{}).(InitDataUser)
	return user, ok
}
