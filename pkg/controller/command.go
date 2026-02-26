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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	platform_connector_lib "github.com/SENERGY-Platform/platform-connector-lib"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/protobuf/types/known/structpb"
)

func (c *Controller) HandleCommand(deviceId string, deviceLocalId string, serviceId string, serviceLocalId string, requestMsg platform_connector_lib.CommandRequestMsg) (responseMsg platform_connector_lib.CommandResponseMsg, qos platform_connector_lib.Qos, err error) {
	serviceLocalIdUint, err := strconv.ParseUint(serviceLocalId, 10, 32)
	if err != nil {
		return nil, platform_connector_lib.SyncIdempotent, errors.Join(fmt.Errorf("unable to parse service local id into uint: %s", serviceId), err)
	}

	// decode incoming message
	var data any
	err = json.Unmarshal([]byte(requestMsg[model.ProtocolSegmentData]), &data)
	if err != nil {
		return nil, platform_connector_lib.SyncIdempotent, errors.Join(fmt.Errorf("unable to parse request into protobuf: %s", requestMsg[model.ProtocolSegmentData]), err)
	}

	downlinkData := map[string]any{
		model.ProtocolSegmentFPort: serviceLocalIdUint,
		model.ProtocolSegmentData:  data,
	}

	object, err := structpb.NewStruct(downlinkData)
	if err != nil {
		return nil, platform_connector_lib.SyncIdempotent, errors.Join(fmt.Errorf("unable to parse request into protobuf: %s", requestMsg), err)
	}

	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	resp, err := c.chirpDevice.Enqueue(ctx, &api.EnqueueDeviceQueueItemRequest{
		QueueItem: &api.DeviceQueueItem{
			DevEui: deviceLocalId,
			FPort:  uint32(serviceLocalIdUint),
			Object: object,
		},
	})
	if err != nil {
		return nil, platform_connector_lib.SyncIdempotent, errors.Join(fmt.Errorf("unable to enque request"), err)
	}
	responseMsg = platform_connector_lib.CommandResponseMsg{
		model.ProtocolSegmentData: resp.Id,
	}
	return responseMsg, platform_connector_lib.SyncIdempotent, nil
}
