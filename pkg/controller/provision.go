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
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/chirpstack/chirpstack/api/go/v4/api"
)

const appName = "platform-integration"
const encoding = api.Encoding_PROTOBUF

// ProvisionUser creates a tenant and integration for the given user. chirpUserId is the id of the user in chirpstack. If the user does not exist (empty chirpUserId), it will be created.
func (c *Controller) ProvisionUser(ctx context.Context, chirpUserId string, userInfo *model.UserInfo) error {
	// input validation
	if userInfo == nil {
		return errors.Join(model.ErrBadRequest, fmt.Errorf("userInfo is nil"))
	}
	if userInfo.PreferredUsername == nil {
		return errors.Join(model.ErrBadRequest, fmt.Errorf("userInfo has no preferred_username set"))
	}
	if userInfo.Email == nil {
		return errors.Join(model.ErrBadRequest, fmt.Errorf("userInfo has no email set"))
	}
	if userInfo.Sub == nil {
		return errors.Join(model.ErrBadRequest, fmt.Errorf("userInfo has no sub set"))
	}

	if chirpUserId == "" {
		var limit uint32 = 1000
		var offset uint32 = 0
		cont := true
		for cont {
			users, err := c.chirpUserClient.List(ctx, &api.ListUsersRequest{Limit: limit, Offset: offset})
			if err != nil {
				return err
			}
			for _, user := range users.GetResult() {
				if user.Email == *userInfo.Email {
					chirpUserId = user.Id
					cont = false
					break
				}
			}
			cont = len(users.GetResult()) == int(limit)
			offset += limit
		}
		if chirpUserId == "" {
			userCreateResp, err := c.chirpUserClient.Create(ctx, &api.CreateUserRequest{
				User: &api.User{
					Email:    *userInfo.Email,
					IsActive: true,
					IsAdmin:  false,
					Note:     "Created by lorawan-platform-connector",
				},
			})
			if err != nil {
				return err
			}
			chirpUserId = userCreateResp.GetId()
		}
	}

	// get tenant id
	tenantResp, err := c.chirpTenant.List(ctx, &api.ListTenantsRequest{UserId: chirpUserId, Limit: 2})
	if err != nil {
		return err
	}
	tenants := tenantResp.GetResult()
	var tenantId string
	if len(tenants) == 0 {
		tenant, err := c.createTenant(ctx, *userInfo.PreferredUsername)
		if err != nil {
			return err
		}
		tenantId = tenant.Id
	} else if len(tenants) > 1 {
		log.Logger.Error("found multiple tenants", "chirpstack_user_id", chirpUserId, "keycloak_user_id", userInfo.Sub, "email", userInfo.Email)
		return fmt.Errorf("found multiple tenants")
	} else {
		tenantId = tenants[0].Id
	}

	// add user to tenant if needed
	_, err = c.chirpTenant.GetUser(ctx, &api.GetTenantUserRequest{
		TenantId: tenantId,
		UserId:   chirpUserId,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			_, err = c.chirpTenant.AddUser(ctx, &api.AddTenantUserRequest{TenantUser: &api.TenantUser{
				TenantId:       tenantId,
				UserId:         chirpUserId,
				Email:          *userInfo.Email,
				IsGatewayAdmin: false,
				IsAdmin:        false,
				IsDeviceAdmin:  false,
			}})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// get app id
	appList, err := c.chirpApp.List(ctx, &api.ListApplicationsRequest{TenantId: tenantId, Search: appName, Limit: 2})
	if err != nil {
		return err
	}

	var appId string
	if len(appList.Result) == 0 {
		app, err := c.createApp(ctx, tenantId)
		if err != nil {
			return err
		}
		appId = app.Id
	} else if len(appList.Result) > 1 {
		log.Logger.Error("found multiple apps", "tenant_id", tenantId)
		return fmt.Errorf("found multiple apps")
	} else {
		appId = appList.Result[0].Id
	}

	// update or create integration
	endpoint := fmt.Sprintf("%s:%s%s", c.config.Host, strconv.FormatUint(uint64(c.config.ServerPort), 10), model.EventPath)
	integration, err := c.chirpApp.GetHttpIntegration(ctx, &api.GetHttpIntegrationRequest{
		ApplicationId: appId,
	})
	if err == nil {
		if integration.Integration.EventEndpointUrl != endpoint || integration.Integration.Encoding != encoding {
			_, err = c.chirpApp.DeleteHttpIntegration(ctx, &api.DeleteHttpIntegrationRequest{
				ApplicationId: appId,
			})
			if err != nil {
				return err
			}
			err = c.createIntegration(ctx, appId, *userInfo.Sub, endpoint)
		}
	} else if status.Code(err) == codes.NotFound {
		err = c.createIntegration(ctx, appId, *userInfo.Sub, endpoint)
	} else {
		return err
	}
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) ProvisionAllUsers() error {
	getUsersCtx, getUsersCf := context.WithTimeout(context.Background(), 10*time.Second)
	defer getUsersCf()
	c.jwtMux.RLock()
	kcUsers, err := c.gocloakClient.GetUsers(getUsersCtx, c.jwt.AccessToken, "master", gocloak.GetUsersParams{})
	c.jwtMux.RUnlock()
	if err != nil {
		return err
	}
	for _, kcUser := range kcUsers {
		provisionCtx, provisionCf := context.WithTimeout(context.Background(), 10*time.Second)
		err = c.ProvisionUser(provisionCtx, "", model.UserInfoFromUser(kcUser))
		if err != nil {
			log.Logger.Warn("unable to provision user", "user", *kcUser.Username, attributes.ErrorKey, err)
		}
		provisionCf()
	}
	return nil
}

func (c *Controller) createTenant(ctx context.Context, username string) (tenant *api.CreateTenantResponse, err error) {
	return c.chirpTenant.Create(ctx, &api.CreateTenantRequest{
		Tenant: &api.Tenant{
			Name:                username,
			PrivateGatewaysUp:   true,
			PrivateGatewaysDown: true,
			CanHaveGateways:     true,
			Tags: map[string]string{
				"Managed-By": "lorawan-platform-connector",
			},
		},
	})
}

func (c *Controller) createApp(ctx context.Context, tenantId string) (app *api.CreateApplicationResponse, err error) {
	return c.chirpApp.Create(ctx, &api.CreateApplicationRequest{
		Application: &api.Application{
			Name:        appName,
			Description: "automatic intgegration managed by lorawan-platform-connector",
			TenantId:    tenantId,
		},
	})
}

func (c *Controller) createIntegration(ctx context.Context, appId string, platformUserId string, endpoint string) (err error) {
	_, err = c.chirpApp.CreateHttpIntegration(ctx, &api.CreateHttpIntegrationRequest{
		Integration: &api.HttpIntegration{
			ApplicationId: appId,
			Headers: map[string]string{
				"X-UserID": platformUserId,
			},
			Encoding:         encoding,
			EventEndpointUrl: endpoint,
		},
	})
	return err
}
