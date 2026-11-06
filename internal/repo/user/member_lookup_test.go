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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/plugin"
	"xorm.io/xorm"
)

func TestPublicMemberSearchFindsMixedCaseAndExcludesUnavailable(t *testing.T) {
	db, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "members.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Sync2(new(entity.User)); err != nil {
		t.Fatal(err)
	}
	for _, user := range []*entity.User{{ID: "1", Username: "MarkusD", DisplayName: "Markus Doe", Status: 1, MailStatus: 1}, {ID: "2", Username: "robert", DisplayName: "Robert Smith", Status: 9, MailStatus: 1}, {ID: "3", Username: "rob", DisplayName: "Robert Brown", Status: 1, MailStatus: 2}} {
		if _, err := db.Insert(user); err != nil {
			t.Fatal(err)
		}
	}
	for i, name := range []string{"mark_us", "markXus", "percent%", "bang!", `back\slash`} {
		if _, err := db.Insert(&entity.User{ID: fmt.Sprint(i + 10), Username: name, DisplayName: name, Status: 1, MailStatus: 1}); err != nil {
			t.Fatal(err)
		}
	}
	NewUserRepo(&data.Data{DB: db})
	for _, query := range []string{"markusd", "MARKUSD", "MarkusD"} {
		members, err := plugin.SearchMembers(context.Background(), query)
		if err != nil || len(members) != 1 || members[0].Username != "MarkusD" {
			t.Fatal(query, members, err)
		}
	}
	for _, query := range []string{"mark_", "percent%", "bang!", `back\`} {
		members, err := plugin.SearchMembers(context.Background(), query)
		if err != nil || len(members) != 1 {
			t.Fatal(query, members, err)
		}
	}
	members, err := plugin.SearchMembers(context.Background(), "rob")
	if err != nil || len(members) != 0 {
		t.Fatal(members, err)
	}
	members, err = plugin.LookupMembers(context.Background(), []string{"1", "2", "3"})
	if err != nil || len(members) != 1 || members[0].ID != "1" {
		t.Fatal(members, err)
	}
}
