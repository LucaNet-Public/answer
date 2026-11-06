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
	"os"
	"testing"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"xorm.io/xorm"
)

func TestLookupReadsCurrentUsernameAndExcludesUnavailableAccounts(t *testing.T) {
	if os.Getenv("ANSWER_ENTRA_TEST_REQUIRE_POSTGRES") == "1" && os.Getenv("ANSWER_ENTRA_TEST_POSTGRES_DSN") == "" {
		t.Fatal("PostgreSQL CI regression requires a database DSN")
	}
	driver, dsn := "sqlite", ":memory:"
	if postgres := os.Getenv("ANSWER_ENTRA_TEST_POSTGRES_DSN"); postgres != "" {
		driver, dsn = "postgres", postgres
	}
	t.Logf("identity repository regression on %s", driver)
	db, err := xorm.NewEngine(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Sync2(new(entity.User), new(entity.UserExternalLogin)); err != nil {
		t.Fatal(err)
	}
	user := &entity.User{Username: "Andrei Sverdlov", Status: entity.UserStatusAvailable}
	if _, err := db.Insert(user); err != nil {
		t.Fatal(err)
	}
	alias := &entity.UserExternalLogin{Provider: "lucanet-entra-attribution", ExternalID: "tenant:object", UserID: user.ID, MetaInfo: "existing-sub"}
	if _, err := db.Insert(alias); err != nil {
		t.Fatal(err)
	}
	repo := &userExternalLoginRepo{data: &data.Data{DB: db}}
	if err := repo.SaveEntraAlias(context.Background(), user.ID, "tenant:object", "existing-sub"); err != nil {
		t.Fatal(err)
	}
	count, err := db.Where("provider = ?", "lucanet-entra-attribution").Count(new(entity.UserExternalLogin))
	if err != nil || count != 1 {
		t.Fatal("retry duplicated alias")
	}
	binding := &entity.UserExternalLogin{Provider: "basic", ExternalID: "existing-sub", UserID: user.ID}
	if _, err := db.Insert(binding); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.LookupEntraUsers(context.Background(), []string{"tenant:object"})
	if err != nil || len(rows) != 1 || rows[0].Username != "Andrei Sverdlov" {
		t.Fatalf("lookup failed: %v %v", rows, err)
	}
	user.Username = "changed"
	if _, err := db.ID(user.ID).Cols("username").Update(user); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.LookupEntraUsers(context.Background(), []string{"tenant:object"})
	if err != nil || rows[0].Username != "changed" {
		t.Fatal("username cached")
	}
	if _, err := db.Where("provider = ?", "basic").Delete(new(entity.UserExternalLogin)); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.LookupEntraUsers(context.Background(), []string{"tenant:object"})
	if err != nil || len(rows) != 0 {
		t.Fatal("removed SSO binding still resolved")
	}
	if _, err := db.Insert(&entity.UserExternalLogin{Provider: "basic", ExternalID: "existing-sub", UserID: user.ID}); err != nil {
		t.Fatal(err)
	}
	user.Status = entity.UserStatusDeleted
	if _, err := db.ID(user.ID).Cols("status").Update(user); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.LookupEntraUsers(context.Background(), []string{"tenant:object"})
	if err != nil || len(rows) != 0 {
		t.Fatal("deleted account exposed")
	}
}
