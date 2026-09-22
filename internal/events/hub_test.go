package events

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan Event) (Event, bool) {
	t.Helper()
	select {
	case e, ok := <-ch:
		return e, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}, false
	}
}

func TestFanOutIsPerProject(t *testing.T) {
	h := NewHub()
	a, cancelA := h.Subscribe(1)
	b, cancelB := h.Subscribe(1)
	other, cancelOther := h.Subscribe(2)
	defer cancelA()
	defer cancelB()
	defer cancelOther()

	h.Publish(Event{Type: "stories", ProjectID: 1, StoryID: 7, Client: "tab-1"})
	for _, ch := range []<-chan Event{a, b} {
		e, ok := recv(t, ch)
		if !ok || e.StoryID != 7 || e.Client != "tab-1" {
			t.Fatalf("got %+v, %v", e, ok)
		}
	}
	select {
	case e := <-other:
		t.Fatalf("project 2 received %+v", e)
	default:
	}
	if h.Subscribers(0) != 3 || h.Subscribers(1) != 2 {
		t.Fatalf("subscribers = %d / %d", h.Subscribers(0), h.Subscribers(1))
	}
}

func TestCancelClosesAndIsIdempotent(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Subscribe(1)
	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed")
	}
	if h.Subscribers(0) != 0 {
		t.Fatal("subscriber still registered")
	}
	h.Publish(Event{ProjectID: 1}) // must not panic on the closed channel
}

func TestSlowSubscriberIsDropped(t *testing.T) {
	h := NewHub()
	slow, cancelSlow := h.Subscribe(1)
	fast, cancelFast := h.Subscribe(1)
	defer cancelSlow()
	defer cancelFast()

	for i := 0; i <= bufferSize; i++ {
		h.Publish(Event{ProjectID: 1, StoryID: int64(i)})
		if i < bufferSize {
			<-fast
		}
	}
	// slow never read: it has bufferSize queued events and was closed on the next publish.
	n := 0
	for range slow {
		n++
	}
	if n != bufferSize {
		t.Fatalf("slow subscriber received %d events before being dropped, want %d", n, bufferSize)
	}
	if h.Dropped.Load() != 1 || h.Subscribers(1) != 1 {
		t.Fatalf("dropped = %d, subscribers = %d", h.Dropped.Load(), h.Subscribers(1))
	}
	if e, ok := recv(t, fast); !ok || e.StoryID != bufferSize {
		t.Fatalf("fast subscriber got %+v, %v", e, ok)
	}
}
