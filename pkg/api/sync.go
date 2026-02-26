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

package api

import (
	"errors"
	"net/http"

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/gin-gonic/gin"
)

// patchSyncAllUsers godoc
// @Summary      Sync Users
// @Description  Syncs all users
// @Success      200 {object} string "status message (or null)"
// @Failure      400
// @Failure      500
// @Tags         Sync
// @Security     Bearer
// @Router       /sync/users [PATCH]
func patchSyncAllUsers(controller *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodPatch, "/sync/users", func(gc *gin.Context) {
		err := errors.Join(
			controller.ProvisionAllUsers(),
			controller.DeleteOutdatedUsers(),
		)
		if err != nil {
			gc.Error(err)
			return
		}
	}
}
