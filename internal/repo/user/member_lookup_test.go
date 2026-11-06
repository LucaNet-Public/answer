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
	defer db.Close()
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
