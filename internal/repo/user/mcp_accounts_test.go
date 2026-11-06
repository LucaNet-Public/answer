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
