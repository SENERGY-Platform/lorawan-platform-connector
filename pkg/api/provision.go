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
	"fmt"
	"net/http"

	"github.com/Nerzal/gocloak/v13"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/gin-gonic/gin"
)

// postProvision godoc
// @Summary      Provision
// @Description  Runs the provision for a new user
// @Accept       json
// @Param        user_id query string true "newly creted user id"
// @Param        payload body gocloak.UserInfo true "user info"
// @Success      200 {object} string "status message (or null)"
// @Failure      400
// @Failure      500
// @Router       /provision [POST]
func postProvision(controller *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/provision", func(gc *gin.Context) {
		chirpUserId := gc.Query("user_id")
		if chirpUserId == "" {
			_ = gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("missing query param user_id")))
			return
		}

		var userInfo gocloak.UserInfo
		err := gc.ShouldBindJSON(&userInfo)
		if err != nil {
			_ = gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse request body"), err))
			return
		}
		err = controller.ProvisionUser(gc.Request.Context(), chirpUserId, userInfo)
		if err != nil {
			_ = gc.Error(errors.Join(fmt.Errorf("unable to provision user"), err))
			return
		}
	}
}
