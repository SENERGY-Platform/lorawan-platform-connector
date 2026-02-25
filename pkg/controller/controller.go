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
	"sync"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Controller struct {
	config          configuration.Config
	chirpUserClient api.UserServiceClient
	chirpTenant     api.TenantServiceClient
	chirpApp        api.ApplicationServiceClient
	gocloakClient   *gocloak.GoCloak
	jwt             *gocloak.JWT
	jwtMux          sync.RWMutex
}

func New(config configuration.Config, ctx context.Context) (*Controller, error) {
	// create chirpstack client
	conn, err := grpc.NewClient(config.ChirpstackUrl, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})), grpc.WithPerRPCCredentials(config.ChirpstackApiToken))
	if err != nil {
		return nil, err
	}

	chirpUserClient := api.NewUserServiceClient(conn)
	chirpTenant := api.NewTenantServiceClient(conn)
	chirpApp := api.NewApplicationServiceClient(conn)

	// test connection
	gocloakCtx, chirpCf := context.WithTimeout(ctx, 10*time.Second)
	defer chirpCf()
	_, err = chirpTenant.List(gocloakCtx, nil)
	if err != nil {
		return nil, err
	}

	// create gocloak client
	gocloakClient := gocloak.NewClient(config.KeycloakUrl)
	gocloakCtx, gocloakCf := context.WithTimeout(ctx, 10*time.Second)
	defer gocloakCf()
	jwt, err := gocloakClient.LoginClient(gocloakCtx, config.KeycloakClientId, config.KeycloakClientSecret, "master")
	if err != nil {
		return nil, err
	}
	// create controller
	controller := &Controller{config: config, chirpUserClient: chirpUserClient, chirpTenant: chirpTenant, chirpApp: chirpApp, jwt: jwt, gocloakClient: gocloakClient, jwtMux: sync.RWMutex{}}
	controller.setupSync(ctx)

	// setup token refresh
	go func() {
		ticker := time.NewTicker(time.Duration(jwt.ExpiresIn)*time.Second - 10*time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				jwt, err := gocloakClient.RefreshToken(ctx, jwt.RefreshToken, config.KeycloakClientId, config.KeycloakClientSecret, "master")
				if err != nil {
					log.Logger.Error("failed to refresh token", "error", err)
					continue
				}
				controller.jwtMux.Lock()
				controller.jwt = jwt
				controller.jwtMux.Unlock()
			}
		}
	}()
	return controller, nil
}
