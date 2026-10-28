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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/Nerzal/gocloak/v13"
	device_repo "github.com/SENERGY-Platform/device-repository/lib/model"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	platform_connector_lib "github.com/SENERGY-Platform/platform-connector-lib"
	"github.com/SENERGY-Platform/platform-connector-lib/kafka"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/chirpstack/chirpstack/api/go/v4/common"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (c *Controller) SyncGateway(ctx context.Context, hub *models.Hub) error {
	eui := GetHubEUI(hub)

	if eui == nil {
		return nil
	}

	expiration := GetHubCertExpiration(hub)
	if expiration != nil && time.Until(*expiration) < 30*24*time.Hour {
		err := c.connector.SendNotification(platform_connector_lib.Notification{
			UserId:  hub.OwnerId,
			Title:   "LoRaWAN Certificate Expiry Warning",
			Message: fmt.Sprintf("Your certificate of hub %s will expire on %s and has not been renewed.", hub.Id, expiration.Format(time.RFC1123)),
		})
		if err != nil {
			log.Logger.Error("error sending certificate expiry notification", attributes.ErrorKey, err, "hub_id", hub.Id)
		}
	}

	gateway, err := c.chirpGateway.Get(ctx, &api.GetGatewayRequest{
		GatewayId: *eui,
	})

	if err != nil && status.Code(err) != codes.NotFound {
		log.Logger.Error("error getting gateway", attributes.ErrorKey, err)
		return err
	}

	// get user info
	user, err := c.gocloakClient.GetUserByID(ctx, c.accessToken(), "master", hub.OwnerId)
	if err != nil {
		return errors.Join(fmt.Errorf("unable to read user from keycloak"), err)
	}
	if user.Email == nil || *user.Email == "" {
		// user has no email, cannot determine tenant
		log.Logger.Warn("user has no email, cannot determine tenant for gateway", "userId", hub.OwnerId, "gateway_eui", eui, "hub_id", hub.Id)
		return nil
	}
	tenant, err := c.getOrCreateChirpstackTenantId(ctx, *user.Email, hub.OwnerId)
	if err != nil {
		return errors.Join(fmt.Errorf("unable to read tenant from chirpstack"), err)
	}

	// check if gateway exists in chirpstack
	if gateway != nil && tenant != gateway.Gateway.TenantId {
		if model.UpsertGatewayAttribute(models.Attribute{
			Key:    model.DeviceAttributeDuplicateKey,
			Value:  "true",
			Origin: model.AttributeOrigin,
		}, hub) {
			_, err, _ := c.deviceRepo.SetHub("Bearer "+c.accessToken(), *hub, device_repo.HubUpdateOptions{})
			if err != nil {
				return errors.Join(fmt.Errorf("unable to update hub (duplicate)"), err)
			}
		}
		return nil
	}

	var gw *api.Gateway
	if gateway != nil {
		gw = gateway.Gateway
	}

	newGateway, updateGw := prepareGateway(gw, hub, tenant)

	update := fillHubAttributes(gw, hub)
	update = c.linkHubDevices(ctx, gw, hub) || update // careful: lazy eval!
	if update {
		_, err, _ := c.deviceRepo.SetHub("Bearer "+c.accessToken(), *hub, device_repo.HubUpdateOptions{})
		if err != nil {
			return errors.Join(fmt.Errorf("unable to update hub (attributes & device link)"), err)
		}
	}

	if gateway == nil {
		// create gateway in chirpstack
		_, err := c.chirpGateway.Create(ctx, &api.CreateGatewayRequest{
			Gateway: newGateway,
		})
		if err != nil {
			log.Logger.Error("error creating gateway", "err", err)
			return err
		}
		log.Logger.Debug("created gateway in chirpstack", "gateway_eui", eui, "tenant_id", tenant)
		return nil
	}

	if updateGw {
		_, err := c.chirpGateway.Update(ctx, &api.UpdateGatewayRequest{
			Gateway: newGateway,
		})
		if err != nil {
			log.Logger.Error("error updating gateway", "err", err)
			return err
		}
		log.Logger.Debug("updated gateway in chirpstack", "gateway_eui", eui, "tenant_id", tenant)
	}
	return nil

}

