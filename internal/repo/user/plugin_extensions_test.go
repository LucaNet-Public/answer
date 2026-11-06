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
	"testing"
	"time"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/plugin"
	"xorm.io/xorm"
)

type createdObserver struct {
	t   *testing.T
	ids []string
}

func (*createdObserver) Info() plugin.Info {
	return plugin.Info{SlugName: "test_user_created_observer"}
}
func (o *createdObserver) UserCreated(ctx context.Context, id, email string) {
	resolved, exists, err := plugin.UserIDByEmail(ctx, email)
	if err != nil || !exists {
		o.t.Fatalf("account is not committed before notification: %v %v", exists, err)
	}
	if id != "" && id != resolved {
		o.t.Fatal("creation event has an incorrect ID")
	}
	id = resolved
	o.ids = append(o.ids, id)
}

func TestUserCreatedExtension(t *testing.T) {
	db, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	db.SetMaxOpenConns(1)
	if err := db.Sync2(new(entity.User)); err != nil {
		t.Fatal(err)
	}
	observer := &createdObserver{t: t}
	plugin.Register(observer)
	plugin.StatusManager.Enable(observer.Info().SlugName, true)
	defer plugin.StatusManager.Enable(observer.Info().SlugName, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d := &data.Data{DB: db}
	repo := NewUserRepo(d)
	admin := &userAdminRepo{data: d}
	newUser := func(name string) *entity.User { return &entity.User{Username: name, EMail: name + "@example.test"} }
	registered, single := newUser("registered"), newUser("single")
	batch := []*entity.User{newUser("batch1"), newUser("batch2")}
	if err := repo.AddUser(ctx, registered); err != nil {
		t.Fatal(err)
	}
	if err := admin.AddUser(ctx, single); err != nil {
		t.Fatal(err)
	}
	if err := admin.AddUsers(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if len(observer.ids) != 4 {
		t.Fatalf("expected four creation events: %v", observer.ids)
	}
	seen := map[string]bool{}
	for _, id := range observer.ids {
		if id == "" || seen[id] {
			t.Fatalf("missing or duplicate generated ID: %v", observer.ids)
		}
		seen[id] = true
		if exists, err := db.ID(id).Exist(new(entity.User)); err != nil || !exists {
			t.Fatalf("event precedes account commit: %v", err)
		}
	}
	if err := repo.AddUser(ctx, newUser("registered")); err == nil {
		t.Fatal("duplicate registration succeeded")
	}
	if len(observer.ids) != 4 {
		t.Fatal("failed registration emitted an event")
	}
	plugin.StatusManager.Enable(observer.Info().SlugName, false)
	if err := repo.AddUser(ctx, newUser("disabled")); err != nil {
		t.Fatal(err)
	}
	if len(observer.ids) != 4 {
		t.Fatal("disabled observer received an event")
	}
}
