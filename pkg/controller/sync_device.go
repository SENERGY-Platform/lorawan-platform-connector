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
	"fmt"
	"sync"
	"time"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	platform_connector_lib_model "github.com/SENERGY-Platform/platform-connector-lib/model"

	device_repo "github.com/SENERGY-Platform/device-repository/lib/model"

	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Controller) SyncDevice(ctx context.Context, platformDevice *platform_connector_lib_model.Device) error {
	chirpDevice, err := c.chirpDevice.Get(ctx, &api.GetDeviceRequest{
		DevEui: platformDevice.LocalId,
	})
	if err == nil && chirpDevice.Device.Name == platformDevice.Name { // TODO check device is activated (OTAA)
		return nil // nothing to do
	}
	newChirpDevice, activation, err := c.prepareChirpDevice(ctx, platformDevice)
	if err != nil {
		return err
	}

	if status.Code(err) == codes.NotFound {
		_, err = c.chirpDevice.Create(ctx, &api.CreateDeviceRequest{
			Device: newChirpDevice,
		})
	} else {
		_, err = c.chirpDevice.Update(ctx, &api.UpdateDeviceRequest{
			Device: newChirpDevice,
		})
	}
	if err != nil {
		return err
	}
	if chirpDevice == nil { // or needs to be activated
		_, err = c.chirpDevice.Activate(ctx, &api.ActivateDeviceRequest{
			DeviceActivation: activation,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) SyncAllDevices() (err error) {
	limit := 1000
	offset := 0
	cont := true
	wg := sync.WaitGroup{}
	mux := sync.Mutex{}
	for cont {
		c.jwtMux.RLock()
		deviceTypes, _, err, _ := c.deviceRepo.ListDeviceTypesV3("Bearer "+c.jwt.AccessToken, device_repo.DeviceTypeListOptions{
			AttributeKeys:   []string{model.DeviceTypeAttributeManagedByKey},
			AttributeValues: []string{model.DeviceTypeAttributeManagedByValue},
			Limit:           int64(limit),
			Offset:          int64(offset),
		})
		c.jwtMux.RUnlock()
		if err != nil {
			return err
		}
		for _, dt := range deviceTypes {
			found := false
			for _, a := range dt.Attributes {
				if a.Key == model.DeviceTypeAttributeManagedByKey && a.Value == model.DeviceTypeAttributeManagedByValue {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			wg.Go(func() {
				cont := true
				limit := 1000
				offset := 0
				for cont {
					c.jwtMux.RLock()
					devices, err2, _ := c.deviceRepo.ListDevices("Bearer "+c.jwt.AccessToken, device_repo.DeviceListOptions{
						DeviceTypeIds: []string{dt.Id},
						Limit:         int64(limit),
						Offset:        int64(offset),
					})
					c.jwtMux.RUnlock()
					if err2 != nil {
						mux.Lock()
						err = errors.Join(err, fmt.Errorf("unable to list devices for device type %s", dt.Id))
						mux.Unlock()
					}
					for _, d := range devices {
						wg.Go(func() {
							ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
							err2 := c.SyncDevice(ctx, &d)
							cf()
							if err2 != nil {
								mux.Lock()
								err = errors.Join(err, fmt.Errorf("unable to sync device %s", d.Id))
								mux.Unlock()
							}
						})
					}
					cont = len(devices) == limit
					offset += limit
				}

			})
		}
		cont = len(deviceTypes) == limit
		offset += limit
	}
	wg.Wait()
	return
}

func (c *Controller) prepareChirpDevice(ctx context.Context, platformDevice *platform_connector_lib_model.Device) (*api.Device, *api.DeviceActivation, error) {
	c.jwtMux.RLock()
	user, err := c.gocloakClient.GetUserByID(ctx, c.jwt.AccessToken, "master", platformDevice.OwnerId)
	c.jwtMux.RUnlock()
	if err != nil {
		return nil, nil, err
	}

	if user.Email == nil || *user.Email == "" {
		return nil, nil, fmt.Errorf("user has no email")
	}
	tenantId, err := c.getOrCreateChirpstackTenantId(ctx, *user.Email)
	if err != nil {
		return nil, nil, err
	}

	appId, err := c.getOrCreateChirpstackAppId(ctx, tenantId)
	if err != nil {
		return nil, nil, err
	}

	token, err := c.connector.Security().GetCachedUserToken(platformDevice.OwnerId, platform_connector_lib_model.RemoteInfo{})
	if err != nil {
		return nil, nil, err
	}
	deviceType, err := c.connector.IotCache.GetDeviceType(token, platformDevice.DeviceTypeId)
	if err != nil {
		return nil, nil, err
	}
	var deviceProvileId string
	for _, a := range deviceType.Attributes {
		if a.Key == model.DeviceTypeAttributeDeviceProfileIdKey {
			deviceProvileId = a.Value
			break
		}
	}
	if deviceProvileId == "" {
		return nil, nil, fmt.Errorf("deviceType has no deviceProfileId")
	}

	device := &api.Device{
		DevEui:          platformDevice.LocalId,
		Name:            platformDevice.Name,
		ApplicationId:   appId,
		DeviceProfileId: deviceProvileId,
		Description:     "Managed by lorawan-platform-connector",
	}

	activation := &api.DeviceActivation{
		DevEui: platformDevice.LocalId,
	}

	for _, a := range platformDevice.Attributes {
		switch a.Key {
		case model.DeviceAttributeDevAddr:
			activation.DevAddr = a.Value
		case model.DeviceAttributeAppSKey:
			activation.AppSKey = a.Value
		case model.DeviceAttributeNwkSEncKey:
			activation.NwkSEncKey = a.Value
		case model.DeviceAttributeSNwkSIntKey:
			activation.SNwkSIntKey = a.Value
		case model.DeviceAttributeFNwkSIntKey:
			activation.FNwkSIntKey = a.Value
		case model.DeviceAttributeJoinEuiKey:
			device.JoinEui = a.Value
		default:
			continue
		}
	}

	return device, activation, nil
}
