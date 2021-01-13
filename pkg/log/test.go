// SPDX-License-Identifier: Apache-2.0
//
// Copyright 2021 Digital Ocean, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
