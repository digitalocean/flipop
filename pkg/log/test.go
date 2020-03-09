package log

import (
	"testing"

	"github.com/sirupsen/logrus"
)

// NewTestLogger builds a logger which outputs to the t.Log().
func NewTestLogger(tb testing.TB) *logrus.Logger {
	ll := logrus.New()
	ll.SetOutput(&testLogger{tb: tb})
	ll.SetLevel(logrus.DebugLevel)
	return ll
}

type testLogger struct {
	tb testing.TB
}

// Write implements io.Writer
func (l *testLogger) Write(p []byte) (n int, err error) {
	l.tb.Log(string(p))
	return len(p), nil
}
