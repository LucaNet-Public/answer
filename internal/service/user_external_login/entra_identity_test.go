package user_external_login

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
)

const testTenant = "11111111-1111-1111-1111-111111111111"
const testObject = "22222222-2222-2222-2222-222222222222"

type identityRepo struct {
	UserExternalLoginRepo
	aliases []*entity.UserExternalLogin
	rows    []EntraUser
	caller  *entity.UserExternalLogin
	queried []string
}

func (r *identityRepo) GetByExternalID(_ context.Context, provider, id string) (*entity.UserExternalLogin, bool, error) {
	for _, row := range r.aliases {
		if row.Provider == provider && row.ExternalID == id {
			return row, true, nil
		}
	}
	return nil, false, nil
}
func (r *identityRepo) AddUserExternalLogin(_ context.Context, row *entity.UserExternalLogin) error {
	r.aliases = append(r.aliases, row)
	return nil
}
func (r *identityRepo) SaveEntraAlias(ctx context.Context, userID, id, subject string) error {
	for _, row := range r.aliases {
		if row.Provider == entraProvider && row.ExternalID == id && row.UserID == userID {
			row.MetaInfo = subject
			return nil
		}
	}
	return r.AddUserExternalLogin(ctx, &entity.UserExternalLogin{UserID: userID, Provider: entraProvider, ExternalID: id, MetaInfo: subject})
}

func (r *identityRepo) GetByUserID(_ context.Context, provider, id string) (*entity.UserExternalLogin, bool, error) {
	if r.caller != nil && provider == "teams-miner" && id == r.caller.UserID {
		return r.caller, true, nil
	}
	return nil, false, nil
}
func (r *identityRepo) LookupEntraUsers(_ context.Context, ids []string) ([]EntraUser, error) {
	r.queried = ids
	return r.rows, nil
}

func TestStableAliasIsAdditiveAndIdempotent(t *testing.T) {
	setIdentityScope(t)
	metadata, _ := json.Marshal(map[string]any{"lucanet_verified_entra": map[string]string{"tenant_id": testTenant, "object_id": testObject}})
	info := &schema.ExternalLoginUserInfoCache{Provider: "basic", ExternalID: "existing-sub", MetaInfo: string(metadata)}
	repo := &identityRepo{}
	service := &UserExternalLoginService{userExternalLoginRepo: repo}
	for i := 0; i < 2; i++ {
		if err := service.saveEntraIdentity(context.Background(), "123", info); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.aliases) != 1 || repo.aliases[0].ExternalID != testTenant+":"+testObject || info.ExternalID != "existing-sub" {
		t.Fatal("alias changed login or was not idempotent")
	}
	if err := service.saveEntraIdentity(context.Background(), "456", info); err == nil {
		t.Fatal("conflicting account replaced")
	}
	if repo.aliases[0].UserID != "123" {
		t.Fatal("existing mapping reassigned")
	}
	info.Provider = "another-provider"
	if err := service.saveEntraIdentity(context.Background(), "789", info); err != nil {
		t.Fatal(err)
	}
	if len(repo.aliases) != 2 {
		t.Fatal("untrusted provider produced an alias")
	}
}

func TestBatchLookupIsRestrictedAndRejectsAmbiguity(t *testing.T) {
	setIdentityScope(t)
	repo := &identityRepo{caller: &entity.UserExternalLogin{UserID: "miner", ExternalID: "configured-client"}}
	service := &UserExternalLoginService{userExternalLoginRepo: repo}
	if _, err := service.ResolveEntraUsers(context.Background(), "human", []string{testObject}); err == nil {
		t.Fatal("ordinary account allowed")
	}
	originalScope := attributionScope
	attributionScope = func() (string, string, string) { return "", "", "" }
	if _, err := service.ResolveEntraUsers(context.Background(), "miner", []string{testObject}); err == nil {
		t.Fatal("disabled connectors still resolved identities")
	}
	attributionScope = func() (string, string, string) { return testTenant, "teams-miner", "reconfigured-client" }
	if _, err := service.ResolveEntraUsers(context.Background(), "miner", []string{testObject}); err == nil {
		t.Fatal("old service identity survived reconfiguration")
	}
	attributionScope = originalScope
	id := testTenant + ":" + testObject
	repo.rows = []EntraUser{{ExternalID: id, UserID: "123", Username: "Andrei Sverdlov"}}
	rows, err := service.ResolveEntraUsers(context.Background(), "miner", []string{testObject, testObject})
	if err != nil || len(rows) != 1 || rows[0].Username != "Andrei Sverdlov" || len(repo.queried) != 1 {
		t.Fatalf("custom username or batching failed: %v %v", rows, err)
	}
	repo.rows[0].Username = "renamed"
	rows, err = service.ResolveEntraUsers(context.Background(), "miner", []string{testObject})
	if err != nil || rows[0].Username != "renamed" {
		t.Fatal("stale username")
	}
	repo.rows = append(repo.rows, EntraUser{ExternalID: id, UserID: "456", Username: "another"})
	rows, err = service.ResolveEntraUsers(context.Background(), "miner", []string{testObject})
	if err != nil || len(rows) != 0 {
		t.Fatal("ambiguous identity linked")
	}
	for _, ids := range [][]string{{"not-a-uuid"}, make([]string, 101)} {
		if _, err := service.ResolveEntraUsers(context.Background(), "miner", ids); err == nil {
			t.Fatal("invalid batch allowed")
		}
	}
}

func setIdentityScope(t *testing.T) {
	t.Helper()
	old := attributionScope
	attributionScope = func() (string, string, string) { return testTenant, "teams-miner", "configured-client" }
	t.Cleanup(func() { attributionScope = old })
}

func TestAliasFormatsResolveToTheSameCanonicalIdentity(t *testing.T) {
	setIdentityScope(t)
	canonical := "abcdef12-abcd-abcd-abcd-abcdef123456"
	for _, object := range []string{
		canonical, "ABCDEF12-ABCD-ABCD-ABCD-ABCDEF123456",
		"{" + canonical + "}", "urn:uuid:" + canonical,
		"abcdef12abcdabcdabcdabcdef123456",
	} {
		t.Run(object, func(t *testing.T) {
			metadata, _ := json.Marshal(map[string]any{"lucanet_verified_entra": map[string]string{"tenant_id": testTenant, "object_id": object}})
			info := &schema.ExternalLoginUserInfoCache{Provider: "basic", ExternalID: "existing-sub", MetaInfo: string(metadata)}
			repo := &identityRepo{caller: &entity.UserExternalLogin{UserID: "miner", ExternalID: "configured-client"}}
			service := &UserExternalLoginService{userExternalLoginRepo: repo}
			if err := service.saveEntraIdentity(context.Background(), "123", info); err != nil {
				t.Fatal(err)
			}
			if len(repo.aliases) != 1 {
				t.Fatal("alias was not recorded")
			}
			repo.rows = []EntraUser{{ExternalID: repo.aliases[0].ExternalID, UserID: "123", Username: "custom-name"}}
			rows, err := service.ResolveEntraUsers(context.Background(), "miner", []string{canonical})
			if err != nil || len(rows) != 1 || rows[0].Username != "custom-name" {
				t.Fatalf("stored identity does not resolve canonically: %v %v", rows, err)
			}
		})
	}
}
