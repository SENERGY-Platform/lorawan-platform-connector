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
	"net/http"
	"time"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Controller) ProvisionGatewayCerts(ctx context.Context, token jwt.Token, hubId string) (certs *model.Certs, err error) {
	hub, err, code := c.deviceRepo.ReadHub(hubId, token.Token, models.Write)
	if err != nil {
		return certs, provisionGatewayCertsHandleErr(err, &code)
	}
	eui := GetHubEUI(&hub)
	if eui == nil {
		return certs, errors.Join(model.ErrBadRequest, fmt.Errorf("hub does not have a valid EUI"))
	}
	email := token.Email
	sub := token.Sub
	if hub.OwnerId != string(token.Sub) {
		// get user info
		c.jwtMux.RLock()
		user, err := c.gocloakClient.GetUserByID(ctx, c.jwt.AccessToken, "master", hub.OwnerId)
		c.jwtMux.RUnlock()
		if err != nil {
			return certs, err
		}
		if user.Email == nil || user.ID == nil {
			return certs, errors.Join(model.ErrBadRequest, fmt.Errorf("user does not have email or id"))
		}
		email = *user.Email
		sub = *user.ID
	}

	tenantId, err := c.getOrCreateChirpstackTenantId(ctx, email, sub)
	if err != nil {
		return certs, err
	}

	// search all gateways with the same name as the hub, because chirpstack does not support searching by EUI.
	gw, err := c.chirpGateway.Get(ctx, &api.GetGatewayRequest{
		GatewayId: *eui,
	})
	if err != nil || gw.Gateway == nil {
		return certs, provisionGatewayCertsHandleErr(err, nil)
	}
	if gw.Gateway.TenantId != tenantId {
		return certs, errors.Join(model.ErrForbidden, fmt.Errorf("did not find matching chirpstack gateway"))
	}

	certResp, err := c.chirpGateway.GenerateClientCertificate(ctx, &api.GenerateGatewayClientCertificateRequest{
		GatewayId: *eui,
	})
	if err != nil {
		return certs, err
	}
	update := model.UpsertGatewayAttribute(models.Attribute{
		Key:    model.GatewayAttributeCertsExpiration,
		Value:  certResp.ExpiresAt.AsTime().Format(time.RFC3339),
		Origin: model.AttributeOrigin,
	}, &hub)
	if update {
		_, err, code = c.deviceRepo.SetHub(token.Token, hub)
		if err != nil {
			return certs, provisionGatewayCertsHandleErr(err, &code)
		}
	}
	return &model.Certs{
		Certificate: certResp.TlsCert,
		Key:         certResp.TlsKey,
		ExpiresAt:   certResp.ExpiresAt.AsTime(),
	}, nil

}

func provisionGatewayCertsHandleErr(err error, code *int) error {
	if code != nil {
		switch *code {
		case http.StatusNotFound:
			return errors.Join(model.ErrNotFound, err)
		case http.StatusForbidden:
			return errors.Join(model.ErrForbidden, err)
		}
	}
	if status.Code(err) == codes.NotFound {
		return errors.Join(model.ErrForbidden, fmt.Errorf("did not find matching chirpstack gateway"))
	}
	return err
}
