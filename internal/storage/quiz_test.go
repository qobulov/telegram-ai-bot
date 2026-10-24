package storage

import (
	"context"
	"testing"

	"telegram-ai-bot/internal/quizfile"
)

var twoQuestions = []quizfile.Question{
	{Text: "Savol bir?", Options: []string{"a", "b"}, Correct: 0},
	{Text: "Savol ikki?", Options: []string{"c", "d", "e"}, Correct: 2},
}

func TestQuizTestIsKeptOnceWhoeverSendsIt(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	id, err := store.SaveQuizTest(ctx, alice, "Iqtisodiyot.doc", twoQuestions)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.SaveQuizTest(ctx, bob, "Iqtisodiyot.pdf", twoQuestions)
	if err != nil || again != id {
		t.Fatalf("again = %d, %v; want the same test %d", again, err, id)
	}
	test, err := store.QuizTest(ctx, id)
	if err != nil || test.OwnerID != alice || test.Title != "Iqtisodiyot.doc" || len(test.Questions) != 2 || test.Questions[1].Options[test.Questions[1].Correct] != "e" {
		t.Errorf("test = %+v, %v", test, err)
	}
}

// A run goes ready, running, scores each question once, and finishes.
func TestQuizRunScoresEachQuestionOnce(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	testID, _ := store.SaveQuizTest(ctx, alice, "T", twoQuestions)

	old, _ := store.NewQuizRun(ctx, testID, alice, alice, 0, 2)
	run, err := store.NewQuizRun(ctx, testID, alice, alice, 0, 2)
	if err != nil || run.Status != QuizReady {
		t.Fatalf("run = %+v, %v", run, err)
	}
	if stale, _ := store.QuizRun(ctx, old.ID, alice); stale.Status != QuizStopped {
		t.Errorf("the older run is %s, want stopped", stale.Status)
	}
	if _, ok, _ := store.StartQuizRun(ctx, run.ID, bob); ok {
		t.Error("someone else started the run")
	}
	if run, ok, err := store.StartQuizRun(ctx, run.ID, alice); !ok || err != nil || run.Status != QuizRunning || run.StartedAt.IsZero() {
		t.Fatalf("start = %+v, %v, %v", run, ok, err)
	}

	if err := store.AskedQuizQuestion(ctx, run.ID, "poll-1", 1); err != nil {
		t.Fatal(err)
	}
	scored, ok, err := store.AnswerQuizQuestion(ctx, "poll-1", 1)
	if !ok || err != nil || scored.Position != 1 || scored.Correct != 1 || scored.PollID != "" {
		t.Fatalf("answer = %+v, %v, %v", scored, ok, err)
	}
	if _, ok, _ := store.AnswerQuizQuestion(ctx, "poll-1", -1); ok {
		t.Error("the end of the quiz's time counted after its answer")
	}
	_ = store.AskedQuizQuestion(ctx, run.ID, "poll-2", 0)
	if scored, _, _ := store.AnswerQuizQuestion(ctx, "poll-2", -1); scored.Skipped != 1 || scored.MissedInRow != 1 || scored.Position != 2 {
		t.Errorf("missed = %+v", scored)
	}

	done, ok, err := store.EndQuizRun(ctx, run.ID, alice, QuizDone)
	if !ok || err != nil || done.Status != QuizDone || done.FinishedAt.Before(done.StartedAt) {
		t.Errorf("done = %+v, %v, %v", done, ok, err)
	}
	if _, ok, _ := store.ActiveQuizRun(ctx, alice); ok {
		t.Error("a finished run is still active")
	}
}

// Only each person's first finished run of a part is ranked.
func TestQuizRankCountsFirstRuns(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	testID, _ := store.SaveQuizTest(ctx, alice, "T", twoQuestions)
	finish := func(user int64, rightAnswers int) {
		t.Helper()
		run, _ := store.NewQuizRun(ctx, testID, user, user, 0, 2)
		_, _, _ = store.StartQuizRun(ctx, run.ID, user)
		for i := range 2 {
			poll := string(rune('a'+user%26)) + string(rune('0'+run.ID%10)) + string(rune('0'+i))
			_ = store.AskedQuizQuestion(ctx, run.ID, poll, 0)
			chosen := 1
			if i < rightAnswers {
				chosen = 0
			}
			_, _, _ = store.AnswerQuizQuestion(ctx, poll, chosen)
		}
		if _, ok, err := store.EndQuizRun(ctx, run.ID, user, QuizDone); !ok || err != nil {
			t.Fatalf("finish: %v, %v", ok, err)
		}
	}
	finish(alice, 1)
	finish(bob, 2)
	finish(alice, 2) // a later try does not move alice up

	if rank, total, err := store.QuizRank(ctx, testID, 0, alice); err != nil || rank != 2 || total != 2 {
		t.Errorf("alice = %d of %d, %v; want 2 of 2", rank, total, err)
	}
	if rank, _, _ := store.QuizRank(ctx, testID, 0, bob); rank != 1 {
		t.Errorf("bob = %d, want 1", rank)
	}
}
