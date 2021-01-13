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

package provider

import "time"

// RetrySchedule defines a schedule for retrying on errors.
type RetrySchedule []time.Duration

// Equal returns true if o is equal to this retry schedule, false otherwise.
func (r RetrySchedule) Equal(o RetrySchedule) bool {
	if len(r) != len(o) {
		return false
	}
	for i, d := range r {
		if o[i] != d {
			return false
		}
	}
	return true
}

type retryError struct {
	error
	retrySchedule RetrySchedule
}

var (
	// RetryFast should be used for error which are likely to resolve themselves quickly.
	RetryFast = RetrySchedule{
		1 * time.Second, 1 * time.Second,
		5 * time.Second, 5 * time.Second,
		10 * time.Second, 10 * time.Second, 10 * time.Second,
		1 * time.Minute, 1 * time.Minute, 1 * time.Minute, 1 * time.Minute, 1 * time.Minute,
		5 * time.Minute,
	}
	// RetrySlow is used for errors which are unlikely to resolve themselves without other interactions.
	RetrySlow = RetrySchedule{
		1 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
	}
)

// Next returns the next retry for the schedule.
func (r RetrySchedule) Next(retries int) (int, time.Time) {
	return retries + 1, time.Now().Add(r.After(retries))
}

// After returns the duration we should wait before our next attempt.
func (r RetrySchedule) After(retries int) time.Duration {
	if retries >= len(r) {
		return r[len(r)-1]
	}
	return r[retries]
}

// NewRetryError creates a new retry error with the provided schedule. It's not recommended
// for use outside of provider, but is exported for testing via the mock provider interface.
func NewRetryError(err error, s RetrySchedule) error {
	return &retryError{
		error:         err,
		retrySchedule: s,
	}
}

// ErrorToRetrySchedule returns a retry schedule appropriate for the given error type,
// or a default slow retry if the error is unknown.
func ErrorToRetrySchedule(err error) RetrySchedule {
	rErr, ok := err.(*retryError)
	if !ok {
		return RetrySlow
	}
	return rErr.retrySchedule
}
