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
)

func main() {
	// default config
	config := configuration.Config{
		ChirpstackUrl: "http://chirpstack:8080",
		KeycloakUrl:   "https://auth.senergy.infai.org",
		Host:          "http://api",
		LogHandler:    "text",
		LogLevel:      "info",
		ServerPort:    8080,
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
