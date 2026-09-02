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
	"reflect"
	"slices"
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

// ensureSyncedService compares the prepared content variable with the existing one by reflect.DeepEqual,
// which is order sensitive. prepareContentVariable collects the sub content variables from go maps, so
// without sorting the order varied per call and every event triggered a device type update.
func TestPrepareContentVariableOrderIsStable(t *testing.T) {
	event := map[string]any{
		"TempC_SHT":    22.5,
		"Hum_SHT":      55.1,
		"TempC_DS":     21.0,
		"Ext":          int64(1),
		"Systimestamp": int64(1756800000),
		"Node_type":    "LHT52",
		"nested": map[string]any{
			"b": true,
			"a": false,
		},
		"list": []any{"x", "y", "z"},
	}

	var val any = event
	first := prepareContentVariable(&val, nil)
	if first == nil {
		t.Error("prepareContentVariable returned nil")
		return
	}

	for range 50 {
		var v any = event
		cv := prepareContentVariable(&v, nil)
		if cv == nil {
			t.Error("prepareContentVariable returned nil")
			return
		}
		if !reflect.DeepEqual(*first, *cv) {
			t.Errorf("order is not stable: first %v, now %v", subNames(first), subNames(cv))
			return
		}
	}

	// the comparison in ensureSyncedService has to hold when the same event is prepared against itself
	for range 50 {
		var v any = event
		cv := prepareContentVariable(&v, first)
		if cv == nil {
			t.Error("prepareContentVariable returned nil")
			return
		}
		if !reflect.DeepEqual(*first, *cv) {
			t.Errorf("unchanged event reported as changed: base %v, now %v", subNames(first), subNames(cv))
			return
		}
	}

	if !slices.IsSorted(subNames(first)) {
		t.Errorf("sub content variables are not sorted by name: %v", subNames(first))
		return
	}
	for _, sub := range first.SubContentVariables {
		if sub.Name != "list" {
			continue
		}
		// a list keeps its index order instead of being sorted lexicographically
		if !slices.Equal(subNames(&sub), []string{"0", "1", "2"}) {
			t.Errorf("list sub content variables lost their index order: %v", subNames(&sub))
			return
		}
	}
}

func subNames(cv *models.ContentVariable) []string {
	names := []string{}
	for _, sub := range cv.SubContentVariables {
		names = append(names, sub.Name)
	}
	return names
}
