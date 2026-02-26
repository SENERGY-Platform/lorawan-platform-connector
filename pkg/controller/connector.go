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
	"strconv"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	platform_connector_lib "github.com/SENERGY-Platform/platform-connector-lib"
)

func (c *Controller) initConnector(ctx context.Context, config configuration.Config) error {
	connector, err := platform_connector_lib.New(platform_connector_lib.Config{
		EventTimeProvider: provideEventTime,

		KafkaUrl:           config.KafkaBootstrap,
		KafkaGroupName:     "lorawan-platform-connector",
		FatalKafkaError:    true,
		Protocol:           "lorawan",
		KafkaResponseTopic: "response",

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
		return err
	}
	c.connector = connector
	c.connector.SetDeviceCommandHandler(c.HandleCommand)
	err = c.connector.Start(ctx, platform_connector_lib.SyncIdempotent)
	if err != nil {
		return err
	}
	return nil
}
