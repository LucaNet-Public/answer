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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/plugin"
	"github.com/google/uuid"
	"github.com/segmentfault/pacman/log"
)

const entraProvider = "lucanet-entra-attribution"

type EntraUser struct {
	ExternalID string `json:"-" xorm:"external_id"`
	UserID     string `json:"-" xorm:"user_id"`
	ObjectID   string `json:"object_id" xorm:"-"`
	Username   string `json:"username" xorm:"username"`
}

// Kept separate from UserExternalLoginRepo so existing connector implementations
// and their test doubles remain source-compatible.
type entraLookupRepo interface {
	LookupEntraUsers(context.Context, []string) ([]EntraUser, error)
	SaveEntraAlias(context.Context, string, string, string) error
}

func (us *UserExternalLoginService) recordEntraIdentity(ctx context.Context, userID string, info *schema.ExternalLoginUserInfoCache) {
	if err := us.saveEntraIdentity(ctx, userID, info); err != nil {
		// Attribution failure must not strand an existing SSO account.
		log.Warnf("SSO stable attribution unavailable: %v", err)
	}
}

func (us *UserExternalLoginService) saveEntraIdentity(ctx context.Context, userID string, info *schema.ExternalLoginUserInfoCache) error {
	if info.Provider != "basic" {
		return nil
	}
	var metadata struct {
		Identity struct {
			Tenant string `json:"tenant_id"`
			Object string `json:"object_id"`
		} `json:"lucanet_verified_entra"`
	}
	if err := json.Unmarshal([]byte(info.MetaInfo), &metadata); err != nil {
		return nil
	}
	expected, _, _ := attributionScope()
	identity := metadata.Identity
	if expected == "" || identity.Tenant != expected {
		return nil
	}
	if _, err := uuid.Parse(identity.Tenant); err != nil {
		return nil
	}
	object, err := uuid.Parse(identity.Object)
	if err != nil {
		return nil
	}
	id := identity.Tenant + ":" + object.String()
	old, exists, err := us.userExternalLoginRepo.GetByExternalID(ctx, entraProvider, id)
	if err != nil {
		return err
	}
	repo, ok := us.userExternalLoginRepo.(entraLookupRepo)
	if !ok {
		return fmt.Errorf("stable attribution storage unavailable")
	}
	// Retain both associations on a conflict. The batch lookup then refuses
	// to choose either account instead of silently crediting the older one.
	if err := repo.SaveEntraAlias(ctx, userID, id, info.ExternalID); err != nil {
		return err
	}
	if exists && old.UserID != userID {
		return fmt.Errorf("stable attribution is linked to multiple accounts")
	}
	return nil
}

// A read-only batch endpoint for the configured miner account. It returns
// current usernames; neither emails nor raw SSO metadata leave Answer.
func (us *UserExternalLoginService) ResolveEntraUsers(ctx context.Context, caller string, objects []string) ([]EntraUser, error) {
	tenant, provider, client := attributionScope()
	if tenant == "" || provider == "" || client == "" || caller == "" {
		return nil, fmt.Errorf("attribution lookup forbidden")
	}
	miner, exists, err := us.userExternalLoginRepo.GetByUserID(ctx, provider, caller)
	if err != nil {
		return nil, err
	}
	if !exists || miner.ExternalID != client {
		return nil, fmt.Errorf("attribution lookup forbidden")
	}
	if len(objects) == 0 || len(objects) > 100 {
		return nil, fmt.Errorf("attribution lookup requires 1 through 100 object IDs")
	}
	ids := make([]string, 0, len(objects))
	requested := map[string]bool{}
	for _, object := range objects {
		parsed, err := uuid.Parse(object)
		if err != nil {
			return nil, fmt.Errorf("invalid attribution object ID")
		}
		id := tenant + ":" + parsed.String()
		if !requested[id] {
			ids = append(ids, id)
			requested[id] = true
		}
	}
	repo, ok := us.userExternalLoginRepo.(entraLookupRepo)
	if !ok {
		return nil, fmt.Errorf("attribution lookup unavailable")
	}
	rows, err := repo.LookupEntraUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	groups := map[string]map[string]EntraUser{}
	for _, row := range rows {
		if !requested[row.ExternalID] || row.UserID == "" || row.Username == "" {
			continue
		}
		if groups[row.ExternalID] == nil {
			groups[row.ExternalID] = map[string]EntraUser{}
		}
		row.ObjectID = strings.TrimPrefix(row.ExternalID, tenant+":")
		groups[row.ExternalID][row.UserID] = row
	}
	result := make([]EntraUser, 0, len(groups))
	for _, id := range ids {
		group := groups[id]
		if len(group) != 1 {
			continue
		} // No arbitrary choice between accounts.
		for _, row := range group {
			result = append(result, row)
		}
	}
	return result, nil
}

// Both boundaries come from enabled connector instances, so disabling or
// reconfiguring a connector takes effect without stale environment settings.
var attributionScope = enabledAttributionScope

func enabledAttributionScope() (tenant, provider, externalID string) {
	_ = plugin.CallConnector(func(connector plugin.Connector) error {
		if connector.ConnectorSlugName() == "basic" {
			if sso, ok := connector.(interface{ EntraAttributionTenant() string }); ok {
				tenant = sso.EntraAttributionTenant()
			}
		}
		if connector.ConnectorSlugName() == "teams-miner" {
			if miner, ok := connector.(interface{ AttributionLookupIdentity() (string, string) }); ok {
				provider, externalID = miner.AttributionLookupIdentity()
			}
		}
		return nil
	})
	return
}
