package log

import (
	"testing"

	"github.com/sirupsen/logrus"
)

// NewTestLogger builds a logger which outputs to the t.Log().
func NewTestLogger(tb testing.TB) *logrus.Logger {
	log := logrus.New()
	log.SetOutput(&testLogger{tb: tb})
	log.SetLevel(logrus.DebugLevel)
	return log
}

type testLogger struct {
	tb testing.TB
}

// Write implements io.Writer
func (l *testLogger) Write(p []byte) (n int, err error) {
	l.tb.Log(string(p))
	return len(p), nil
}