func (c *Controller) SyncAllGateways() (err error) {
	mux := sync.Mutex{}
	wg := sync.WaitGroup{}
	var limit int64 = 1000
	var offset int64 = 0
	for {
		hubs, listErr, _ := c.deviceRepo.ListHubs("Bearer "+c.accessToken(), device_repo.HubListOptions{
			Limit:  limit,
			Offset: offset,
		})
		if listErr != nil {
			wg.Wait()
			return errors.Join(err, listErr)
		}

		for _, hub := range hubs {

			wg.Go(func() {
				ctx, cf := context.WithTimeout(context.Background(), 10*time.Second)
				defer cf()
				err2 := c.SyncGateway(ctx, &hub)
				if err2 != nil {
					mux.Lock()
					err = errors.Join(err, err2)
					mux.Unlock()
					return
				}
			})

		}

		if int64(len(hubs)) < limit {
			break
		}
		offset += limit
	}
	wg.Wait()
	return err
}

func (c *Controller) DeleteOutdatedGateways() (err error) {
	ctx, cf := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cf()

	// ChirpStack first: a gateway is created after its hub, so the hub of every
	// gateway listed here is in the hub list read afterwards, unless it has been
	// deleted. Read the other way round, a hub created in between was missing
	// from the hub list while its new gateway was already in ChirpStack.
	chirpGws := []*api.GatewayListItem{}
	var limit uint32 = 9999
	var offset uint32 = 0
	for {
		page, listErr := c.chirpGateway.List(ctx, &api.ListGatewaysRequest{
			Limit:  limit,
			Offset: offset,
		})
		if listErr != nil {
			log.Logger.Error("error listing chirpstack gateways", "err", listErr)
			return listErr
		}
		chirpGws = append(chirpGws, page.Result...)
		if int64(len(page.Result)) < int64(limit) {
			break
		}
		offset += limit
	}

	var repoLimit int64 = 9999
	var repoOffset int64 = 0
	hubs := []models.Hub{}
	for {
		hubBatch, listErr, _ := c.deviceRepo.ListHubs("Bearer "+c.accessToken(), device_repo.HubListOptions{
			Limit:  repoLimit,
			Offset: repoOffset,
		})
		if listErr != nil {
			return listErr
		}
		hubs = append(hubs, hubBatch...)
		if int64(len(hubBatch)) < repoLimit {
			break
		}
		repoOffset += repoLimit
	}

	mux := sync.Mutex{}
	wg := sync.WaitGroup{}
	for _, gw := range chirpGws {
		wg.Go(func() {
			exists, err2 := c.gatewayHasHub(ctx, gw, hubs)
			if err2 != nil {
				// unknown is not outdated: keep the gateway and report the error
				mux.Lock()
				err = errors.Join(err, fmt.Errorf("unable to check gateway %s: %w", gw.GatewayId, err2))
				mux.Unlock()
				return
			}
			if exists {
				return
			}
			// no matching gateway found in repo, delete from chirpstack
			_, err2 = c.chirpGateway.Delete(ctx, &api.DeleteGatewayRequest{
				GatewayId: gw.GatewayId,
			})
			if err2 != nil {
				log.Logger.Error("error deleting gateway from chirpstack", "err", err2, "gateway_eui", gw.GatewayId)
				mux.Lock()
				err = errors.Join(err, err2)
				mux.Unlock()
				return
			}
			log.Logger.Debug("deleted gateway from chirpstack", "gateway_eui", gw.GatewayId)
		})
	}
	wg.Wait()
	return err
}

// gatewayHasHub reports whether a hub carries the gateway's EUI and belongs to
// the owner of the gateway's tenant, which is matched by comparing the tenant
// name with the user's email. An error leaves the answer open, and the caller
// must not take it for false: a failed lookup used to delete the gateway.
func (c *Controller) gatewayHasHub(ctx context.Context, gw *api.GatewayListItem, hubs []models.Hub) (bool, error) {
	var tenant *api.GetTenantResponse
	for _, hub := range hubs {
		if !slices.ContainsFunc(hub.Attributes, func(a models.Attribute) bool {
			return a.Key == model.GatewayAttributeEUI && a.Value == gw.GatewayId
		}) {
			continue
		}
		if tenant == nil {
			var err error
			tenant, err = c.chirpTenant.Get(ctx, &api.GetTenantRequest{
				Id: gw.TenantId,
			})
			if err != nil {
				return false, err
			}
		}
		user, err := c.gocloakClient.GetUserByID(ctx, c.accessToken(), "master", hub.OwnerId)
		var apiErr *gocloak.APIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			continue // the hub's owner is gone, so this hub does not keep the gateway
		}
		if err != nil {
			return false, err
		}
		if user.Email != nil && *user.Email == tenant.Tenant.Name {
			return true, nil
		}
	}
	return false, nil
}

