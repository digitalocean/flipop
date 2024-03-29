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
