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

package configuration

type Config struct {
	ChirpstackUrl            string          `env_var:"CHIRPSTACK_URL"`
	ChirpstackApiToken       ChirpstackToken `env_var:"CHIRPSTACK_API_TOKEN"`
	ChirpstackProtectedUsers []string        `env_var:"CHIRPSTACK_PROTECTED_USERS"`
	KeycloakUrl              string          `env_var:"KEYCLOAK_URL"`
	KeycloakClientId         string          `env_var:"KEYCLOAK_CLIENT_ID"`
	KeycloakClientSecret     string          `env_var:"KEYCLOAK_CLIENT_SECRET"`
	LogLevel                 string          `env_var:"LOG_LEVEL"`
	LogHandler               string          `env_var:"LOG_HANDLER"`
	ServerPort               uint            `env_var:"SERVER_PORT"`
	ServerPortCommands       uint            `env_var:"SERVER_PORT_COMMANDS"`
	Host                     string          `env_var:"HOST"`
	KafkaBootstrap           string          `env_var:"KAFKA_BOOTSTRAP"`
	DeviceRepoUrl            string          `env_var:"DEVICE_REPO_URL"`
	PermissionsV2Url         string          `env_var:"PERMISSIONS_V2_URL"`
	MemcachedUrl             string          `env_var:"MEMCACHED_URL"`
	NotificationsUrl         string          `env_var:"NOTIFICATIONS_URL"`
}
