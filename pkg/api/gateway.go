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

	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/model"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/gin-gonic/gin"
)

// generateCert godoc
// @Summary      Generate Certificate
// @Description  Generates a new certificate
// @Param        hub_id path string true "Hub ID"
// @Success      200 {object} model.Certs "generated certificate"
// @Failure      400
// @Failure      500
// @Tags         Gateways
// @Security     Bearer
// @Router       /gateways/{hub_id}/cert [POST]
func generateCert(controller *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodPost, "/gateways/:hub_id/cert", func(gc *gin.Context) {
		token, err := jwt.GetParsedToken(gc.Request)
		if err != nil {
			gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("unable to parse token"), err))
			return
		}
		hubId := gc.Param("hub_id")
		if hubId == "" {
			gc.Error(errors.Join(model.ErrBadRequest, fmt.Errorf("missing query param hub_id")))
			return
		}
		certs, err := controller.ProvisionGatewayCerts(gc.Request.Context(), token, hubId)
		if err != nil {
			gc.Error(err)
		}
		gc.JSON(http.StatusOK, certs)
	}
}
