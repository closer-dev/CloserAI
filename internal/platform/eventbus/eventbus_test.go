package eventbus

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"closerai/internal/kernel"
)

func newBus() *Bus { return New(slog.New(slog.NewTextHandler(io.Discard, nil))) }

func TestSubscribeTyped(t *testing.T) {
	b := newBus()
	var got []kernel.ConversationClosed
	Subscribe(b, func(_ context.Context, e kernel.ConversationClosed) error {
		got = append(got, e)
		return nil
	})
	Subscribe(b, func(_ context.Context, e kernel.MessageCreated) error {
		t.Fatal("подписчик MessageCreated не должен получать ConversationClosed")
		return nil
	})

	ev := kernel.ConversationClosed{TenantID: uuid.New(), ConversationID: uuid.New()}
	b.Publish(context.Background(), ev)

	require.Equal(t, []kernel.ConversationClosed{ev}, got)
}

func TestSubscribeAllAndOrder(t *testing.T) {
	b := newBus()
	var order []string
	Subscribe(b, func(context.Context, kernel.MessageCreated) error { order = append(order, "typed"); return nil })
	b.SubscribeAll(func(_ context.Context, e kernel.Event) error { order = append(order, "all:"+e.EventName()); return nil })

	b.Publish(context.Background(), kernel.MessageCreated{}, kernel.ConversationStatusChanged{})

	require.Equal(t, []string{
		"typed", "all:conversation.message_created",
		"all:conversation.status_changed",
	}, order)
}

func TestHandlerFailureDoesNotStopOthers(t *testing.T) {
	b := newBus()
	calls := 0
	Subscribe(b, func(context.Context, kernel.MessageCreated) error { panic("boom") })
	Subscribe(b, func(context.Context, kernel.MessageCreated) error { return errors.New("fail") })
	Subscribe(b, func(context.Context, kernel.MessageCreated) error { calls++; return nil })

	require.NotPanics(t, func() { b.Publish(context.Background(), kernel.MessageCreated{}) })
	require.Equal(t, 1, calls)
}
