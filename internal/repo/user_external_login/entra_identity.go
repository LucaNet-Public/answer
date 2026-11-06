/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package user_external_login

import (
	"context"

	"github.com/apache/answer/internal/entity"
	service "github.com/apache/answer/internal/service/user_external_login"
)

func (ur *userExternalLoginRepo) LookupEntraUsers(ctx context.Context, ids []string) ([]service.EntraUser, error) {
	rows := make([]service.EntraUser, 0)
	err := ur.data.DB.Context(ctx).Table("user_external_login").
		Select("user_external_login.external_id, user_external_login.user_id, `user`.username").
		Join("INNER", "user", "`user`.id = user_external_login.user_id").
		Where("user_external_login.provider = ?", "lucanet-entra-attribution").
		In("user_external_login.external_id", ids).
		Where("`user`.status = ?", entity.UserStatusAvailable).
		Where("EXISTS (SELECT 1 FROM user_external_login sso WHERE sso.provider = ? AND sso.user_id = user_external_login.user_id AND sso.external_id = user_external_login.meta_info)", "basic").Find(&rows)
	return rows, err
}

// Normal retries do not add rows. A concurrent duplicate for the same account
// is harmless: lookup deduplicates by account ID and rejects different accounts.
func (ur *userExternalLoginRepo) SaveEntraAlias(ctx context.Context, userID, id, subject string) error {
	exists, err := ur.data.DB.Context(ctx).Where("provider = ? AND external_id = ? AND user_id = ?", "lucanet-entra-attribution", id, userID).Exist(new(entity.UserExternalLogin))
	if err != nil {
		return err
	}
	if exists {
		_, err = ur.data.DB.Context(ctx).Where("provider = ? AND external_id = ? AND user_id = ?", "lucanet-entra-attribution", id, userID).Cols("meta_info").Update(&entity.UserExternalLogin{MetaInfo: subject})
		return err
	}
	return ur.AddUserExternalLogin(ctx, &entity.UserExternalLogin{UserID: userID, Provider: "lucanet-entra-attribution", ExternalID: id, MetaInfo: subject})
}
