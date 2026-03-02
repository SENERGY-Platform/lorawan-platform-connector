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
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	device_repo_model "github.com/SENERGY-Platform/device-repository/lib/model"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/chirpstack/chirpstack/api/go/v4/common"
	"github.com/chirpstack/chirpstack/api/go/v4/stream"
	"github.com/go-redis/redis/v8"
	"google.golang.org/protobuf/proto"
)

func (c *Controller) SyncDeviceProfile(ctx context.Context, listItem *api.DeviceProfileListItem, profile *api.DeviceProfile) error {
	c.jwtMux.RLock()
	deviceTypes, _, err, _ := c.deviceRepo.ListDeviceTypesV3("Bearer "+c.jwt.AccessToken, device_repo_model.DeviceTypeListOptions{
		AttributeKeys:   []string{model.DeviceTypeAttributeDeviceProfileIdKey},
		AttributeValues: []string{profile.Id},
	})
	c.jwtMux.RUnlock()
	if err != nil {
		return err
	}
	found := false
	for _, deviceType := range deviceTypes {
		for _, attribute := range deviceType.Attributes {
			if attribute.Key == model.DeviceTypeAttributeDeviceProfileIdKey && attribute.Value == profile.Id {
				found = true
				dt := c.prepareDeviceType(listItem, profile, &deviceType)
				if deviceTypeNeedsUpdate(&deviceType, &dt) {
					dt.Id = deviceType.Id
					c.jwtMux.RLock()
					_, err, _ = c.deviceRepo.SetDeviceType("Bearer "+c.jwt.AccessToken, dt, device_repo_model.DeviceTypeUpdateOptions{})
					c.jwtMux.RUnlock()
					if err != nil {
						return err
					}
				}
				break
			}
		}
	}
	if !found {
		dt := c.prepareDeviceType(listItem, profile, nil)
		c.jwtMux.RLock()
		_, err, _ := c.deviceRepo.SetDeviceType("Bearer "+c.jwt.AccessToken, dt, device_repo_model.DeviceTypeUpdateOptions{})
		c.jwtMux.RUnlock()
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) SyncAllDeviceProfiles() (err error) {
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cf()
	mux := sync.Mutex{}
	wg := sync.WaitGroup{}
	var limit uint32 = 1000
	var offset uint32 = 0
	for {
		resp, err2 := c.chirpDeviceProfile.List(ctx, &api.ListDeviceProfilesRequest{
			Limit:  limit,
			Offset: offset,
		})
		if err2 != nil {
			mux.Lock()
			err = errors.Join(err, err2)
			mux.Unlock()
			break
		}
		for _, listItem := range resp.Result {
			wg.Go(func() {
				if len(c.config.Regions) > 0 && !containsRegion(listItem, c.config.Regions) {
					return
				}
				profile, err2 := c.chirpDeviceProfile.Get(ctx, &api.GetDeviceProfileRequest{Id: listItem.Id})
				if err != nil {
					mux.Lock()
					err = errors.Join(err, err2)
					mux.Unlock()
					return
				}
				err2 = c.SyncDeviceProfile(ctx, listItem, profile.DeviceProfile)
				if err != nil {
					mux.Lock()
					err = errors.Join(err, err2)
					mux.Unlock()
					return
				}
			})
		}
		if uint32(len(resp.Result)) < limit {
			break
		}
		offset += limit
	}
	wg.Wait()
	return err
}

func (c *Controller) ensureSyncedService(dt models.DeviceType, localServiceId string, event any) (serviceId string, err error) {
	service := &models.Service{
		Id:          "",
		LocalId:     localServiceId,
		Interaction: models.EVENT,
		Name:        fmt.Sprintf("Event %s", localServiceId),
		ProtocolId:  c.config.ProtocolId,
		Outputs: []models.Content{{
			Serialization:     models.JSON,
			ProtocolSegmentId: c.config.ProtocolDataSegmentId,
		}},
	}
	var base *models.ContentVariable
	found := -1
	for i, s := range dt.Services {
		if s.LocalId == localServiceId && s.ProtocolId == c.config.ProtocolId && s.Interaction == models.EVENT {
			service.Id = s.Id
			if len(s.Outputs) > 0 {
				base = &s.Outputs[0].ContentVariable
				service.Outputs[0].Id = s.Outputs[0].Id
				service.Name = s.Name
				service.Description = s.Description
				service.Attributes = s.Attributes
				service.ServiceGroupKey = s.ServiceGroupKey
			}
			dt.Services[i] = *service
			found = i
			break
		}
	}
	cv := prepareContentVariable(&event, base)
	if cv == nil {
		return service.Id, nil
	}

	if base != nil && reflect.DeepEqual(*base, *cv) {
		return service.Id, nil
	}
	service.Outputs[0].ContentVariable = *cv
	if found == -1 {
		dt.Services = append(dt.Services, *service)
	} else {
		dt.Services[found] = *service
	}
	token, err := c.connector.Security().Access()
	if err != nil {
		return "", err
	}
	dt, err = c.connector.IotCache.UpdateDeviceType(token, dt)
	if err != nil {
		return "", err
	}
	for _, s := range dt.Services {
		if s.LocalId == localServiceId {
			return s.Id, nil
		}
	}
	return "", fmt.Errorf("service %s not found after update", localServiceId)
}

func (c *Controller) setupEventSyncDeviceProfile(ctx context.Context) error {
	rdb := redis.NewClient(&redis.Options{
		Addr: c.config.RedisUrl,
	})
	// use "$" to only read new entries that arrive after connecting
	lastID := "$"

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			resp, err := rdb.XRead(ctx, &redis.XReadArgs{
				Streams: []string{"api:stream:request", lastID},
				Count:   10,
				Block:   0,
			}).Result()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					return
				}
				log.Logger.Error("error reading from redis stream", attributes.ErrorKey, err)
				time.Sleep(1 * time.Second)
				continue
			}

			if len(resp) != 1 {
				log.Logger.Error("Exactly one stream response is expected")
				time.Sleep(1 * time.Second)
				continue
			}

			for _, msg := range resp[0].Messages {
				lastID = msg.ID

				if b, ok := msg.Values["request"].(string); ok {
					var pl stream.ApiRequestLog
					if err := proto.Unmarshal([]byte(b), &pl); err != nil {
						log.Logger.Error("unable to unmarshal api request log", attributes.ErrorKey, err)
						continue
					}

					if pl.Service == "api.DeviceProfileService" && (pl.Method == "Create" || pl.Method == "Update") {
						profileId, ok := pl.Metadata["device_profile_id"]
						if !ok {
							log.Logger.Error("device profile id not found in metadata")
							continue
						}
						log.Logger.Debug("API Event Stream: Device Profile Created or Updated", "device_profile", profileId)
						ctx2, cf := context.WithTimeout(ctx, 1*time.Minute)
						profile, err := c.chirpDeviceProfile.Get(ctx2, &api.GetDeviceProfileRequest{Id: profileId})
						if err != nil {
							log.Logger.Error("error getting device profile", attributes.ErrorKey, err)
							cf()
							continue
						}
						list, err := c.chirpDeviceProfile.List(ctx2, &api.ListDeviceProfilesRequest{Search: profile.DeviceProfile.Name, Limit: 10000})
						if err != nil {
							log.Logger.Error("error listing device profiles", attributes.ErrorKey, err)
							cf()
							continue
						}
						var listItem *api.DeviceProfileListItem
						for _, item := range list.Result {
							if item.Id == profileId {
								listItem = item
								break
							}
						}
						if listItem == nil {
							log.Logger.Error("device profile list item not found for profile", "profile_id", profileId)
							cf()
							continue
						}
						err = c.SyncDeviceProfile(ctx2, listItem, profile.DeviceProfile)
						if err != nil {
							cf()
							log.Logger.Error("error syncing device profile", attributes.ErrorKey, err)
							continue
						}
						cf()
						log.Logger.Debug("Device profile synced successfully", "device_profile", profileId)
					}
				}

			}
		}
	}()
	return nil
}

