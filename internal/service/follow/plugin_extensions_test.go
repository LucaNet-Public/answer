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

package follow

import (
	"context"
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/service/activity_common"
	"github.com/apache/answer/internal/service/tag_common"
	"github.com/apache/answer/plugin"
)

type extensionTags struct {
	tag_common.TagCommonRepo
	tag *entity.Tag
}

func (r extensionTags) GetTagBySlugName(context.Context, string) (*entity.Tag, bool, error) {
	return r.tag, true, nil
}

type extensionFollows struct{ objectID, userID string }

func (r *extensionFollows) Follow(_ context.Context, id, userID string) error {
	r.objectID, r.userID = id, userID
	return nil
}
func (*extensionFollows) FollowCancel(context.Context, string, string) error {
	panic("unexpected cancellation")
}

type extensionCounts struct{ activity_common.FollowRepo }

func (extensionCounts) GetFollowAmount(context.Context, string) (int, error) { return 1, nil }

func TestTagServiceExtensionsDelegateWithoutPolicy(t *testing.T) {
	// Deleted and merged tags are returned unchanged; consumers own eligibility.
	tag := &entity.Tag{ID: "1", SlugName: "alias", Status: entity.TagStatusDeleted, MainTagID: 2}
	follows := &extensionFollows{}
	NewFollowService(follows, extensionCounts{}, extensionTags{tag: tag})
	got, exists, err := plugin.LookupTag(context.Background(), "alias")
	if err != nil || !exists || got != tag {
		t.Fatalf("lookup changed the core result: %+v %v", got, err)
	}
	if err := plugin.FollowTag(context.Background(), "1", "user"); err != nil {
		t.Fatal(err)
	}
	if follows.objectID != "1" || follows.userID != "user" {
		t.Fatal("follow adapter changed arguments")
	}
}
