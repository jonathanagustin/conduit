package workflow

import (
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestCancelledLogicalRunCannotCommitAnswers(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	refused := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateAnswer, "cancelled-answer", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
			requireTurnError(t, err, ErrTypeRunCancelled)
			refused = true
			value, err := env.QueryWorkflow(QuerySession)
			if err != nil {
				t.Error(err)
				return
			}
			var session domain.Session
			if err := value.Get(&session); err != nil {
				t.Error(err)
			}
			if len(session.Answers) != 0 || len(session.Events) != 0 {
				t.Errorf("cancelled run committed answers: %#v", session)
			}
			env.CancelWorkflow()
		}}, AnswerRequest{Actor: "alice", RunID: "cancelled", Answers: []domain.Answer{{ID: "question", Value: "answer"}}})
	}, time.Millisecond)
	env.ExecuteWorkflow(SessionWorkflow, Input{Actor: "alice", Carry: &domain.Session{ID: "session", Status: domain.StatusNeedsInput, NeedsInput: []domain.Question{{ID: "question", Type: domain.QuestionString, Required: true}}}, TurnRuns: map[string]string{"cancelled": "cancelled"}})
	if !refused {
		t.Fatal("cancelled answer was not refused")
	}
}
