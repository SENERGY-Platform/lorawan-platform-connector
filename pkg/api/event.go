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
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/chirpstack/chirpstack/api/go/v4/integration"
	"github.com/gin-gonic/gin"
)

// postEvent godoc
// @Summary      Event
// @Description  Event endpoint to be called from chirpstack
// @Accept       json
// @Accept       application/x-protobuf
// @Param        X-UserId header string true "Platform User ID"
// @Param        event query string true "Event Type ('up'/'join')"
// @Param        payload body []string true "requested values"
// @Success      200 {object} string "status message (or null)"
// @Failure      400
// @Failure      500
// @Router       /provision [POST]
func postEvent(controller *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodPost, model.EventPath, func(gc *gin.Context) {
		event := gc.Query("event")
		switch event {
		case "up":
			var up integration.UplinkEvent
			err := gc.ShouldBind(&up)
			if err != nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body")))
				return
			}
			deviceInfo := up.GetDeviceInfo()
			if deviceInfo == nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body")))
				return
			}
			log.Logger.Debug("Uplink received", "dev_eui", deviceInfo.DevEui, "payload", hex.EncodeToString(up.Data))
		case "join":
			var join integration.JoinEvent
			err := gc.ShouldBind(&join)
			if err != nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body")))
				return
			}
			deviceInfo := join.GetDeviceInfo()
			if deviceInfo == nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body")))
				return
			}
			log.Logger.Debug("Device joined", "dev_eui", deviceInfo.DevEui, "dev_addr", join.DevAddr)
		default:
			gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unknown event type "+event)))
			return
		}
	}
}
