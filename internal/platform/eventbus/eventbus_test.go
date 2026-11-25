package eventbus

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
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
	b.SubscribeAll(func(_ context.Context, e kernel.Event) error {
		order = append(order, "first:"+e.EventName())
		return nil
	})
	Subscribe(b, func(context.Context, kernel.MessageCreated) error {
		order = append(order, "typed")
		return nil
	})
	b.SubscribeAll(func(_ context.Context, e kernel.Event) error {
		order = append(order, "last:"+e.EventName())
		return nil
	})

	b.Publish(context.Background(), kernel.MessageCreated{}, kernel.ConversationStatusChanged{})

	require.Equal(t, []string{
		"first:conversation.message_created", "typed", "last:conversation.message_created",
		"first:conversation.status_changed", "last:conversation.status_changed",
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

func TestSubscribeDuringDispatch(t *testing.T) {
	b := newBus()
	var got []int64
	Subscribe(b, func(_ context.Context, e kernel.MessageCreated) error {
		if e.MessageID == 1 {
			Subscribe(b, func(_ context.Context, e kernel.MessageCreated) error {
				got = append(got, e.MessageID)
				return nil
			})
		}
		return nil
	})

	b.Publish(t.Context(), kernel.MessageCreated{MessageID: 1}, kernel.MessageCreated{MessageID: 2})

	require.Equal(t, []int64{2}, got)
}

func TestConcurrentSubscribeAndPublish(t *testing.T) {
	b := newBus()
	var got atomic.Int64
	Subscribe(b, func(context.Context, kernel.MessageCreated) error {
		got.Add(1)
		return nil
	})

	const publishers, perPublisher = 8, 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		for range 20 {
			b.SubscribeAll(func(context.Context, kernel.Event) error { return nil })
		}
	})
	for range publishers {
		wg.Go(func() {
			<-start
			for range perPublisher {
				b.Publish(t.Context(), kernel.MessageCreated{})
			}
		})
	}
	close(start)
	wg.Wait()

	require.EqualValues(t, publishers*perPublisher, got.Load())
}
