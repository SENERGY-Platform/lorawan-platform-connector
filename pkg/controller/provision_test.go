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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Nerzal/gocloak/v13"
)

// fakeKeycloakUsers answers the users endpoint of the master realm like keycloak
// does without max: with the first 100 users only. The email filter is the
// loosest a keycloak version applies, a case-insensitive substring search that
// ignores exact.
func fakeKeycloakUsers(t *testing.T, emails []string) *Controller {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/realms/master/users" {
			t.Error("unexpected path", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		selected := []string{}
		for _, email := range emails {
			if filter := query.Get("email"); filter != "" && !strings.Contains(strings.ToLower(email), strings.ToLower(filter)) {
				continue
			}
			selected = append(selected, email)
		}
		first, _ := strconv.Atoi(query.Get("first"))
		max := 100
		if query.Has("max") {
			max, _ = strconv.Atoi(query.Get("max"))
		}
		first = min(first, len(selected))
		users := []gocloak.User{}
		for _, email := range selected[first:min(first+max, len(selected))] {
			users = append(users, gocloak.User{Email: gocloak.StringP(email)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(users)
	}))
	t.Cleanup(server.Close)
	return &Controller{
		gocloakClient: gocloak.NewClient(server.URL),
		jwt:           &gocloak.JWT{AccessToken: "token"},
	}
}

// DeleteOutdatedUsers deleted the ChirpStack user and tenant of everyone past
// the first 100 keycloak users, because the list was read without paging
func TestKeycloakUsersReadsEveryPage(t *testing.T) {
	emails := []string{}
	for i := range 250 {
		emails = append(emails, fmt.Sprintf("user%03d@example.com", i))
	}
	c := fakeKeycloakUsers(t, emails)
	users, err := c.keycloakUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != len(emails) {
		t.Fatal(len(users))
	}
	for i, user := range users {
		if *user.Email != emails[i] {
			t.Fatal(i, *user.Email)
		}
	}
}

func TestKeycloakUsersExactPageSize(t *testing.T) {
	emails := []string{}
	for i := range 200 {
		emails = append(emails, fmt.Sprintf("user%03d@example.com", i))
	}
	users, err := fakeKeycloakUsers(t, emails).keycloakUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != len(emails) {
		t.Fatal(len(users))
	}
}

func TestKeycloakUserExists(t *testing.T) {
	c := fakeKeycloakUsers(t, []string{"anna@example.com", "johanna@example.com"})
	for email, expected := range map[string]bool{
		"anna@example.com":    true,
		"ANNA@example.com":    true,
		"hanna@example.com":   false, //found by the substring search as johanna, which is someone else
		"johanna@example.com": true,
		"nobody@example.com":  false,
	} {
		exists, err := c.keycloakUserExists(context.Background(), email)
		if err != nil {
			t.Fatal(err)
		}
		if exists != expected {
			t.Error(email, exists)
		}
	}
}
