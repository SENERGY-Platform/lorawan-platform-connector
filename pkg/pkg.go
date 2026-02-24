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

package pkg

import (
	"context"
	"sync"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/api"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
)

func Start(ctx context.Context, config configuration.Config) (wg *sync.WaitGroup, err error) {
	wg = &sync.WaitGroup{}
	controller, err := controller.New(config)
	if err != nil {
		log.Logger.Error("unable to initialize controller", attributes.ErrorKey, err)
	}
	if err != nil {
		return wg, err
	}
	err = api.Start(ctx, wg, config, controller)
	if err != nil {
		return wg, err
	}
	return
}
