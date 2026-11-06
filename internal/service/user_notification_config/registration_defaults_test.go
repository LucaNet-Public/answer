package user_notification_config_test

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	repo "github.com/apache/answer/internal/repo/user_notification_config"
	"github.com/apache/answer/internal/schema"
	service "github.com/apache/answer/internal/service/user_notification_config"
	"xorm.io/xorm"
)

func TestRegistrationNotificationDefaultsAndOptOut(t *testing.T) {
	db, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := db.Sync2(new(entity.UserNotificationConfig)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := repo.NewUserNotificationConfigRepo(&data.Data{DB: db})
	s := service.NewUserNotificationConfigService(nil, r)
	// An existing account with no followed-tag preference must remain opted out.
	if err := r.Add(ctx, []string{"99"}, string(constant.InboxSource), `[{"key":"email","enable":true}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultUserNotificationConfig(ctx, []string{"101", "102"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"101", "102"} {
		config, err := s.GetUserNotificationConfig(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !config.Inbox.Enable || !config.AllNewQuestionForFollowingTags.Enable || config.AllNewQuestion.Enable {
			t.Fatalf("unexpected registration defaults for %s: %+v", id, config)
		}
		if config.AllNewQuestionForFollowingTags.Key != constant.EmailChannel {
			t.Fatalf("followed-tag notifications use the wrong channel: %+v", config)
		}
	}
	subscribers, err := r.GetByUsersAndSource(ctx, []string{"99", "101", "102"}, constant.AllNewQuestionForFollowingTagsSource)
	if err != nil || len(subscribers) != 2 {
		t.Fatalf("new users must be eligible for followed-tag delivery: %v, %v", subscribers, err)
	}
	config, err := s.GetUserNotificationConfig(ctx, "101")
	if err != nil {
		t.Fatal(err)
	}
	config.AllNewQuestionForFollowingTags.Enable = false
	if err := s.UpdateUserNotificationConfig(ctx, &schema.UpdateUserNotificationConfigReq{
		UserID: "101", NotificationConfig: config.NotificationConfig,
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"99", "101", "102"} {
		config, err := s.GetUserNotificationConfig(ctx, id)
		if err != nil || config.AllNewQuestionForFollowingTags.Enable != (id == "102") {
			t.Fatalf("existing preferences or opt-out changed for %s: %+v, %v", id, config, err)
		}
	}
	subscribers, err = r.GetByUsersAndSource(ctx, []string{"99", "101", "102"}, constant.AllNewQuestionForFollowingTagsSource)
	if err != nil || len(subscribers) != 1 || subscribers[0].UserID != "102" {
		t.Fatalf("opted-out users must be excluded from delivery: %v, %v", subscribers, err)
	}
}

type failingNotificationRepo struct {
	service.UserNotificationConfigRepo
	calls  int
	failAt int
	err    error
}

func (r *failingNotificationRepo) Add(context.Context, []string, string, string) error {
	r.calls++
	if r.calls == r.failAt {
		return r.err
	}
	return nil
}

func TestRegistrationNotificationDefaultErrors(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		sentinel := errors.New("notification insert failed")
		r := &failingNotificationRepo{failAt: failAt, err: sentinel}
		s := service.NewUserNotificationConfigService(nil, r)
		if err := s.SetDefaultUserNotificationConfig(context.Background(), []string{"101"}); !errors.Is(err, sentinel) {
			t.Fatalf("insert %d error was lost: %v", failAt, err)
		}
		if r.calls != failAt {
			t.Fatalf("initialization continued after insert %d failed: %d calls", failAt, r.calls)
		}
	}
}
