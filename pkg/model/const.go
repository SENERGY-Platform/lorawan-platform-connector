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

const EventPath = "/event"
const ProtocolSegmentData = "data"
const ProtocolSegmentFPort = "fPort"
const DeviceTypeAttributeManagedByKey = "senergy/managed-by"
const DeviceTypeAttributeManagedByValue = "lorawan-platform-connector"
const DeviceTypeAttributeDeviceProfileIdKey = "senergy/lora/device-profile-id"

const DeviceAttributeDevAddrKey = "senergy/lora/dev-addr"
const DeviceAttributeAppKey = "senergy/lora/app-key"
const DeviceAttributeGenAppKey = "senergy/lora/gen-app-key"
const DeviceAttributeNwkKey = "senergy/lora/nwk-key"
const DeviceAttributeAppSKey = "senergy/lora/app-s-key"
const DeviceAttributeNwkSEncKey = "senergy/lora/nwk-s-enc-key"
const DeviceAttributeSNwkSIntKey = "senergy/lora/s-nwk-s-int-key"
const DeviceAttributeFNwkSIntKey = "senergy/lora/f-nwk-s-int-key"
const DeviceAttributeJoinEuiKey = "senergy/lora/join-eui"
const DeviceAttributeJoinedKey = "senergy/lora/joined"
const DeviceAttributeDuplicateKey = "senergy/lora/duplicate"
const DeviceAttributeSupportsOTAAKey = "senergy/lora/supports-otaa"
const DeviceAttributeSupportsClassBKey = "senergy/lora/supports-class-b"
const DeviceAttributeSupportsClassCKey = "senergy/lora/supports-class-c"
const DeviceAttributeMessageMaxAgeKey = "last_message_max_age"

const GatewayAttributeEUI = "senergy/lora/eui"
const GatewayAttributeLat = "location-lat"
const GatewayAttributeLon = "location-lon"

const AttributeOrigin = "lorawan-platform-connector"
const AttributeOriginWebUI = "web-ui"

const RedisPrefix = "lorawan-platform-connector_"
const RedisKeyFmtGatewayDevice = RedisPrefix + "gateway_%s_%s"
