package session

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBusPublishSubscribe(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(8)

	bus.Publish(Event{Type: EventNewMessage, Data: "hello"})

	select {
	case ev := <-ch:
		assert.Equal(t, EventNewMessage, ev.Type)
		assert.Equal(t, "hello", ev.Data)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber did not receive event")
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	ch1 := bus.Subscribe(8)
	ch2 := bus.Subscribe(8)
	ch3 := bus.Subscribe(8)

	bus.Publish(Event{Type: EventToolCallStart, Data: "Read"})

	for i, ch := range []<-chan Event{ch1, ch2, ch3} {
		select {
		case ev := <-ch:
			assert.Equal(t, EventToolCallStart, ev.Type, "subscriber %d", i)
			assert.Equal(t, "Read", ev.Data, "subscriber %d", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("subscriber %d did not receive event", i)
		}
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(8)

	// Receive one event.
	bus.Publish(Event{Type: EventNewMessage, Data: "first"})
	ev := <-ch
	require.Equal(t, "first", ev.Data)

	// Unsubscribe.
	bus.Unsubscribe(ch)

	// Channel should be closed.
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unsubscribe")

	// Publishing after unsubscribe should not panic.
	bus.Publish(Event{Type: EventNewMessage, Data: "second"})
}

func TestBusNonBlocking(t *testing.T) {
	bus := NewBus()
	// Buffer of 1 - will fill immediately.
	slow := bus.Subscribe(1)
	fast := bus.Subscribe(16)

	// Fill the slow subscriber's buffer.
	bus.Publish(Event{Type: EventNewMessage, Data: "msg1"})

	// This should NOT block even though slow's buffer is full.
	done := make(chan struct{})
	go func() {
		bus.Publish(Event{Type: EventNewMessage, Data: "msg2"})
		bus.Publish(Event{Type: EventNewMessage, Data: "msg3"})
		close(done)
	}()

	select {
	case <-done:
		// Good - publish did not block.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish blocked on slow subscriber")
	}

	// Slow got only msg1 (buffer was full for msg2, msg3).
	ev := <-slow
	assert.Equal(t, "msg1", ev.Data)

	// Fast got all three.
	var fastMsgs []string
	for range 3 {
		ev := <-fast
		fastMsgs = append(fastMsgs, ev.Data.(string))
	}
	assert.Equal(t, []string{"msg1", "msg2", "msg3"}, fastMsgs)
}

func TestBusConcurrentPublish(t *testing.T) {
	bus := NewBus()
	ch := bus.Subscribe(256)

	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			bus.Publish(Event{Type: EventNewMessage, Data: idx})
		}(i)
	}
	wg.Wait()

	// Drain and count.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, n, count)
}
