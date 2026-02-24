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

package log

import (
	"log/slog"
	"os"

	slogger "github.com/SENERGY-Platform/go-service-base/struct-logger"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/lorawan-platform-connector/pkg/configuration"
)

var Logger *slog.Logger

func Init(config configuration.Config) {
	options := &slog.HandlerOptions{
		AddSource: false,
		Level:     slogger.GetLevel(config.LogLevel, slog.LevelWarn),
	}

	handler := slogger.GetHandler(config.LogHandler, os.Stdout, options, slog.Default().Handler())
	handler = handler.WithAttrs([]slog.Attr{
		slog.String(attributes.ProjectKey, "github.com/SENERGY-Platform/lorawan-platform-connector"),
	})

	Logger = slog.New(handler)

	Logger.Debug("Logger Init")
}
