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

package model

import "github.com/SENERGY-Platform/models/go/models"

func UpsertDeviceAttribute(attribute models.Attribute, device *models.Device) bool {
	for i := range device.Attributes {
		if device.Attributes[i].Key == attribute.Key {
			if device.Attributes[i].Value == attribute.Value && device.Attributes[i].Origin == attribute.Origin {
				return false
			}
			device.Attributes[i].Value = attribute.Value
			device.Attributes[i].Origin = attribute.Origin
			return true
		}
	}
	device.Attributes = append(device.Attributes, attribute)
	return true
}

func UpsertGatewayAttribute(attribute models.Attribute, gateway *models.Hub) bool {
	for i := range gateway.Attributes {
		if gateway.Attributes[i].Key == attribute.Key {
			if gateway.Attributes[i].Value == attribute.Value && gateway.Attributes[i].Origin == attribute.Origin {
				return false
			}
			gateway.Attributes[i].Value = attribute.Value
			gateway.Attributes[i].Origin = attribute.Origin
			return true
		}
	}
	gateway.Attributes = append(gateway.Attributes, attribute)
	return true
}
