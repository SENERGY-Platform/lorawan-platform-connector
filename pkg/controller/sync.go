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
	)
}

func (c *Controller) setupSync(ctx context.Context) error {
	err := c.setupEventSyncDevice(ctx)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := c.Sync()
				if err != nil {
					log.Logger.Error("unable to sync", attributes.ErrorKey, err)
					continue
				}
			}
		}
	}()
	return nil
}
