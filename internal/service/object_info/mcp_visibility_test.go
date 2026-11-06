package object_info

import (
	"context"
	"testing"

	"github.com/apache/answer/internal/entity"
	answercommon "github.com/apache/answer/internal/service/answer_common"
	questioncommon "github.com/apache/answer/internal/service/question_common"
	"github.com/apache/answer/pkg/uid"
	"github.com/apache/answer/plugin"
)

type questionFixture struct {
	questioncommon.QuestionRepo
	q *entity.Question
}

func (f questionFixture) GetQuestion(_ context.Context, id string) (*entity.Question, bool, error) {
	return f.q, id == f.q.ID, nil
}

type answerFixture struct {
	answercommon.AnswerRepo
	a *entity.Answer
}

func (f answerFixture) GetAnswer(_ context.Context, id string) (*entity.Answer, bool, error) {
	return f.a, id == f.a.ID, nil
}

func TestPluginVisibilityUsesExistingPublishedContentPolicy(t *testing.T) {
	q := &entity.Question{ID: "10010000000000001", UserID: "reader", Status: entity.QuestionStatusAvailable, Show: entity.QuestionShow}
	a := &entity.Answer{ID: "10020000000000001", UserID: "reader", QuestionID: q.ID, Status: entity.AnswerStatusAvailable}
	previous := plugin.PublishedObject
	t.Cleanup(func() { plugin.PublishedObject = previous })
	NewObjService(answerFixture{a: a}, questionFixture{q: q}, nil, nil, nil)
	ctx := context.Background()
	for _, id := range []string{q.ID, a.ID, uid.EnShortID(q.ID)} {
		if ok, err := plugin.PublishedObject(ctx, id); err != nil || !ok {
			t.Fatalf("published object %s: %v %v", id, ok, err)
		}
	}
	q.Status = entity.QuestionStatusClosed
	if ok, err := plugin.PublishedObject(ctx, a.ID); err != nil || !ok {
		t.Fatal("closed published question was hidden")
	}
	for _, state := range []struct{ status, show int }{{entity.QuestionStatusDeleted, entity.QuestionShow}, {entity.QuestionStatusPending, entity.QuestionShow}, {entity.QuestionStatusAvailable, entity.QuestionHide}} {
		q.Status, q.Show = state.status, state.show
		for _, id := range []string{q.ID, a.ID} {
			if ok, err := plugin.PublishedObject(ctx, id); err != nil || ok {
				t.Fatalf("restricted parent exposed %s: %v %v", id, ok, err)
			}
		}
	}
	q.Status, q.Show = entity.QuestionStatusAvailable, entity.QuestionShow
	a.Status = entity.AnswerStatusDeleted
	if ok, err := plugin.PublishedObject(ctx, a.ID); err != nil || ok {
		t.Fatal("deleted answer exposed")
	}
	if ok, _ := plugin.PublishedObject(ctx, "not-an-object"); ok {
		t.Fatal("unknown object allowed")
	}
}
