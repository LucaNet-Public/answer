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

package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/base/translator"
	"github.com/apache/answer/internal/entity"
	configrepo "github.com/apache/answer/internal/repo/user_notification_config"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/config"
	"github.com/apache/answer/internal/service/export"
	"github.com/apache/answer/internal/service/mock"
	usercommon "github.com/apache/answer/internal/service/user_common"
	"go.uber.org/mock/gomock"
	"xorm.io/xorm"
)

type mentionEmailConfig struct {
	config.ConfigRepo
	port int
}

func (r mentionEmailConfig) GetConfigByKey(context.Context, string) (*entity.Config, error) {
	value, _ := json.Marshal(export.EmailConfig{FromEmail: "noreply@example.test", FromName: "Answer", SMTPHost: "127.0.0.1", SMTPPort: r.port})
	return &entity.Config{Value: string(value)}, nil
}

type mentionEmailCodes struct {
	export.EmailRepo
	contents []string
}

func (r *mentionEmailCodes) SetCode(_ context.Context, _, _, content string, _ time.Duration) error {
	r.contents = append(r.contents, content)
	return nil
}

type mentionEmailUser struct {
	usercommon.UserRepo
	status int
}

func (r mentionEmailUser) GetByUserID(context.Context, string) (*entity.User, bool, error) {
	return &entity.User{Status: r.status}, true, nil
}

// A local SMTP sink exercises the real template and mail sender. No message can
// leave the test machine, and each connection has a deadline to bound failures.
func mentionSMTP(t *testing.T) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	messages := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		smtp := textproto.NewConn(conn)
		_ = smtp.PrintfLine("220 test SMTP")
		for {
			line, err := smtp.ReadLine()
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				_ = smtp.PrintfLine("250 OK")
			case line == "DATA":
				_ = smtp.PrintfLine("354 Send data")
				body, err := smtp.ReadDotBytes()
				if err != nil {
					return
				}
				messages <- string(body)
				_ = smtp.PrintfLine("250 Accepted")
			case line == "QUIT":
				_ = smtp.PrintfLine("221 Bye")
				return
			default:
				_ = smtp.PrintfLine("500 Unexpected command")
			}
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func TestMentionEmailDeliveryHonoursSavedPreferences(t *testing.T) {
	if _, err := translator.NewTranslator(&translator.I18n{BundleDir: "../../../i18n"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		preference string
		status     int
		mention    bool
		want       bool
	}{
		{"enabled", "enabled", entity.UserStatusAvailable, true, true},
		{"disabled", "disabled", entity.UserStatusAvailable, true, false},
		{"admin-created account without preference", "missing", entity.UserStatusAvailable, true, false},
		{"inactive recipient", "enabled", entity.UserStatusSuspended, true, false},
		{"ordinary comment unchanged", "enabled", entity.UserStatusAvailable, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := xorm.NewEngine("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			db.SetMaxOpenConns(1)
			if err := db.Sync2(new(entity.UserNotificationConfig)); err != nil {
				t.Fatal(err)
			}
			prefs := configrepo.NewUserNotificationConfigRepo(&data.Data{DB: db})
			if tc.preference != "missing" {
				channels := fmt.Sprintf(`[{"key":"email","enable":%t}]`, tc.preference == "enabled")
				if err := prefs.Add(context.Background(), []string{"102"}, string(constant.InboxSource), channels); err != nil {
					t.Fatal(err)
				}
			}
			site := mock.NewMockSiteInfoCommonService(gomock.NewController(t))
			site.EXPECT().GetSiteGeneral(gomock.Any()).Return(&schema.SiteGeneralResp{Name: "Answer", SiteUrl: "https://answer.test"}, nil).AnyTimes()
			site.EXPECT().GetSiteSeo(gomock.Any()).Return(&schema.SiteSeoResp{Permalink: constant.PermalinkQuestionID}, nil).AnyTimes()
			codes := &mentionEmailCodes{}
			port, messages := mentionSMTP(t)
			service := &ExternalNotificationService{userNotificationConfigRepo: prefs, userRepo: mentionEmailUser{status: tc.status},
				emailService: export.NewEmailService(config.NewConfigService(mentionEmailConfig{port: port}), codes, site)}
			raw := &schema.NewCommentTemplateRawData{IsMention: tc.mention, CommentUserDisplayName: "Author", QuestionTitle: "Question title", QuestionID: "10010000000000001", AnswerID: "10020000000000001", CommentID: "10070000000000001", CommentSummary: "A comment", UnsubscribeCode: "test-unsubscribe"}
			// French has no local mention translation: the new subject must fall back to English.
			err = service.Handler(context.Background(), &schema.ExternalNotificationMsg{ReceiverUserID: "102", ReceiverEmail: "recipient@example.test", ReceiverLang: "fr_FR", NewCommentTemplateRawData: raw})
			if err != nil {
				t.Fatal(err)
			}
			if !tc.want {
				if len(codes.contents) != 0 {
					t.Fatal("mail was attempted for an opted-out, unconfigured, or inactive recipient")
				}
				select {
				case <-messages:
					t.Fatal("unexpected SMTP message")
				default:
				}
				return
			}
			if len(codes.contents) != 1 {
				t.Fatalf("saved %d unsubscribe records, want 1", len(codes.contents))
			}
			var unsubscribe schema.EmailCodeContent
			if err := json.Unmarshal([]byte(codes.contents[0]), &unsubscribe); err != nil {
				t.Fatal(err)
			}
			if unsubscribe.UserID != "102" || unsubscribe.Email != "recipient@example.test" || len(unsubscribe.NotificationSources) != 1 || unsubscribe.NotificationSources[0] != constant.InboxSource {
				t.Fatalf("wrong unsubscribe preference: %+v", unsubscribe)
			}
			select {
			case rawMail := <-messages:
				msg, err := mail.ReadMessage(strings.NewReader(rawMail))
				if err != nil {
					t.Fatal(err)
				}
				to, err := msg.Header.AddressList("To")
				if err != nil || len(to) != 1 || to[0].Address != "recipient@example.test" {
					t.Fatalf("wrong recipient: %v, %v", to, err)
				}
				subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
				if err != nil {
					t.Fatal(err)
				}
				if tc.mention && subject != "[Answer] Author mentioned you in a comment" {
					t.Fatalf("wrong mention subject: %q", subject)
				}
				if !tc.mention && strings.Contains(subject, "mentioned you") {
					t.Fatalf("ordinary comment used mention subject: %q", subject)
				}
				var body io.Reader = msg.Body
				if msg.Header.Get("Content-Transfer-Encoding") == "quoted-printable" {
					body = quotedprintable.NewReader(body)
				}
				html, err := io.ReadAll(body)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"A comment", "https://answer.test/questions/" + raw.QuestionID + "/" + raw.AnswerID + "?commentId=" + raw.CommentID, "https://answer.test/users/unsubscribe?code=test-unsubscribe"} {
					if !strings.Contains(string(html), want) {
						t.Fatalf("missing %s in delivered body: %s", strconv.Quote(want), html)
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no SMTP delivery")
			}
		})
	}
}
