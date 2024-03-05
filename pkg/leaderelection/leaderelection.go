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

package leaderelection

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// LeaderElection runs the "run" command when it is the leader, and terminates it when it is not.
func LeaderElection(
	ctx context.Context,
	log logrus.FieldLogger,
	namespace string,
	resourceName string,
	client kubernetes.Interface,
	runs ...func(ctx context.Context),
) error {
	var childCtx context.Context
	var childCancel context.CancelFunc
	var wg sync.WaitGroup
	var childLock sync.Mutex

	// leaderCtx doesn't descend from ctx explicitly. It won't be canceled until the child
	// is finished (or until we're certain it will never run). By waiting for children to
	// finish, we can safely use ReleaseOnCancel=true.
	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	defer leaderCancel()
	go func() {
		<-ctx.Done()
		log.Debug("leaderelection canceled context; waiting for children")
		childLock.Lock()
		if childCtx != nil {
			childCancel()
			wg.Wait()
		}
		childLock.Unlock()
		leaderCancel()
	}()

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%s_%s", hostname, string(uuid.NewUUID()))

	rl, err := resourcelock.New(
		resourcelock.LeasesResourceLock,
		namespace,
		resourceName,
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: nil, // eventrecorder is optional, though we may want it sometime
		})
	if err != nil {
		return err
	}

	l, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: rl,
		// durations are recommendations from:
		// https://godoc.org/k8s.io/client-go/tools/leaderelection#LeaderElectionConfig
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(_ context.Context) {
				// OnStartedLeading is spawned in a goroutine. We should only start if our parent
				// will be around to wait for completion.
				childLock.Lock()
				defer childLock.Unlock()
				if ctx.Err() != nil {
					return
				}
				// If the parent context hasn't been canceled, we can be sure they haven't executed
				// wg.Wait() yet, and it's safe to start.
				childCtx, childCancel = context.WithCancel(ctx)
				wg.Add(len(runs))
				for _, run := range runs {
					run := run
					go func() {
						defer wg.Done()
						run(childCtx)
					}()
				}
			},
			OnStoppedLeading: func() {
				childLock.Lock()
				defer childLock.Unlock()
				if childCtx == nil {
					return // ctx must have been canceled before OnStartedLeading launched a child.
				}
				childCancel()
				childCtx = nil
				wg.Wait()
			},
		},
		ReleaseOnCancel: true,
	})
	if err != nil {
		return err
	}
	l.Run(leaderCtx)
	return nil
}
