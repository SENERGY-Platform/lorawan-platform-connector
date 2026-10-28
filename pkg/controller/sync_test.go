/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controller

import (
	"context"
	"testing"
	"time"
)

// the hourly sync never ran, because setupSync stopped the ticker when it
// returned, right after starting the goroutine that waited on it
func TestRunEveryKeepsRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEvery(ctx, 10*time.Millisecond, func() {
			select {
			case calls <- struct{}{}:
			case <-ctx.Done(): //a tick pending at cancel must not block on the send
			}
		})
	}()
	for i := range 3 {
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatal("no call", i)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runEvery did not return after ctx was done")
	}
}
