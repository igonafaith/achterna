package ctxkey

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type contextKey int

const requestIDKey contextKey = iota

// WithRequestID attaches a new random request ID to the context.
func WithRequestID(ctx context.Context) context.Context {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return context.WithValue(ctx, requestIDKey, hex.EncodeToString(b))
}

// RequestID retrieves the request ID from context, or empty string.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}
