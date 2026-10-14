package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"telegram-ai-bot/internal/ai"
)

type fakeGenerator struct {
	err     error
	prompts []string
}

func (f *fakeGenerator) Generate(_ context.Context, prompt string) (ai.Image, error) {
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return ai.Image{}, f.err
	}
	return ai.Image{Data: []byte("PNG"), MimeType: "image/png"}, nil
}

type fakeQuota struct {
	limits   []int
	since    time.Time
	refuse   bool
	released []int64
}

func (f *fakeQuota) ReserveImage(_ context.Context, _, _ int64, since time.Time, limit int) (int64, bool, error) {
	f.limits = append(f.limits, limit)
	f.since = since
	return 42, !f.refuse, nil
}

func (f *fakeQuota) ReleaseImage(_ context.Context, id int64) error {
	f.released = append(f.released, id)
	return nil
}

var tashkent, _ = time.LoadLocation("Asia/Tashkent")

func imageRegistry(t *testing.T, gen *fakeGenerator, quota *fakeQuota) *Registry {
	t.Helper()
	tool := ImageTool(ImageOptions{
		Generator:  gen,
		Quota:      quota,
		DailyLimit: 2,
		Location:   tashkent,
		Now:        func() time.Time { return time.Date(2026, 10, 14, 15, 0, 0, 0, tashkent) },
	})
	return mustRegistry(t, tool)
}

func draw(registry *Registry, ctx context.Context, args string) envelope {
	var env envelope
	_ = json.Unmarshal([]byte(registry.Execute(ctx, userID, "generate_image", args)), &env)
	return env
}

func TestImageIsDrawnIntoTheTurnsSink(t *testing.T) {
	gen, quota := &fakeGenerator{}, &fakeQuota{}
	sink := &Sink{}
	ctx := WithTurn(context.Background(), Caller{UserID: userID, ChatID: -100}, sink)

	env := draw(imageRegistry(t, gen, quota), ctx, `{"prompt":"a cat in sunglasses"}`)

	if !env.OK {
		t.Fatalf("envelope = %+v", env)
	}
	if drawings := sink.Take(); len(drawings) != 1 || string(drawings[0].Data) != "PNG" {
		t.Errorf("sink = %+v", drawings)
	}
	if len(gen.prompts) != 1 || gen.prompts[0] != "a cat in sunglasses" {
		t.Errorf("prompts = %q", gen.prompts)
	}
	if want := time.Date(2026, 10, 14, 0, 0, 0, 0, tashkent); !quota.since.Equal(want) {
		t.Errorf("counted since %v, want midnight in Tashkent", quota.since)
	}
}

func TestOwnersHaveNoDailyLimit(t *testing.T) {
	quota := &fakeQuota{}
	ctx := WithTurn(context.Background(), Caller{UserID: userID, Owner: true}, &Sink{})

	draw(imageRegistry(t, &fakeGenerator{}, quota), ctx, `{"prompt":"x"}`)
	draw(imageRegistry(t, &fakeGenerator{}, quota), WithTurn(context.Background(), Caller{UserID: 5}, &Sink{}), `{"prompt":"x"}`)

	if len(quota.limits) != 2 || quota.limits[0] >= 0 || quota.limits[1] != 2 {
		t.Errorf("limits = %v, want no limit for the owner and 2 for others", quota.limits)
	}
}

func TestDailyLimitIsReportedToTheModel(t *testing.T) {
	gen := &fakeGenerator{}
	ctx := WithTurn(context.Background(), Caller{UserID: userID}, &Sink{})

	env := draw(imageRegistry(t, gen, &fakeQuota{refuse: true}), ctx, `{"prompt":"x"}`)

	if env.OK || env.Error.Code != "daily_limit" || !strings.Contains(env.Error.Message, "2") {
		t.Errorf("envelope = %+v", env)
	}
	if len(gen.prompts) != 0 {
		t.Error("a picture was drawn over the limit")
	}
}

func TestFailedDrawingGivesTheReservationBack(t *testing.T) {
	for name, err := range map[string]error{
		"quota":   &ai.APIError{Status: 429, Message: "You exceeded your current quota"},
		"refusal": errors.Join(ai.ErrNoImage, errors.New("I cannot draw that")),
	} {
		t.Run(name, func(t *testing.T) {
			quota := &fakeQuota{}
			sink := &Sink{}
			ctx := WithTurn(context.Background(), Caller{UserID: userID}, sink)

			env := draw(imageRegistry(t, &fakeGenerator{err: err}, quota), ctx, `{"prompt":"x"}`)

			if env.OK || len(quota.released) != 1 || len(sink.Take()) != 0 {
				t.Errorf("envelope = %+v, released = %v", env, quota.released)
			}
		})
	}
}

func TestImageToolChecksItsArguments(t *testing.T) {
	ctx := WithTurn(context.Background(), Caller{UserID: userID}, &Sink{})
	for _, args := range []string{`{}`, `{"prompt":"  "}`, `{"prompt":"` + strings.Repeat("a", 2001) + `"}`, `{"prompt":"x","n":4}`} {
		if env := draw(imageRegistry(t, &fakeGenerator{}, &fakeQuota{}), ctx, args); env.OK || env.Error.Code != "invalid_arguments" {
			t.Errorf("args %.40s: envelope = %+v", args, env)
		}
	}
}

func TestImageToolNeedsATurn(t *testing.T) {
	env := draw(imageRegistry(t, &fakeGenerator{}, &fakeQuota{}), context.Background(), `{"prompt":"x"}`)
	if env.OK {
		t.Error("drew a picture with nowhere to send it")
	}
}