func (c *Controller) setupEventSyncGateway(ctx context.Context) error {
	if c.config.KafkaBootstrap == "" {
		log.Logger.Warn("unable to setup kafka event sync: no kafka bootstrap defined")
		return nil
	}
	return kafka.NewConsumer(ctx, kafka.ConsumerConfig{
		KafkaUrl:         c.config.KafkaBootstrap,
		GroupId:          "lorawan-platform-connector",
		Topic:            "hubs",
		MaxWait:          time.Second,
		MinBytes:         1000,
		MaxBytes:         1000000,
		InitTopic:        false,
		AllowOldMessages: false,
		Logger:           log.Logger,
	}, func(_ string, msg []byte, _ time.Time) error {
		var command model.HubCommand
		err := json.Unmarshal(msg, &command)
		if err != nil {
			return err
		}
		switch command.Command {
		case model.RightsCommand:
			return nil
		case model.PutCommand:
			// get latest version of hub
			hub, err, code := c.deviceRepo.ReadHub(command.Hub.Id, "Bearer "+c.accessToken(), device_repo.READ)
			if err != nil {
				// A hub that is gone cannot be synced and no redelivery changes
				// that. Every other error is transient as far as we can tell and
				// has to be reported, or the hub silently never gets synced.
				if code == http.StatusNotFound {
					log.Logger.Warn("skipping gateway sync, hub not found", "hub_id", command.Hub.Id)
					return nil
				}
				return errors.Join(fmt.Errorf("unable to read hub from device repo"), err)
			}
			ctx2, cf := context.WithTimeout(ctx, 10*time.Second)
			defer cf()
			return c.SyncGateway(ctx2, &hub)
		case model.DeleteCommand:
			eui := GetHubEUI(&command.Hub)

			if eui == nil {
				return nil
			}
			ctx2, cf := context.WithTimeout(ctx, 10*time.Second)
			defer cf()
			log.Logger.Debug("deleting gateway", "hub_id", command.Hub.Id, "gateway_eui", eui)
			gateway, err := c.chirpGateway.Get(ctx2, &api.GetGatewayRequest{
				GatewayId: *eui,
			})

			// if gateway not found, nothing to do
			if err != nil && status.Code(err) == codes.NotFound {
				return nil
			} else if err != nil {
				return err
			}

			// get user info
			user, err := c.gocloakClient.GetUserByID(ctx2, c.accessToken(), "master", command.Hub.OwnerId)
			if err != nil {
				return err
			}
			if user.Email == nil || *user.Email == "" {
				// user has no email, cannot determine tenant
				return nil
			}
			tenantId, err := c.getOrCreateChirpstackTenantId(ctx2, *user.Email, command.Hub.OwnerId)
			if err != nil {
				return err
			}

			if tenantId != gateway.Gateway.TenantId {
				return nil // gateway is in different application, do not delete
			}

			_, err = c.chirpGateway.Delete(ctx2, &api.DeleteGatewayRequest{
				GatewayId: *eui,
			})
			if err != nil && status.Code(err) != codes.NotFound {
				return err
			}
			return nil
		default:
			log.Logger.Warn("unhandeled command on hub kafka topic", "command", command.Command)
			return nil
		}
	}, func(err error) {
		log.Logger.Error("kafka EventSyncGateway error", attributes.ErrorKey, err)
	})
}

