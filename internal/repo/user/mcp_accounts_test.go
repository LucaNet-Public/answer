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

package user

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/plugin"
	"xorm.io/xorm"
)

func TestPluginUserAvailableUsesCurrentAccountState(t *testing.T) {
	db, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Sync2(new(entity.User)); err != nil {
		t.Fatal(err)
	}
	previous := plugin.UserAvailable
	t.Cleanup(func() { plugin.UserAvailable = previous })
	NewUserRepo(&data.Data{DB: db})
	u := &entity.User{ID: "123", Username: "reader", EMail: "reader@example.com", Status: entity.UserStatusAvailable, MailStatus: entity.EmailStatusAvailable}
	if _, err = db.Insert(u); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if ok, err := plugin.UserAvailable(ctx, u.ID); err != nil || !ok {
		t.Fatalf("active account: %v %v", ok, err)
	}
	for _, status := range []int{entity.UserStatusSuspended, entity.UserStatusDeleted} {
		if _, err = db.Where("id = ?", u.ID).Cols("status").Update(&entity.User{Status: status}); err != nil {
			t.Fatal(err)
		}
		if ok, err := plugin.UserAvailable(ctx, u.ID); err != nil || ok {
			t.Fatalf("status %d: %v %v", status, ok, err)
		}
	}
	if _, err = db.Where("id = ?", u.ID).Cols("status", "mail_status").Update(&entity.User{Status: entity.UserStatusAvailable, MailStatus: entity.EmailStatusToBeVerified}); err != nil {
		t.Fatal(err)
	}
	if ok, err := plugin.UserAvailable(ctx, u.ID); err != nil || ok {
		t.Fatalf("unverified account: %v %v", ok, err)
	}
	if ok, err := plugin.UserAvailable(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing account: %v %v", ok, err)
	}
}
