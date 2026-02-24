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
	"crypto/tls"
	"time"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"

	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Controller struct {
	config          configuration.Config
	chirpUserClient api.UserServiceClient
	chirpTenant     api.TenantServiceClient
	chirpApp        api.ApplicationServiceClient
}

func New(config configuration.Config) (*Controller, error) {

	conn, err := grpc.NewClient(config.ChirpstackUrl, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})), grpc.WithPerRPCCredentials(config.ChirpstackApiToken))
	if err != nil {
		return nil, err
	}

	chirpUserClient := api.NewUserServiceClient(conn)
	chirpTenant := api.NewTenantServiceClient(conn)
	chirpApp := api.NewApplicationServiceClient(conn)

	// test connection
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	defer cf()
	_, err = chirpTenant.List(ctx, nil)
	if err != nil {
		return nil, err
	}

	return &Controller{config: config, chirpUserClient: chirpUserClient, chirpTenant: chirpTenant, chirpApp: chirpApp}, nil
}
