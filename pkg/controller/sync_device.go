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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/Nerzal/gocloak/v13"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/platform-connector-lib/kafka"
	"github.com/SENERGY-Platform/service-commons/pkg/cache"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"

	device_repo "github.com/SENERGY-Platform/device-repository/lib/model"

	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Controller) SyncDevice(ctx context.Context, platformDevice *models.ExtendedDevice) error {
	name := platformDevice.DisplayName
	if name == "" {
		name = platformDevice.Name
	}
	newChirpDevice, newActivation, newKeys, err := c.prepareChirpDevice(ctx, platformDevice, name)
	if err != nil {
		return err
	}
	chirpDevice, getErr := c.chirpDevice.Get(ctx, &api.GetDeviceRequest{
		DevEui: platformDevice.LocalId,
	})
	if getErr != nil && status.Code(getErr) != codes.NotFound {
		return getErr
	}

	deviceProfile, err := c.chirpDeviceProfile.Get(ctx, &api.GetDeviceProfileRequest{
		Id: newChirpDevice.DeviceProfileId,
	})
	if err != nil {
		return err
	}

	deviceNeedsCreate := status.Code(getErr) == codes.NotFound
	var existingActivation *api.DeviceActivation
	var existingKeys *api.DeviceKeys
	deviceNeedsKeyCreation := false
	if !deviceNeedsCreate {
		activationResp, err := c.chirpDevice.GetActivation(ctx, &api.GetDeviceActivationRequest{
			DevEui: platformDevice.LocalId,
		})
		if status.Code(err) == codes.NotFound || activationResp == nil || activationResp.DeviceActivation == nil {
			deviceNeedsKeyCreation = true
		} else if err != nil {
			return err
		}
		if activationResp != nil {
			existingActivation = activationResp.DeviceActivation
		}
		keysResp, err := c.chirpDevice.GetKeys(ctx, &api.GetDeviceKeysRequest{
			DevEui: platformDevice.LocalId,
		})
		if err != nil && status.Code(err) != codes.NotFound {
			return err
		}
		if keysResp != nil {
			existingKeys = keysResp.DeviceKeys
		}
	}

	deviceNeedsUpdate := !deviceNeedsCreate && chirpDevice.Device.Name != name
	deviceNeedsKeyUpdate := deviceNeedsCreate || (deviceProfile.DeviceProfile.SupportsOtaa && (existingKeys == nil || !reflect.DeepEqual(existingKeys, newKeys)))
	deviceNeedsActivation := deviceNeedsCreate || (newActivation != nil && !reflect.DeepEqual(existingActivation, newActivation))

	if !deviceNeedsActivation && !deviceNeedsCreate && !deviceNeedsUpdate && !deviceNeedsKeyUpdate {
		return nil // nothing to do
	}

	if deviceNeedsCreate {
		_, err = c.chirpDevice.Create(ctx, &api.CreateDeviceRequest{
			Device: newChirpDevice,
		})
	} else if deviceNeedsUpdate {
		_, err = c.chirpDevice.Update(ctx, &api.UpdateDeviceRequest{
			Device: newChirpDevice,
		})
	}
	if err != nil {
		return err
	}
	if deviceNeedsKeyCreation {
		_, err = c.chirpDevice.CreateKeys(ctx, &api.CreateDeviceKeysRequest{
			DeviceKeys: newKeys,
		})
		if err != nil {
			return err
		}
	} else if deviceNeedsKeyUpdate {
		_, err = c.chirpDevice.UpdateKeys(ctx, &api.UpdateDeviceKeysRequest{
			DeviceKeys: newKeys,
		})
		if err != nil {
			return err
		}
	}
	if deviceNeedsActivation {
		_, err = c.chirpDevice.Activate(ctx, &api.ActivateDeviceRequest{
			DeviceActivation: newActivation,
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
			if !deviceTypeManagedByLorawanPlatformConnector(dt) {
				continue
			}
			wg.Go(func() {
				cont := true
				limit := 1000
				offset := 0
				for cont {
					c.jwtMux.RLock()
					devices, _, err2, _ := c.deviceRepo.ListExtendedDevices("Bearer "+c.jwt.AccessToken, device_repo.ExtendedDeviceListOptions{
						DeviceTypeIds: []string{dt.Id},
						Limit:         int64(limit),
						Offset:        int64(offset),
						FullDt:        true,
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

func (c *Controller) DeleteOutdatedDevices() error {
	limit := 1000
	offset := 0
	cont := true
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cf()
	c.jwtMux.RLock()
	kcUsers, err := c.gocloakClient.GetUsers(ctx, c.jwt.AccessToken, "master", gocloak.GetUsersParams{})
	if err != nil {
		return err
	}
	c.jwtMux.RUnlock()
	wg := sync.WaitGroup{}
	mux := sync.Mutex{}
	var sErr error
	for cont {
		tenants, err := c.chirpTenant.List(ctx, &api.ListTenantsRequest{
			Limit:  uint32(limit),
			Offset: uint32(offset),
		})
		if err != nil {
			return err
		}
		cont = len(tenants.Result) == limit
		offset += limit

		for _, tenant := range tenants.Result {
			limit := 1000
			offset := 0
			cont := true
			ownerId := ""
			for _, user := range kcUsers {
				if user.Email != nil && *user.Email == tenant.Name {
					ownerId = *user.ID
				}
			}
			if ownerId == "" {
				log.Logger.Warn("Could not get owner from tenant", "tenant_name", tenant.Name)
				continue
			}
			wg.Go(func() {
				for cont {
					apps, err := c.chirpApp.List(ctx, &api.ListApplicationsRequest{
						Limit:    uint32(limit),
						Offset:   uint32(offset),
						TenantId: tenant.Id,
					})
					if err != nil {
						mux.Lock()
						defer mux.Unlock()
						sErr = errors.Join(sErr, err)
						return
					}
					cont = len(apps.Result) == limit
					offset += limit

					for _, application := range apps.Result {
						limit := 100
						offset := 0
						cont := true
						for cont {
							chirpDevices, err := c.chirpDevice.List(ctx, &api.ListDevicesRequest{
								Limit:         uint32(limit),
								Offset:        uint32(offset),
								ApplicationId: application.Id,
							})
							if err != nil {
								mux.Lock()
								defer mux.Unlock()
								sErr = errors.Join(sErr, err)
								return
							}
							cont = len(chirpDevices.Result) == limit
							if len(chirpDevices.Result) == 0 {
								continue
							}
							offset += limit
							localIds := []string{}
							for _, d := range chirpDevices.Result {
								localIds = append(localIds, d.DevEui)
							}
							c.jwtMux.RLock()
							platformDevices, err, _ := c.deviceRepo.ListDevices("Bearer "+c.jwt.AccessToken, device_repo.DeviceListOptions{
								LocalIds: localIds,
								Owner:    ownerId,
							})
							c.jwtMux.RUnlock()
							if err != nil {
								mux.Lock()
								defer mux.Unlock()
								sErr = errors.Join(sErr, err)
								return
							}
							if len(platformDevices) == len(chirpDevices.Result) {
								continue
							}
							for _, localId := range localIds {
								if slices.ContainsFunc(platformDevices, func(platformDevice models.Device) bool {
									return platformDevice.LocalId == localId
								}) {
									continue
								}
								// misses in platform devices --> delete
								_, err := c.chirpDevice.Delete(ctx, &api.DeleteDeviceRequest{
									DevEui: localId,
								})
								if err != nil && status.Code(err) != codes.NotFound {
									mux.Lock()
									defer mux.Unlock()
									sErr = errors.Join(sErr, err)
									return
								}
							}
						}
					}
				}
			})
		}
	}
	wg.Wait()
	return nil
}

func (c *Controller) setupEventSyncDevice(ctx context.Context) error {
	if c.config.KafkaBootstrap == "" {
		log.Logger.Warn("unable to setup kafka event sync: no kafka bootstrap defined")
		return nil
	}
	return kafka.NewConsumer(ctx, kafka.ConsumerConfig{
		KafkaUrl:         c.config.KafkaBootstrap,
		GroupId:          "lorawan-platform-connector",
		Topic:            "devices",
		MaxWait:          time.Second,
		MinBytes:         1000,
		MaxBytes:         1000000,
		InitTopic:        false,
		AllowOldMessages: false,
		Logger:           log.Logger,
	}, func(_ string, msg []byte, _ time.Time) error {
		var command model.DeviceCommand
		err := json.Unmarshal(msg, &command)
		if err != nil {
			return err
		}
		switch command.Command {
		case model.RightsCommand:
			return nil
		case model.PutCommand:
			c.jwtMux.RLock()
			devices, _, err, _ := c.deviceRepo.ListExtendedDevices("Bearer "+c.jwt.AccessToken, device_repo.ExtendedDeviceListOptions{
				Ids:    []string{command.Device.Id},
				FullDt: true,
			})
			c.jwtMux.RUnlock()
			if err != nil {
				return err
			}
			if len(devices) != 1 {
				return fmt.Errorf("got unexpected list of devices, expected length to be 1", "device_id", command.Device.Id)
			}
			if !deviceTypeManagedByLorawanPlatformConnector(*devices[0].DeviceType) {
				return nil
			}
			ctx2, cf := context.WithTimeout(ctx, 10*time.Second)
			defer cf()
			return c.SyncDevice(ctx2, &devices[0])
		case model.DeleteCommand:
			ctx2, cf := context.WithTimeout(ctx, 10*time.Second)
			defer cf()
			_, err := c.chirpDevice.Delete(ctx2, &api.DeleteDeviceRequest{
				DevEui: command.Device.LocalId,
			})
			if err != nil && status.Code(err) != codes.NotFound {
				return err
			}
			return nil
		default:
			log.Logger.Warn("unhandeled command on device kafka topic", "command", command.Command)
			return nil
		}
	}, func(err error) {
		log.Logger.Error("kafka EventSyncDevice error", attributes.ErrorKey, err)
	})
}

func (c *Controller) prepareChirpDevice(ctx context.Context, platformDevice *models.ExtendedDevice, name string) (*api.Device, *api.DeviceActivation, *api.DeviceKeys, error) {
	user, err := cache.Use(c.connector.IotCache.GetCache(), "lpc_user_"+platformDevice.OwnerId, func() (gocloak.User, error) {
		c.jwtMux.RLock()
		user, err := c.gocloakClient.GetUserByID(ctx, c.jwt.AccessToken, "master", platformDevice.OwnerId)
		c.jwtMux.RUnlock()
		if err != nil {
			return gocloak.User{}, err
		}
		if user == nil {
			return gocloak.User{}, fmt.Errorf("gocloak returned nil user")
		}
		return *user, err
	}, nil, time.Minute)
	if err != nil {
		return nil, nil, nil, err
	}

	if user.Email == nil || *user.Email == "" {
		return nil, nil, nil, fmt.Errorf("user has no email")
	}
	tenantId, err := c.getOrCreateChirpstackTenantId(ctx, *user.Email)
	if err != nil {
		return nil, nil, nil, err
	}

	appId, err := c.getOrCreateChirpstackAppId(ctx, tenantId)
	if err != nil {
		return nil, nil, nil, err
	}

	var deviceProvileId string
	for _, a := range platformDevice.DeviceType.Attributes {
		if a.Key == model.DeviceTypeAttributeDeviceProfileIdKey {
			deviceProvileId = a.Value
			break
		}
	}
	if deviceProvileId == "" {
		return nil, nil, nil, fmt.Errorf("deviceType has no deviceProfileId")
	}

	device := &api.Device{
		DevEui:          platformDevice.LocalId,
		Name:            name,
		ApplicationId:   appId,
		DeviceProfileId: deviceProvileId,
		Description:     "Managed by lorawan-platform-connector",
	}

	activation := &api.DeviceActivation{
		DevEui: platformDevice.LocalId,
	}

	keys := &api.DeviceKeys{
		DevEui:    platformDevice.LocalId,
		NwkKey:    "00000000000000000000000000000000",
		AppKey:    "00000000000000000000000000000000",
		GenAppKey: "00000000000000000000000000000000",
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
		case model.DeviceAttributeAppKey:
			keys.AppKey = a.Value
		case model.DeviceAttributeGenAppKey:
			keys.GenAppKey = a.Value
		case model.DeviceAttributeNwkKey:
			keys.NwkKey = a.Value
		default:
			continue
		}
	}

	if activation.DevAddr == "" && activation.AppSKey == "" && activation.NwkSEncKey == "" && activation.SNwkSIntKey == "" && activation.FNwkSIntKey == "" && activation.FCntUp == 0 && activation.NFCntDown == 0 && activation.AFCntDown == 0 {
		activation = nil
	}

	return device, activation, keys, nil
}

func deviceTypeManagedByLorawanPlatformConnector(dt models.DeviceType) bool {
	for _, a := range dt.Attributes {
		if a.Key == model.DeviceTypeAttributeManagedByKey && a.Value == model.DeviceTypeAttributeManagedByValue {
			return true
		}
	}
	return false
}
