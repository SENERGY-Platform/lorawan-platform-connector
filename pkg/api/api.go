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

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	gin_mw "github.com/SENERGY-Platform/gin-middleware"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
)

const healthCheckPath = "/health-check"

var routes = gin_mw.Routes[*controller.Controller]{
	getHealthCheck,
	getSwaggerDoc,
	postProvision,
	postEvent,
}

// Start godoc
// @title LoRaWAN Platform Connector API
// @license.name Apache-2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath /
func Start(ctx context.Context, wg *sync.WaitGroup, config configuration.Config, controller *controller.Controller) error {
	gin.SetMode(gin.ReleaseMode)
	httpHandler := gin.New()
	httpHandler.Use(
		gin_mw.StructLoggerHandlerWithDefaultGenerators(
			log.Logger.With(attributes.LogRecordTypeKey, attributes.HttpAccessLogRecordTypeVal),
			attributes.Provider,
			[]string{healthCheckPath},
			nil,
		),
		requestid.New(requestid.WithCustomHeaderStrKey("X-Request-ID")),
		gin_mw.ErrorHandler(model.GetStatusCode, ", "),
		gin_mw.StructRecoveryHandler(log.Logger, gin_mw.DefaultRecoveryFunc),
	)
	// httpHandler.UseRawPath = true
	rg := httpHandler.Group("")
	_, err := routes.Set(controller, rg)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:    ":" + strconv.FormatUint(uint64(config.ServerPort), 10),
		Handler: httpHandler}

	wg.Go(func() {
		log.Logger.Info("starting http server")
		if err = httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Logger.Error("starting server failed", attributes.ErrorKey, err)
		}
	})

	wg.Go(func() {
		<-ctx.Done()
		log.Logger.Info("stopping http server")
		ctxWt, cf2 := context.WithTimeout(context.Background(), time.Second*5)
		defer cf2()
		if err := httpServer.Shutdown(ctxWt); err != nil {
			log.Logger.Error("stopping server failed", attributes.ErrorKey, err)
		} else {
			log.Logger.Info("http server stopped")
		}
	})

	return nil
}
