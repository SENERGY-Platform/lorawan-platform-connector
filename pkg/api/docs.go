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
	"net/http"
	"strings"

	_ "github.com/SENERGY-Platform/lorawan-platform-connector/docs"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/controller"
	"github.com/gin-gonic/gin"
	"github.com/swaggo/swag"
)

//go:generate go tool swag init -o ../../docs --parseDependency -d .. -g api/api.go
func getSwaggerDoc(_ *controller.Controller) (string, string, gin.HandlerFunc) {
	return http.MethodGet, "/doc", func(gc *gin.Context) {
		doc, err := swag.ReadDoc()
		if err != nil {
			_ = gc.Error(err)
			return
		}

		gc.Header("Content-Type", gin.MIMEJSON)
		doc = strings.Replace(doc, `"host": "",`, "", 1)
		_, err = gc.Writer.Write([]byte(doc))
		if err != nil {
			_ = gc.Error(err)
			return
		}
	}
}
