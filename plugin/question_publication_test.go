package plugin

import (
	"context"
	"testing"
)

type invitationTestProvider struct{ calls int }

func (*invitationTestProvider) Info() Info { return Info{SlugName: "publication_hook_test"} }
func (p *invitationTestProvider) QuestionPublished(context.Context, string) error {
	p.calls++
	return nil
}
func TestQuestionInvitationProviderRespectsEnablement(t *testing.T) {
	p := &invitationTestProvider{}
	registerQuestionPublication(p)
	defer StatusManager.Enable(p.Info().SlugName, false)
	invoke := func() error {
		return NotifyQuestionPublished(context.Background(), "1")
	}
	if err := invoke(); err != nil || p.calls != 0 {
		t.Fatal(p.calls, err)
	}
	StatusManager.Enable(p.Info().SlugName, true)
	if err := invoke(); err != nil || p.calls != 1 {
		t.Fatal(p.calls, err)
	}
	StatusManager.Enable(p.Info().SlugName, false)
	if err := invoke(); err != nil || p.calls != 1 {
		t.Fatal(p.calls, err)
	}
}
