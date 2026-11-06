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

package review

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	answercommon "github.com/apache/answer/internal/service/answer_common"
	commentcommon "github.com/apache/answer/internal/service/comment_common"
	"github.com/apache/answer/internal/service/mock"
	"github.com/apache/answer/internal/service/object_info"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	usercommon "github.com/apache/answer/internal/service/user_common"
	"github.com/apache/answer/internal/service/vector_sync"
	"go.uber.org/mock/gomock"
)

type mentionQueue[T any] struct{ messages []T }

func (q *mentionQueue[T]) Send(_ context.Context, msg T)                { q.messages = append(q.messages, msg) }
func (*mentionQueue[T]) RegisterHandler(func(context.Context, T) error) {}
func (*mentionQueue[T]) Close()                                         {}

type mentionUsers struct {
	usercommon.UserRepo
	users     map[string]*entity.User
	calls     map[string]int
	authorErr error
}

func (r *mentionUsers) GetByUsername(_ context.Context, name string) (*entity.User, bool, error) {
	if name == "broken" {
		return nil, false, errors.New("lookup failed")
	}
	u, ok := r.users[name]
	if !ok {
		return &entity.User{}, false, nil
	}
	return u, true, nil
}
func (r *mentionUsers) GetByUserID(_ context.Context, id string) (*entity.User, bool, error) {
	if r.calls != nil {
		r.calls[id]++
	}
	if id == "101" && r.authorErr != nil {
		return nil, false, r.authorErr
	}
	for _, u := range r.users {
		if u.ID == id {
			return u, true, nil
		}
	}
	return &entity.User{}, false, nil
}

type mentionQuestionRepo struct {
	questioncommon.QuestionRepo
	question *entity.Question
}

func (r mentionQuestionRepo) GetQuestion(context.Context, string) (*entity.Question, bool, error) {
	return r.question, true, nil
}

type mentionAnswerRepo struct {
	answercommon.AnswerRepo
	answer *entity.Answer
}

func (r mentionAnswerRepo) GetAnswer(context.Context, string) (*entity.Answer, bool, error) {
	return r.answer, r.answer != nil, nil
}

type mentionReviewRepo struct {
	ReviewRepo
	review *entity.Review
}

func (r mentionReviewRepo) GetReview(context.Context, int) (*entity.Review, bool, error) {
	return r.review, true, nil
}
func (r mentionReviewRepo) UpdateReviewStatus(_ context.Context, _ int, _ string, status int) error {
	r.review.Status = status
	return nil
}

type mentionPendingComments struct {
	commentcommon.CommentCommonRepo
	comment *entity.Comment
}

func (r mentionPendingComments) GetCommentWithoutStatus(context.Context, string) (*entity.Comment, bool, error) {
	return r.comment, true, nil
}
func (r mentionPendingComments) UpdateCommentStatus(_ context.Context, _ string, status int) error {
	r.comment.Status = status
	return nil
}

