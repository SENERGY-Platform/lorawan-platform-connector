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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Nerzal/gocloak/v13"
	device_repo_client "github.com/SENERGY-Platform/device-repository/lib/client"
	device_repo "github.com/SENERGY-Platform/device-repository/lib/model"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/log"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeGateways struct {
	api.GatewayServiceClient
	list    []*api.GatewayListItem
	mux     sync.Mutex
	deleted []string
}

func (f *fakeGateways) List(_ context.Context, _ *api.ListGatewaysRequest, _ ...grpc.CallOption) (*api.ListGatewaysResponse, error) {
	return &api.ListGatewaysResponse{Result: f.list}, nil
}

func (f *fakeGateways) Get(_ context.Context, _ *api.GetGatewayRequest, _ ...grpc.CallOption) (*api.GetGatewayResponse, error) {
	return nil, status.Error(codes.NotFound, "not found")
}

func (f *fakeGateways) Delete(_ context.Context, in *api.DeleteGatewayRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	f.mux.Lock()
	defer f.mux.Unlock()
	f.deleted = append(f.deleted, in.GatewayId)
	return &emptypb.Empty{}, nil
}

// every tenant is named after the email of its owner
type fakeTenants struct {
	api.TenantServiceClient
}

func (fakeTenants) Get(_ context.Context, in *api.GetTenantRequest, _ ...grpc.CallOption) (*api.GetTenantResponse, error) {
	return &api.GetTenantResponse{Tenant: &api.Tenant{Id: in.Id, Name: in.Id + "@example.com"}}, nil
}

type fakeHubRepo struct {
	device_repo_client.Interface
	hubs []models.Hub
}

func (f fakeHubRepo) ListHubs(_ string, _ device_repo.HubListOptions) ([]models.Hub, error, int) {
	return f.hubs, nil, http.StatusOK
}

// fakeKeycloakUserByID knows owner-a with the email of tenant-a, answers 404
// for owner-gone and fails for everyone else
func fakeKeycloakUserByID(t *testing.T) *httptest.Server {
	if log.Logger == nil {
		log.Logger = slog.New(slog.DiscardHandler)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/admin/realms/master/users/") {
		case "owner-a":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(gocloak.User{Email: gocloak.StringP("tenant-a@example.com")})
		case "owner-gone":
			http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
		default:
			http.Error(w, "unavailable", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func hub(owner string, eui string) models.Hub {
	return models.Hub{OwnerId: owner, Attributes: []models.Attribute{{Key: model.GatewayAttributeEUI, Value: eui}}}
}

// a failed owner lookup used to count as "no hub", so a short keycloak outage
// deleted every gateway it touched; now that the sync runs every hour, that
// would happen with every outage
func TestDeleteOutdatedGatewaysKeepsGatewayWhenCheckFails(t *testing.T) {
	keycloak := fakeKeycloakUserByID(t)
	gateways := &fakeGateways{list: []*api.GatewayListItem{
		{GatewayId: "aaaa", TenantId: "tenant-a"}, //hub and owner match
		{GatewayId: "bbbb", TenantId: "tenant-b"}, //keycloak fails for the owner
		{GatewayId: "cccc", TenantId: "tenant-c"}, //no hub
		{GatewayId: "dddd", TenantId: "tenant-d"}, //the hub's owner is gone
	}}
	c := &Controller{
		chirpGateway:  gateways,
		chirpTenant:   fakeTenants{},
		deviceRepo:    fakeHubRepo{hubs: []models.Hub{hub("owner-a", "aaaa"), hub("owner-b", "bbbb"), hub("owner-gone", "dddd")}},
		gocloakClient: gocloak.NewClient(keycloak.URL),
		jwt:           &gocloak.JWT{AccessToken: "token"},
	}

	err := c.DeleteOutdatedGateways()
	if err == nil || !strings.Contains(err.Error(), "bbbb") {
		t.Error("expected the failed check of bbbb to be reported, got", err)
	}
	slices.Sort(gateways.deleted)
	if !slices.Equal(gateways.deleted, []string{"cccc", "dddd"}) {
		t.Error(gateways.deleted)
	}
}

// the goroutines joined their errors into a variable shadowed by the list loop,
// so SyncAllGateways reported success however many gateways failed
func TestSyncAllGatewaysReportsFailedGateway(t *testing.T) {
	keycloak := fakeKeycloakUserByID(t)
	c := &Controller{
		chirpGateway:  &fakeGateways{},
		deviceRepo:    fakeHubRepo{hubs: []models.Hub{hub("owner-b", "bbbb")}},
		gocloakClient: gocloak.NewClient(keycloak.URL),
		jwt:           &gocloak.JWT{AccessToken: "token"},
	}
	if err := c.SyncAllGateways(); err == nil {
		t.Error("expected the failed keycloak lookup of owner-b to be reported")
	}
}
