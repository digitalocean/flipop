package log

import (
	"context"

	"github.com/sirupsen/logrus"
)

type (
	logKey struct{}
)

// AddToContext clones a child context with the provided logger stored as a value.
func AddToContext(ctx context.Context, logger logrus.FieldLogger) context.Context {
	return context.WithValue(ctx, logKey{}, logger)
}

// FromContext extracts a logrus logger from the provided context, or creates
// a new one if one is not available.
func FromContext(ctx context.Context) logrus.FieldLogger {
	log := ctx.Value(logKey{})

	if log == nil {
		return logrus.WithContext(ctx)
	}

	return log.(*logrus.Entry)
}