func TestCommentMentionQueuesEmail(t *testing.T) {
	for _, onAnswer := range []bool{false, true} {
		t.Run(fmt.Sprintf("answer=%t", onAnswer), func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				names       []string
				replyUserID string
				want        map[string]string
				hidden      bool
				owner       bool
				rejected    bool
			}{
				{name: "mention", names: []string{"recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}},
				{name: "multiple mentions", names: []string{"recipient", "other"}, want: map[string]string{"102": constant.NotificationMentionYou, "106": constant.NotificationMentionYou}},
				{name: "repeated mention", names: []string{"recipient", "recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}},
				{name: "self mention", names: []string{"author"}},
				{name: "unknown and failed lookup", names: []string{"missing", "broken", "recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}},
				{name: "hidden question", names: []string{"recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}, hidden: true},
				{name: "hidden question owner", names: []string{"recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}, hidden: true, owner: true},
				{name: "reply mentioning another person", replyUserID: "105", names: []string{"recipient"}, want: map[string]string{"105": constant.NotificationReplyToYou, "102": constant.NotificationMentionYou}},
				{name: "reply mentioning its recipient and another person", replyUserID: "105", names: []string{"reply-user", "recipient"}, want: map[string]string{"105": constant.NotificationReplyToYou, "102": constant.NotificationMentionYou}},
				{name: "reply with repeated and self mentions", replyUserID: "105", names: []string{"recipient", "recipient", "reply-user", "author", "other"}, want: map[string]string{"105": constant.NotificationReplyToYou, "102": constant.NotificationMentionYou, "106": constant.NotificationMentionYou}},
				{name: "reply and mention same person once", replyUserID: "105", names: []string{"reply-user"}, want: map[string]string{"105": constant.NotificationReplyToYou}},
				{name: "reply to own comment mentioning another person", replyUserID: "101", names: []string{"recipient"}, want: map[string]string{"102": constant.NotificationMentionYou}},
				{name: "reply with no mentions", replyUserID: "105", want: map[string]string{"105": constant.NotificationReplyToYou}},
				{name: "rejected reply with mentions", replyUserID: "105", names: []string{"recipient", "other"}, rejected: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					users := &mentionUsers{users: map[string]*entity.User{
						"recipient":  {ID: "102", Username: "recipient", EMail: "recipient@example.test", Language: "de_DE", Status: entity.UserStatusAvailable},
						"author":     {ID: "101", Username: "author", DisplayName: "Author", Status: entity.UserStatusAvailable},
						"reply-user": {ID: "105", Username: "reply-user", EMail: "reply@example.test", Language: "de_DE", Status: entity.UserStatusAvailable},
						"other":      {ID: "106", Username: "other", EMail: "other@example.test", Language: "de_DE", Status: entity.UserStatusAvailable},
					}}
					site := mock.NewMockSiteInfoCommonService(gomock.NewController(t))

					site.EXPECT().FormatAvatar(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&schema.AvatarInfo{}).AnyTimes()
					common := usercommon.NewUserCommon(users, nil, nil, site)
					inbox := &mentionQueue[*schema.NotificationMsg]{}
					external := &mentionQueue[*schema.ExternalNotificationMsg]{}
					question := &entity.Question{ID: "10010000000000001", UserID: "103", Title: "Question title", Status: entity.QuestionStatusAvailable}
					if tc.hidden {
						question.Show = entity.QuestionHide
					}
					if tc.owner {
						question.UserID = "102"
					}
					comment := &entity.Comment{ID: "10070000000000001", ObjectID: question.ID, QuestionID: question.ID, UserID: "101", ParsedText: "<p>A comment</p>"}
					var answer *entity.Answer
					if onAnswer {
						answer = &entity.Answer{ID: "10020000000000001", QuestionID: question.ID, UserID: "104", Status: entity.AnswerStatusAvailable}
						comment.ObjectID = answer.ID
					}
					for _, name := range tc.names {
						comment.OriginalText += "[@" + name + "](/users/" + name + ") "
					}
					objects := object_info.NewObjService(mentionAnswerRepo{answer: answer}, mentionQuestionRepo{question: question}, nil, nil, nil)
					if tc.replyUserID != "" {
						comment.SetReplyUserID(tc.replyUserID)
						comment.SetReplyCommentID("10070000000000002")
					}
					comment.Status = entity.CommentStatusPending
					pending := &entity.Review{ID: 1, ObjectID: comment.ID, ObjectType: constant.ObjectTypeStrMapping[constant.CommentObjectType], Status: entity.ReviewStatusPending}
					service := &ReviewService{userRepo: users, userCommon: common, notificationQueueService: inbox, externalNotificationQueueService: external, objectInfoService: objects,
						reviewRepo: mentionReviewRepo{review: pending}, commentCommonRepo: mentionPendingComments{comment: comment}, questionRepo: mentionQuestionRepo{question: question}, vectorSyncService: &mentionQueue[*vector_sync.Task]{}}
					decision := "approve"
					wantStatus := entity.CommentStatusAvailable
					if tc.rejected {
						decision = "reject"
						wantStatus = entity.CommentStatusDeleted
					}
					if err := service.UpdateReview(context.Background(), &schema.UpdateReviewReq{ReviewID: 1, Status: decision, UserID: "moderator"}); err != nil {
						t.Fatal(err)
					}
					if comment.Status != wantStatus {
						t.Fatalf("wrong comment status: %d, want %d", comment.Status, wantStatus)
					}
					if len(inbox.messages) != len(tc.want) {
						t.Fatalf("queued %d inbox messages, want recipients %v", len(inbox.messages), tc.want)
					}
					seen := map[string]bool{}
					for _, msg := range inbox.messages {
						action, ok := tc.want[msg.ReceiverUserID]
						if !ok || action != msg.NotificationAction || seen[msg.ReceiverUserID] {
							t.Fatalf("unexpected or duplicate inbox event: %+v; want %v", msg, tc.want)
						}
						seen[msg.ReceiverUserID] = true
					}
					wantMail := len(tc.want)
					if tc.hidden && !tc.owner {
						wantMail = 0
					}
					if len(external.messages) != wantMail {
						t.Fatalf("queued %d email messages, want %d for recipients %v", len(external.messages), wantMail, tc.want)
					}
					seen = map[string]bool{}
					for _, msg := range external.messages {
						action, ok := tc.want[msg.ReceiverUserID]
						if !ok || seen[msg.ReceiverUserID] {
							t.Fatalf("unexpected or duplicate email: %+v", msg)
						}
						seen[msg.ReceiverUserID] = true
						receiver, _, _ := users.GetByUserID(context.Background(), msg.ReceiverUserID)
						raw := msg.NewCommentTemplateRawData
						if msg.ReceiverEmail != receiver.EMail || msg.ReceiverLang != receiver.Language || raw == nil {
							t.Fatalf("wrong recipient/payload: %+v", msg)
						}
						if raw.IsMention != (action == constant.NotificationMentionYou) {
							t.Fatalf("wrong notification kind: %+v", raw)
						}
						if raw.IsMention && ((answer == nil && raw.AnswerID != "") || (answer != nil && raw.AnswerID != answer.ID)) {
							t.Fatalf("wrong answer context: %+v", raw)
						}
						if raw.QuestionID != question.ID || raw.QuestionTitle != question.Title || raw.CommentID != comment.ID || raw.CommentSummary != "A comment" || raw.CommentUserDisplayName != "Author" || raw.UnsubscribeCode == "" {
							t.Fatalf("wrong comment context: %+v", raw)
						}
					}
				})
			}
		})
	}
}

func TestMentionEmailAuthorLookup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		missing   bool
		blank     bool
		lookupErr error
		wantMail  int
	}{
		{name: "author fetched once", wantMail: 2},
		{name: "failed author lookup", lookupErr: errors.New("author lookup failed")},
		{name: "missing author", missing: true},
		{name: "empty author display name", blank: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &mentionUsers{calls: map[string]int{}, authorErr: tc.lookupErr, users: map[string]*entity.User{
				"recipient": {ID: "102", Username: "recipient", EMail: "recipient@example.test", Status: entity.UserStatusAvailable},
				"other":     {ID: "106", Username: "other", EMail: "other@example.test", Status: entity.UserStatusAvailable},
			}}
			if !tc.missing {
				name := "Author"
				if tc.blank {
					name = ""
				}
				users.users["author"] = &entity.User{ID: "101", Username: "author", DisplayName: name, Status: entity.UserStatusAvailable}
			}
			site := mock.NewMockSiteInfoCommonService(gomock.NewController(t))
			site.EXPECT().FormatAvatar(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&schema.AvatarInfo{}).AnyTimes()
			inbox := &mentionQueue[*schema.NotificationMsg]{}
			external := &mentionQueue[*schema.ExternalNotificationMsg]{}
			service := &ReviewService{userRepo: users, userCommon: usercommon.NewUserCommon(users, nil, nil, site), notificationQueueService: inbox, externalNotificationQueueService: external}
			obj := &schema.SimpleObjectInfo{ObjectType: constant.QuestionObjectType, QuestionID: "10010000000000001", Title: "Question", QuestionStatus: entity.QuestionStatusAvailable}
			service.notificationMention(context.Background(), []string{"recipient", "other"}, "10070000000000001", "101", map[string]bool{}, obj, "Comment")
			if len(inbox.messages) != 2 {
				t.Fatalf("author lookup must not suppress inbox notifications: %d", len(inbox.messages))
			}
			if len(external.messages) != tc.wantMail {
				t.Fatalf("queued %d emails, want %d", len(external.messages), tc.wantMail)
			}
			if users.calls["101"] != 1 {
				t.Fatalf("author fetched %d times, want 1", users.calls["101"])
			}
			for _, msg := range external.messages {
				if msg.NewCommentTemplateRawData.CommentUserDisplayName != "Author" {
					t.Fatalf("unattributed mail: %+v", msg.NewCommentTemplateRawData)
				}
			}
		})
	}
}