func containsRegion(profile *api.DeviceProfileListItem, i []int32) bool {
	return slices.Contains(i, int32(profile.Region))
}

func (c *Controller) prepareDeviceType(listItem *api.DeviceProfileListItem, profile *api.DeviceProfile, base *models.DeviceType) (deviceType models.DeviceType) {
	dtId := ""
	sId := ""
	oId := ""
	cvIdRoot := ""
	cvIdEPS := ""
	cvIdBLU := ""
	cvIdBL := ""
	if base != nil {
		dtId = base.Id
		for _, svc := range base.Services {
			if svc.LocalId == "status" {
				sId = svc.Id
				for _, content := range svc.Outputs {
					if content.ProtocolSegmentId == c.config.ProtocolDataSegmentId {
						oId = content.Id
						cvIdRoot = content.ContentVariable.Id
						for _, cv := range content.ContentVariable.SubContentVariables {
							if cv.Name == "external_power_source" {
								cvIdEPS = cv.Id
							} else if cv.Name == "battery_level_unavailable" {
								cvIdBLU = cv.Id
							} else if cv.Name == "battery_level" {
								cvIdBL = cv.Id
							}
						}
						break
					}
				}
				break
			}
		}
	}
	dt := models.DeviceType{
		Id:            dtId,
		Name:          fmt.Sprintf("%s %s (%s) v%s", listItem.VendorName, profile.Name, common.Region_name[int32(profile.Region)], profile.FirmwareVersion),
		Description:   profile.Description,
		DeviceClassId: c.config.DeviceClassId,
		Attributes: []models.Attribute{
			{
				Key:    model.DeviceTypeAttributeManagedByKey,
				Value:  model.DeviceTypeAttributeManagedByValue,
				Origin: model.DeviceAttributeOrigin,
			},
			{
				Key:    model.DeviceTypeAttributeDeviceProfileIdKey,
				Value:  profile.Id,
				Origin: model.DeviceAttributeOrigin,
			},
			{
				Key:    model.DeviceAttributeSupportsOTAAKey,
				Value:  strconv.FormatBool(profile.SupportsOtaa),
				Origin: model.DeviceAttributeOrigin,
			},
			{
				Key:    model.DeviceAttributeSupportsClassBKey,
				Value:  strconv.FormatBool(profile.SupportsClassB),
				Origin: model.DeviceAttributeOrigin,
			},
			{
				Key:    model.DeviceAttributeSupportsClassCKey,
				Value:  strconv.FormatBool(profile.SupportsClassC),
				Origin: model.DeviceAttributeOrigin,
			},
			{
				Key:    "last_message_max_age",
				Value:  fmt.Sprintf("%ds", profile.UplinkInterval),
				Origin: model.DeviceAttributeOrigin,
			},
		},
		Services: []models.Service{{
			Id:          sId,
			LocalId:     "status",
			Interaction: models.EVENT,
			Name:        "Get Battery Level",
			ProtocolId:  c.config.ProtocolId,
			Outputs: []models.Content{{
				Id:                oId,
				Serialization:     models.JSON,
				ProtocolSegmentId: c.config.ProtocolDataSegmentId,
				ContentVariable: models.ContentVariable{
					Id:   cvIdRoot,
					Name: "root",
					Type: models.Structure,
					SubContentVariables: []models.ContentVariable{
						{
							Id:   cvIdEPS,
							Name: "external_power_source",
							Type: models.Boolean,
						},
						{
							Id:   cvIdBLU,
							Name: "battery_level_unavailable",
							Type: models.Boolean,
						},
						{
							Id:               cvIdBL,
							Name:             "battery_level",
							Type:             models.Integer,
							CharacteristicId: c.config.BatteryCharacteristicId,
							AspectId:         c.config.BatteryAspectId,
							FunctionId:       c.config.BatteryFunctionId,
						},
					},
				},
			}},
		}},
	}
	if base != nil {
		for _, svc := range base.Services {
			if svc.LocalId != "status" {
				dt.Services = append(dt.Services, svc)
			}
		}
		dt.ServiceGroups = base.ServiceGroups
	}
	return dt
}

