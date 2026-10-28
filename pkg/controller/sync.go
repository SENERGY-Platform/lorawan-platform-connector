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
	"errors"
	"time"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
)

func (c *Controller) Sync() (err error) {
	return errors.Join(
		c.ProvisionAllUsers(),
		c.DeleteOutdatedUsers(),
		c.SyncAllDevices(),
		c.DeleteOutdatedDevices(),
		c.SyncAllDeviceProfiles(),
		c.SyncAllGateways(),
		c.DeleteOutdatedGateways(),
	)
}

func (c *Controller) setupSync(ctx context.Context) error {
	err := c.setupEventSyncDevice(ctx)
	if err != nil {
		return err
	}
	err = c.setupEventSyncGateway(ctx)
	if err != nil {
		return err
	}
	err = c.setupEventSyncDeviceProfile(ctx)
	if err != nil {
		return err
	}
	go runEvery(ctx, time.Hour, func() {
		err := c.Sync()
		if err != nil {
			log.Logger.Error("unable to sync", attributes.ErrorKey, err)
		}
	})
	return nil
}

// runEvery calls f every interval until ctx is done. The ticker belongs to the
// goroutine that reads it: stopped by the function that only started the
// goroutine, it never fired, and the hourly sync never ran.
func runEvery(ctx context.Context, interval time.Duration, f func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f()
		}
	}
}
