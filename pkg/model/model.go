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

package model

import "github.com/Nerzal/gocloak/v13"

type UserInfo struct {
	PreferredUsername *string `json:"preferred_username"`
	Email             *string `json:"email"`
	Sub               *string `json:"sub"`
}

func UserInfoFromUser(u *gocloak.User) *UserInfo {
	return &UserInfo{
		PreferredUsername: u.Username,
		Email:             u.Email,
		Sub:               u.ID,
	}
}

func UserInfoFromGocloakUserInfo(u *gocloak.UserInfo) *UserInfo {
	return &UserInfo{
		PreferredUsername: u.PreferredUsername,
		Email:             u.Email,
		Sub:               u.Sub,
	}
}
