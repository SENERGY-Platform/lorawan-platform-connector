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
	"slices"
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
func (c *Controller) ProvisionUser(ctx context.Context, chirpUserId string, userInfo *model.UserInfo) (err error) {
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

	// get chirpstack user id
	if chirpUserId == "" {
		chirpUserId, err = c.getOrCreateChirpstackUserId(ctx, *userInfo.Email)
		if err != nil {
			return err
		}
	}

	// get tenant id
	tenantId, err := c.getOrCreateChirpstackTenantId(ctx, *userInfo.Email)
	if err != nil {
		return err
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

func (c *Controller) DeleteUser(ctx context.Context, email string) error {
	// get ids
	userId, err := c.getOrCreateChirpstackUserId(ctx, email)
	if err != nil {
		return err
	}
	tenantId, err := c.getOrCreateChirpstackTenantId(ctx, email)
	if err != nil {
		return err
	}

	// delete
	_, err = c.chirpUserClient.Delete(ctx, &api.DeleteUserRequest{Id: userId})
	if err != nil {
		return err
	}
	_, err = c.chirpTenant.Delete(ctx, &api.DeleteTenantRequest{Id: tenantId})
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) DeleteOutdatedUsers() error {
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
	c.jwtMux.RLock()
	kcUsers, err := c.gocloakClient.GetUsers(ctx, c.jwt.AccessToken, "master", gocloak.GetUsersParams{})
	c.jwtMux.RUnlock()
	cf()
	if err != nil {
		return err
	}

	var limit uint32 = 1000
	var offset uint32 = 0
	chirpUsers := []*api.UserListItem{}
	cont := true
	for cont {
		ctx, cf = context.WithTimeout(context.Background(), 10*time.Second)
		users, err := c.chirpUserClient.List(ctx, &api.ListUsersRequest{Limit: limit, Offset: offset})
		cf()
		if err != nil {
			return err
		}
		chirpUsers = append(chirpUsers, users.GetResult()...)
		cont = len(users.GetResult()) == int(limit)
		offset += limit
	}

	for _, chirpUser := range chirpUsers {
		if slices.Contains(c.config.ChirpstackProtectedUsers, chirpUser.Email) {
			continue
		}
		// search matching keycloak user
		match := false
		for _, kcUser := range kcUsers {
			if kcUser == nil || kcUser.Email == nil {
				continue
			}
			if chirpUser.Email == *kcUser.Email {
				match = true
				break
			}
		}
		if match {
			continue
		}
		// no matching keycloak user exists
		ctx, cf = context.WithTimeout(context.Background(), 10*time.Second)
		err = c.DeleteUser(ctx, chirpUser.Email)
		cf()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) createTenant(ctx context.Context, email string) (tenant *api.CreateTenantResponse, err error) {
	return c.chirpTenant.Create(ctx, &api.CreateTenantRequest{
		Tenant: &api.Tenant{
			Name:                email,
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

func (c *Controller) getOrCreateChirpstackUserId(ctx context.Context, email string) (string, error) {
	var limit uint32 = 1000
	var offset uint32 = 0
	cont := true
	for cont {
		users, err := c.chirpUserClient.List(ctx, &api.ListUsersRequest{Limit: limit, Offset: offset})
		if err != nil {
			return "", err
		}
		for _, user := range users.GetResult() {
			if user.Email == email {
				return user.Id, nil
			}
		}
		cont = len(users.GetResult()) == int(limit)
		offset += limit
	}
	// not found, creating
	userCreateResp, err := c.chirpUserClient.Create(ctx, &api.CreateUserRequest{
		User: &api.User{
			Email:    email,
			IsActive: true,
			IsAdmin:  false,
			Note:     "Created by lorawan-platform-connector",
		},
	})
	if err != nil {
		return "", err
	}
	return userCreateResp.GetId(), err
}

func (c *Controller) getOrCreateChirpstackTenantId(ctx context.Context, email string) (string, error) {
	tenantResp, err := c.chirpTenant.List(ctx, &api.ListTenantsRequest{Search: email, Limit: 2})
	if err != nil {
		return "", err
	}
	tenants := tenantResp.GetResult()
	if len(tenants) == 0 {
		tenant, err := c.createTenant(ctx, email)
		if err != nil {
			return "", err
		}
		return tenant.Id, nil
	} else if len(tenants) > 1 {
		log.Logger.Error("found multiple tenants", "email", email)
		return "", fmt.Errorf("found multiple tenants")
	}
	return tenants[0].Id, nil

}