func prepareGateway(gw *api.Gateway, hub *models.Hub, tenantId string) (*api.Gateway, bool) {
	rv := &api.Gateway{
		TenantId: tenantId,
	}
	updated := false

	if gw != nil {
		rv.Description = gw.Description
		rv.Location = gw.Location
		rv.StatsInterval = gw.StatsInterval
		rv.GatewayId = gw.GatewayId
		rv.Metadata = gw.Metadata
		rv.StatsInterval = gw.StatsInterval
		rv.Tags = gw.Tags
		rv.Name = gw.Name
	}

	if rv.Name != hub.Name {
		updated = true
	}
	rv.Name = hub.Name
	for _, a := range hub.Attributes {
		switch a.Key {
		case model.GatewayAttributeEUI:
			rv.GatewayId = a.Value
		case model.GatewayAttributeLat:
			if rv.Location == nil {
				rv.Location = &common.Location{}
				updated = true
			}
			lat, err := strconv.ParseFloat(a.Value, 64)
			if err != nil {
				log.Logger.Error("error parsing gateway latitude", "err", err, "value", a.Value)
			} else {
				if rv.Location.Latitude != lat {
					updated = true
				}
				rv.Location.Latitude = lat
			}
		case model.GatewayAttributeLon:
			if rv.Location == nil {
				rv.Location = &common.Location{}
				updated = true
			}
			lon, err := strconv.ParseFloat(a.Value, 64)
			if err != nil {
				log.Logger.Error("error parsing gateway longitude", "err", err, "value", a.Value)
			} else {
				if rv.Location.Longitude != lon {
					updated = true
				}
				rv.Location.Longitude = lon
			}

		case model.DeviceAttributeMessageMaxAgeKey:
			maxAge, err := time.ParseDuration(a.Value)
			if err != nil {
				log.Logger.Error("error parsing gateway message max age", "err", err, "value", a.Value)
			} else {
				if rv.StatsInterval != uint32(maxAge.Seconds()) {
					updated = true
				}
				rv.StatsInterval = uint32(maxAge.Seconds())
			}
		default:
			continue
		}
	}
	return rv, updated
}

func fillHubAttributes(gateway *api.Gateway, hub *models.Hub) bool {
	if gateway == nil {
		return false
	}
	updated := false
	if gateway.Location != nil {
		if gateway.Location.Latitude != 0 {
			updated = model.UpsertGatewayAttribute(models.Attribute{
				Key:    model.GatewayAttributeLat,
				Value:  strconv.FormatFloat(gateway.Location.Latitude, 'f', -1, 64),
				Origin: model.AttributeOriginWebUI,
			}, hub) || updated // careful: lazy eval!
		}
		if gateway.Location.Longitude != 0 {
			updated = model.UpsertGatewayAttribute(models.Attribute{
				Key:    model.GatewayAttributeLon,
				Value:  strconv.FormatFloat(gateway.Location.Longitude, 'f', -1, 64),
				Origin: model.AttributeOriginWebUI,
			}, hub) || updated // careful: lazy eval!
		}
	}
	if gateway.StatsInterval != 0 {
		updated = model.UpsertGatewayAttribute(models.Attribute{
			Key:    model.DeviceAttributeMessageMaxAgeKey,
			Value:  strconv.FormatUint(uint64(gateway.StatsInterval), 10) + "s",
			Origin: model.AttributeOriginWebUI,
		}, hub) || updated // careful: lazy eval!
	}
	return updated
}

func (c *Controller) linkHubDevices(ctx context.Context, gateway *api.Gateway, hub *models.Hub) bool {
	if gateway == nil {
		return false
	}
	localIds := []string{}
	rx := regexp.MustCompile(fmt.Sprintf(model.RedisKeyFmtGatewayDevice, gateway.GatewayId, "(.*)"))

	var cursor uint64
	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, fmt.Sprintf(model.RedisKeyFmtGatewayDevice, gateway.GatewayId, "*"), 1000).Result()
		if err != nil {
			log.Logger.Error("error scanning redis keys", attributes.ErrorKey, err)
		}

		for _, key := range keys {
			matches := rx.FindStringSubmatch(key)
			if len(matches) != 2 {
				log.Logger.Error("unexpected key in redis", "key", key)
				continue
			}
			localIds = append(localIds, matches[1])
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	updated := !slices.Equal(hub.DeviceLocalIds, localIds)
	hub.DeviceLocalIds = localIds
	return updated
}

func GetHubEUI(hub *models.Hub) *string {
	for _, a := range hub.Attributes {
		if a.Key == model.GatewayAttributeEUI {
			return &a.Value
		}
	}
	return nil
}

func GetHubCertExpiration(hub *models.Hub) *time.Time {
	for _, a := range hub.Attributes {
		if a.Key == model.GatewayAttributeCertsExpiration {
			t, err := time.Parse(time.RFC3339, a.Value)
			if err != nil {
				log.Logger.Error("error parsing gateway cert expiration", attributes.ErrorKey, err, "value", a.Value)
				return nil
			}
			return &t
		}
	}
	return nil
}
