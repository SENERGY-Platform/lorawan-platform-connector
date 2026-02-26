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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

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
// @Accept       application/octet-stream
// @Param        X-UserId header string true "Platform User ID"
// @Param        event query string true "Event Type ('up'/'join'/'status')"
// @Param        payload body []string true "requested values"
// @Success      200 {object} string "status message (or null)"
// @Failure      400
// @Failure      500
// @Router       /event [POST]
func postEvent(controller *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodPost, model.EventPath, func(gc *gin.Context) {
		userId := gc.GetHeader("X-UserId")
		if userId == "" {
			gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("missing header X-UserId")))
			return
		}
		event := gc.Query("event")
		switch event {
		case "up":
			var up integration.UplinkEvent
			err := unmarshalEvent(gc, &up)
			if err != nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), err))
				return
			}
			deviceInfo := up.GetDeviceInfo()
			if deviceInfo == nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), fmt.Errorf("deviceInfo is nil")))
				return
			}
			log.Logger.Debug("Uplink received", "dev_eui", deviceInfo.DevEui, "payload", fmt.Sprintf("%#v", up.Object), "user", userId, "fport", strconv.FormatUint(uint64(up.FPort), 10))
			err = controller.HandleEvent(gc.Request.Context(), userId, deviceInfo.DevEui, strconv.FormatUint(uint64(up.FPort), 10), up.Object, up.Time.AsTime())
			if err != nil {
				gc.Error(err)
				return
			}
			return
		case "join":
			var join integration.JoinEvent

			err := unmarshalEvent(gc, &join)
			if err != nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), err))
				return
			}
			deviceInfo := join.GetDeviceInfo()
			if deviceInfo == nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), fmt.Errorf("deviceInfo is nil")))
				return
			}
			log.Logger.Debug("Device joined", "dev_eui", deviceInfo.DevEui, "dev_addr", join.DevAddr, "user", userId)
			err = controller.AnnotateDeviceJoined(gc.Request.Context(), userId, deviceInfo.DevEui)
			if err != nil {
				gc.Error(err)
				return
			}
		case "status":
			var status integration.StatusEvent

			err := unmarshalEvent(gc, &status)
			if err != nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), err))
				return
			}
			deviceInfo := status.GetDeviceInfo()
			if deviceInfo == nil {
				gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), fmt.Errorf("statusInfo is nil")))
				return
			}
			log.Logger.Debug("Status received", "dev_eui", deviceInfo.DevEui, "user", userId)
			data := map[string]any{
				"external_power_source":     status.ExternalPowerSource,
				"battery_level_unavailable": status.BatteryLevelUnavailable,
				"battery_level":             status.BatteryLevel,
			}
			err = controller.HandleEvent(gc.Request.Context(), userId, deviceInfo.DevEui, "status", data, status.Time.AsTime())
			if err != nil {
				gc.Error(err)
				return
			}
			return

		default:
			gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unknown event type "+event)))
			return
		}
	}
}

func unmarshalEvent(gc *gin.Context, v proto.Message) error {
	body, err := io.ReadAll(gc.Request.Body)
	gc.Request.Body.Close()
	if err != nil {
		return err
	}
	switch gc.ContentType() {
	case "application/x-protobuf":
		fallthrough
	case "application/octet-stream":
		return proto.Unmarshal(body, v)
	case "application/json":
		return protojson.Unmarshal(body, v)
	default:
		return fmt.Errorf("unsupported content type " + gc.ContentType())
	}
}
