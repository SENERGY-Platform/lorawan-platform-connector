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

package main

import (
	"context"
	log_ "log"
	"os"
	"os/signal"
	"syscall"

	envldr "github.com/SENERGY-Platform/go-env-loader"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/chirpstack/chirpstack/api/go/v4/common"
)

func main() {
	// default config
	config := configuration.Config{
		ChirpstackUrl:      "ui.lora.senergy.infai.org",
		KeycloakUrl:        "https://auth.senergy.infai.org/auth",
		KeycloakClientId:   "lorawan-platform-connector",
		Host:               "http://connector",
		LogHandler:         "json",
		LogLevel:           "info",
		ServerPort:         8080,
		ServerPortCommands: 8081,
		ChirpstackProtectedUsers: []string{
			"admin",
		},
		KafkaBootstrap:          "kafka.kafka:9092",
		DeviceRepoUrl:           "http://api.device-repository:8080",
		PermissionsV2Url:        "http://permv2.permissions:8080",
		MemcachedUrl:            "memcached:11211",
		NotificationsUrl:        "http://api.notifier:5000",
		Regions:                 []int32{int32(common.Region_EU868)},
		ProtocolId:              "urn:infai:ses:protocol:9c956b2b-c34d-4083-9f8e-d9cc35246137",
		ProtocolDataSegmentId:   "urn:infai:ses:protocol-segment:7855de22-8643-4fd8-96e2-df8eb6f9948c",
		BatteryCharacteristicId: "urn:infai:ses:characteristic:062da5dd-085e-4b01-9e13-e331cb97dc0f",
		BatteryFunctionId:       "urn:infai:ses:measuring-function:00549f18-88b5-44c7-adb1-f558e8d53d1d",
		BatteryAspectId:         "urn:infai:ses:aspect:81936bcb-3625-4054-9f88-8934ee63d3ca",
		DeviceClassId:           "urn:infai:ses:device-class:ff64280a-58e6-4cf9-9a44-e70d3831a79d",
	}

	// load config from environment
	if err := envldr.LoadEnvUserParser(&config, nil, configuration.GetTypeParser(), nil); err != nil {
		log_.Fatal(err.Error())
		return
	}

	log.Init(config)

	ctx, cancel := context.WithCancel(context.Background())

	wg, err := pkg.Start(ctx, config)
	if err != nil {
		log.Logger.Error(err.Error())
		os.Exit(1)
	}

	go func() {
		shutdown := make(chan os.Signal, 1)
		signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
		sig := <-shutdown
		log.Logger.Info("received shutdown signal", "signal", sig)
		cancel()
	}()

	wg.Wait()
}
