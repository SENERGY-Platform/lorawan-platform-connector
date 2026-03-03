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
	"fmt"

	"time"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	platform_connector_lib "github.com/SENERGY-Platform/platform-connector-lib"
	platform_connector_lib_model "github.com/SENERGY-Platform/platform-connector-lib/model"
	"github.com/SENERGY-Platform/service-commons/pkg/cache"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
)

const timeKey = "lora/time"

func (c *Controller) HandleEvent(ctx context.Context, userId string, localDeviceId string, localServiceId string, payload any, ts time.Time, rxInfo []*gw.UplinkRxInfo, deviceProfileId string) error {
	token, err := c.connector.Security().GetCachedUserToken(userId, platform_connector_lib_model.RemoteInfo{})
	if err != nil {
		return err
	}
	device, err := c.connector.IotCache.GetDeviceByLocalId(token, localDeviceId)
	if err != nil {
		return err
	}
	deviceType, err := c.connector.IotCache.GetDeviceType(token, device.DeviceTypeId)
	if err != nil {
		return err
	}

	go func() {
		if len(rxInfo) == 0 {
			return
		}
		found := false
		var expiration time.Duration
		var err2 error
		for _, a := range device.Attributes {
			if a.Key == model.DeviceAttributeMessageMaxAgeKey {
				if d, err := time.ParseDuration(a.Value); err == nil {
					expiration = d
				}
				found = true
				break
			}
		}
		if !found {
			expiration, err2 = cache.Use(c.connector.IotCache.GetCache(), "lpc_device_profile_"+deviceProfileId, func() (time.Duration, error) {
				ctx, cf := context.WithTimeout(context.Background(), time.Second*10)
				defer cf()
				profile, err := c.chirpDeviceProfile.Get(ctx, &api.GetDeviceProfileRequest{
					Id: deviceProfileId,
				})
				if err != nil {
					return time.Hour, err
				}
				return time.Second * time.Duration(profile.DeviceProfile.UplinkInterval), nil
			}, nil, time.Minute)
		}
		if err2 != nil {
			log.Logger.Error("unable to get message max age from device profile, using default", attributes.ErrorKey, err2, "device_profile_id", deviceProfileId)
			expiration = time.Hour
		}
		value := ts.Format(time.RFC3339Nano)
		ctx, cf := context.WithTimeout(context.Background(), time.Second*10)
		defer cf()
		for _, rx := range rxInfo {
			err := c.rdb.Set(ctx, fmt.Sprintf(model.RedisKeyFmtGatewayDevice, rx.GatewayId, localDeviceId), value, expiration).Err()
			if err != nil {
				log.Logger.Error("unable to set device timestamp in redis", attributes.ErrorKey, err, "gateway_id", rx.GatewayId, "device_id", localDeviceId)
			}
		}
	}()

	event := platform_connector_lib.EventMsg{}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event[model.ProtocolSegmentData] = string(encoded)
	event[timeKey] = ts.Format(time.RFC3339Nano)

	var jsoned any
	err = json.Unmarshal(encoded, &jsoned)
	if err != nil {
		return err
	}

	serviceId, err := c.ensureSyncedService(deviceType, localServiceId, jsoned)
	if err != nil {
		return err
	}
	err = c.connector.HandleDeviceEventWithAuthToken(token, device.Id, serviceId, event, platform_connector_lib.SyncIdempotent)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) AnnotateDeviceJoined(ctx context.Context, userId string, localDeviceId string) error {
	token, err := c.connector.Security().GetCachedUserToken(userId, platform_connector_lib_model.RemoteInfo{})
	if err != nil {
		return err
	}
	device, err := c.connector.IotCache.GetDeviceByLocalId(token, localDeviceId)
	if err != nil {
		return err
	}
	model.UpsertDeviceAttribute(platform_connector_lib_model.Attribute{
		Key:    model.DeviceAttributeJoinedKey,
		Value:  "true",
		Origin: model.AttributeOrigin,
	}, &device)
	_, err = c.connector.IotCache.UpdateDevice(token, device)
	return err
}

func provideEventTime(msg platform_connector_lib.EventMsg) (platform_connector_lib.EventMsg, time.Time) {
	t, ok := msg[timeKey]
	if !ok {
		log.Logger.Error("no time in message", "message", fmt.Sprintf("%#v", msg))
		return msg, time.Now()
	}
	ts, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		log.Logger.Error("unable to parse time in message", attributes.ErrorKey, err, "message", fmt.Sprintf("%#v", msg))
		return msg, time.Now()
	}
	return msg, ts
}
