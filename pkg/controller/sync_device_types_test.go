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
	"testing"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/models/go/models"
)

// ensureSyncedService receives the device type as returned by the iot cache, which hands out the cached
// value without copying it. Modifying the service slice in place used to leave a service with an empty
// content variable behind, which the device repository later rejects with "missing content variable name".
func TestEnsureSyncedServiceDoesNotModifyInput(t *testing.T) {
	const protocolId = "urn:infai:ses:protocol:1"
	const segmentId = "urn:infai:ses:protocol-segment:1"

	c := &Controller{
		config: configuration.Config{
			ProtocolId:            protocolId,
			ProtocolDataSegmentId: segmentId,
		},
	}

	dt := models.DeviceType{
		Id:   "urn:infai:ses:device-type:1",
		Name: "Dragino LHT52 (EU868) v1.0.0",
		Services: []models.Service{{
			Id:          "urn:infai:ses:service:status",
			LocalId:     "status",
			Interaction: models.EVENT,
			Name:        "Get Battery Level",
			ProtocolId:  protocolId,
			Outputs: []models.Content{{
				Id:                "urn:infai:ses:content:status",
				Serialization:     models.JSON,
				ProtocolSegmentId: segmentId,
				ContentVariable: models.ContentVariable{
					Id:   "urn:infai:ses:content-variable:root",
					Name: "root",
					Type: models.Structure,
					SubContentVariables: []models.ContentVariable{{
						Id:   "urn:infai:ses:content-variable:eps",
						Name: "external_power_source",
						Type: models.Boolean,
					}},
				},
			}},
		}},
	}
	services := dt.Services

	// the event matches the existing service, so no update is needed and the connector is not used
	serviceId, err := c.ensureSyncedService(dt, "status", map[string]any{"external_power_source": true})
	if err != nil {
		t.Error("unexpected error", err)
		return
	}
	if serviceId != "urn:infai:ses:service:status" {
		t.Error("unexpected service id", serviceId)
		return
	}

	cv := services[0].Outputs[0].ContentVariable
	if cv.Name != "root" || cv.Type != models.Structure {
		t.Errorf("content variable of the caller's device type was modified: %#v", cv)
		return
	}
	if len(cv.SubContentVariables) != 1 || cv.SubContentVariables[0].Name != "external_power_source" {
		t.Errorf("sub content variables of the caller's device type were modified: %#v", cv.SubContentVariables)
		return
	}
}
