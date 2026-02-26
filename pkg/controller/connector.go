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
	"strconv"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	platform_connector_lib "github.com/SENERGY-Platform/platform-connector-lib"
	platform_connector_lib_model "github.com/SENERGY-Platform/platform-connector-lib/model"
)

const joinedAttributeKey = "lora/joined"
const timeKey = "lora/time"

func (c *Controller) HandleEvent(ctx context.Context, userId string, localDeviceId string, localServiceId string, payload any, ts time.Time) error {
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

	for _, service := range deviceType.Services {
		if service.LocalId == localServiceId {
			event := platform_connector_lib.EventMsg{}
			encoded, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			event["data"] = string(encoded)
			event[timeKey] = ts.Format(time.RFC3339Nano)
			err = c.connector.HandleDeviceEventWithAuthToken(token, device.Id, service.Id, event, platform_connector_lib.SyncIdempotent)
			if err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("service %s not found", localServiceId)
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
	found := false
	for i := range device.Attributes {
		if device.Attributes[i].Key == joinedAttributeKey {
			device.Attributes[i].Value = "true"
		}
	}
	if !found {
		device.Attributes = append(device.Attributes, platform_connector_lib_model.Attribute{
			Key:   joinedAttributeKey,
			Value: "true",
		})
	}
	_, err = c.connector.IotCache.UpdateDevice(token, device)
	return err
}

func getConnector(ctx context.Context, config configuration.Config) (*platform_connector_lib.Connector, error) {
	connector, err := platform_connector_lib.New(platform_connector_lib.Config{
		EventTimeProvider: provideEventTime,

		KafkaUrl:        config.KafkaBootstrap,
		KafkaGroupName:  "lorawan-platform-connector",
		FatalKafkaError: true,
		Protocol:        "lorawan",

		DeviceManagerUrl: config.DeviceRepoUrl,
		DeviceRepoUrl:    config.DeviceRepoUrl,
		PermissionsV2Url: config.PermissionsV2Url,

		AuthClientId:             config.KeycloakClientId,
		AuthClientSecret:         config.KeycloakClientSecret,
		AuthExpirationTimeBuffer: 10,
		AuthEndpoint:             strings.TrimSuffix(config.KeycloakUrl, "/auth"),

		DeviceExpiration:     60,
		DeviceTypeExpiration: 60,
		TokenCacheExpiration: 60,
		IotCacheUrl:          []string{config.MemcachedUrl},
		TokenCacheUrl:        []string{config.MemcachedUrl},
		Debug:                config.LogLevel == "debug",

		Validate:                  true,
		ValidateAllowUnknownField: true,
		ValidateAllowMissingField: true,

		CharacteristicExpiration: 60,
		PartitionsNum:            1,
		ReplicationFactor:        2,

		PublishToPostgres: false,

		HttpCommandConsumerPort: strconv.Itoa(int(config.ServerPortCommands)),

		AsyncPgThreadMax:    1000,
		AsyncFlushMessages:  200,
		AsyncFlushFrequency: 500 * time.Millisecond,
		AsyncCompression:    sarama.CompressionSnappy,
		SyncCompression:     sarama.CompressionSnappy,

		KafkaConsumerMaxWait:  "1s",
		KafkaConsumerMinBytes: 1000,
		KafkaConsumerMaxBytes: 1000000,

		IotCacheTimeout:      "200ms",
		IotCacheMaxIdleConns: 100,

		NotificationUrl: config.NotificationsUrl,

		DeviceTypeTopic: "device-types",

		NotificationsIgnoreDuplicatesWithinS: 3600,
		NotificationUserOverwrite:            "",

		DeveloperNotificationUrl: "http://api.developer-notifications:8080",

		MutedUserNotificationTitles: []string{"Device-Message Format-Error", "Client-Error"},

		InitTopics: false,

		Logger: log.Logger,
	})
	if err != nil {
		return nil, err
	}
	err = connector.InitProducer(ctx, []platform_connector_lib.Qos{platform_connector_lib.SyncIdempotent})
	if err != nil {
		return nil, err
	}
	return connector, nil
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