func deviceTypeNeedsUpdate(existing *models.DeviceType, new *models.DeviceType) bool {
	if existing == nil {
		return true
	}
	// check basics
	if existing.Name != new.Name || existing.Description != new.Description || existing.DeviceClassId != new.DeviceClassId {
		return true
	}

	// check attributes
	if attributeListNeedsUpdate(existing.Attributes, new.Attributes) {
		return true
	}

	// check services
	if len(new.Services) > len(existing.Services) {
		return true
	}
	for _, svc := range new.Services {
		found := false
		for _, existingSvc := range existing.Services {
			if svc.Name == existingSvc.Name && svc.LocalId == existingSvc.LocalId {
				found = true
				if svc.Description != existingSvc.Description || svc.Interaction != existingSvc.Interaction || svc.ProtocolId != existingSvc.ProtocolId || contentListNeedsUpdate(svc.Inputs, existingSvc.Inputs) || contentListNeedsUpdate(svc.Outputs, existingSvc.Outputs) || attributeListNeedsUpdate(svc.Attributes, existingSvc.Attributes) {
					return true
				}
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func attributeListNeedsUpdate(existing []models.Attribute, new []models.Attribute) bool {
	if len(new) > len(existing) {
		return true
	}
	for _, newAttr := range new {
		found := false
		for _, existingAttr := range existing {
			if newAttr.Key == existingAttr.Key {
				found = true
				if newAttr.Value != existingAttr.Value || newAttr.Origin != existingAttr.Origin {
					return true
				}
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func contentListNeedsUpdate(existing []models.Content, new []models.Content) bool {
	if len(new) > len(existing) {
		return true
	}
	for _, newContent := range new {
		found := false
		for _, existingContent := range existing {
			if newContent.ProtocolSegmentId == existingContent.ProtocolSegmentId {
				found = true
				if contentVariableNeedsUpdate(&existingContent.ContentVariable, &newContent.ContentVariable) {
					return true
				}
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func contentVariableNeedsUpdate(existing *models.ContentVariable, new *models.ContentVariable) bool {
	if existing == nil {
		return true
	}
	if existing.Name != new.Name || existing.Type != new.Type || existing.IsVoid != new.IsVoid || existing.CharacteristicId != new.CharacteristicId || existing.AspectId != new.AspectId || existing.FunctionId != new.FunctionId || existing.OmitEmpty != new.OmitEmpty || !reflect.DeepEqual(existing.Value, new.Value) || !reflect.DeepEqual(existing.SerializationOptions, new.SerializationOptions) || existing.UnitReference != new.UnitReference {
		return true
	}
	if len(new.SubContentVariables) > len(existing.SubContentVariables) {
		return true
	}
	for _, sub := range new.SubContentVariables {
		found := false
		for _, existingSub := range existing.SubContentVariables {
			if sub.Name == existingSub.Name && sub.Type == existingSub.Type {
				found = true
				if contentVariableNeedsUpdate(&existingSub, &sub) {
					return true
				}
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

func prepareContentVariable(val *any, base *models.ContentVariable) *models.ContentVariable {
	if val == nil {
		return nil
	}
	t := inferModelFromValue(*val)
	if t == nil {
		return nil
	}
	cv := &models.ContentVariable{
		Name: "root",
		Type: *t,
	}
	if base != nil {
		cv.Id = base.Id
		cv.IsVoid = base.IsVoid
		cv.OmitEmpty = base.OmitEmpty
		cv.Value = base.Value
		cv.CharacteristicId = base.CharacteristicId
		cv.AspectId = base.AspectId
		cv.FunctionId = base.FunctionId
		cv.OmitEmpty = base.OmitEmpty
		cv.SerializationOptions = base.SerializationOptions
		cv.UnitReference = base.UnitReference
	}

	if *t == models.Structure {
		m, ok := (*val).(map[string]any)
		if !ok {
			log.Logger.Warn("unable to cast value to map[string]any for structure content variable", "value", fmt.Sprintf("%v", *val))
			return cv
		}
		remainingBaseSubs := map[int]any{}
		if base != nil {
			for i := range base.SubContentVariables {
				remainingBaseSubs[i] = nil
			}
		}

		for key, value := range m {
			var baseSub *models.ContentVariable
			if base != nil {
				for i, sub := range base.SubContentVariables {
					if sub.Name == key {
						baseSub = &sub
						delete(remainingBaseSubs, i)
						break
					}
				}
			}
			sub := prepareContentVariable(&value, baseSub)
			if sub == nil {
				continue
			}
			sub.Name = key
			cv.SubContentVariables = append(cv.SubContentVariables, *sub)
		}
		if base != nil {
			for i := range remainingBaseSubs {
				// add remaining sub content variables from base that were not in the new value
				cv.SubContentVariables = append(cv.SubContentVariables, base.SubContentVariables[i])
			}

		}
	} else if *t == models.List {
		s, ok := (*val).([]any)
		if !ok {
			log.Logger.Warn("unable to cast value to []any for list content variable", "value", fmt.Sprintf("%v", *val))
			return cv
		}
		remainingBaseSubs := map[int]any{}
		if base != nil {
			for i := range base.SubContentVariables {
				remainingBaseSubs[i] = nil
			}
		}
		for i, v := range s {
			var baseSub *models.ContentVariable
			if base != nil {
				for i, sub := range base.SubContentVariables {
					if sub.Name == fmt.Sprintf("%d", i) {
						baseSub = &sub
						delete(remainingBaseSubs, i)
						break
					}
				}
			}
			sub := prepareContentVariable(&v, baseSub)
			if sub == nil {
				continue
			}
			sub.Name = fmt.Sprintf("%d", i)
			cv.SubContentVariables = append(cv.SubContentVariables, *sub)
		}
		if base != nil {
			// add remaining sub content variables from base that were not in the new value
			for i := range remainingBaseSubs {
				cv.SubContentVariables = append(cv.SubContentVariables, base.SubContentVariables[i])
			}
		}
	}
	return cv
}

func inferModelFromValue(value any) *models.Type {
	switch value.(type) {
	case map[string]any:
		s := models.Structure
		return &s
	case []any:
		s := models.List
		return &s
	case string:
		s := models.String
		return &s
	case int64, int, int16, int32, int8, uint32, uint64, uint16, uint8, uint:
		s := models.Integer
		return &s
	case float64, float32:
		s := models.Float
		return &s
	case bool:
		s := models.Boolean
		return &s
	default:
		log.Logger.Warn("unable to get models.Type from golang type", "golang_type", fmt.Sprintf("%v", value))
		return nil
	}
}
