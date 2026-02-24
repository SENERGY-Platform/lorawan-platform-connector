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

package configuration

import (
	"reflect"

	envldr "github.com/SENERGY-Platform/go-env-loader"
)

func GetTypeParser() map[reflect.Type]envldr.Parser {
	return map[reflect.Type]envldr.Parser{
		reflect.TypeFor[ChirpstackToken](): chirpstackTokenParser,
	}
}

func chirpstackTokenParser(_ reflect.Type, val string, _ []string, _ map[string]string) (interface{}, error) {
	return ChirpstackToken(val), nil
}
