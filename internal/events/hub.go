// Package events is the in-process change feed behind the SSE endpoint.
//
// Handlers publish a small "something changed" event after each successful
// write; each open board subscribes to its project and refetches on receipt.
// Nothing is persisted or replayed: a client that reconnects simply reloads.
// This is deliberately single-process; a multi-instance deployment would put
// a PostgreSQL LISTEN/NOTIFY implementation behind the same two methods.
package events

import (
	"sync"
	"sync/atomic"
)

// Event describes one change. Client is the originating tab's id, so that
// tab can ignore its own echo.
type Event struct {
	Type      string `json:"type"` // "stories" | "project"
	ProjectID int64  `json:"project_id"`
	StoryID   int64  `json:"story_id,omitempty"`
	Client    string `json:"client,omitempty"`
}

// bufferSize events may queue per subscriber before it is considered stuck.
const bufferSize = 16

type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[uint64]chan Event
	next uint64
	// Dropped counts subscribers closed because they could not keep up.
	Dropped atomic.Int64
}

func NewHub() *Hub { return &Hub{subs: map[int64]map[uint64]chan Event{}} }

// Subscribe returns a channel of events for projectID and a cancel function
// that must be called when the subscriber goes away. The channel is closed
// by the hub if the subscriber falls behind; the subscriber should then
// treat it like a disconnect.
func (h *Hub) Subscribe(projectID int64) (<-chan Event, func()) {
	ch := make(chan Event, bufferSize)
	h.mu.Lock()
	id := h.next
	h.next++
	if h.subs[projectID] == nil {
		h.subs[projectID] = map[uint64]chan Event{}
	}
	h.subs[projectID][id] = ch
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if _, ok := h.subs[projectID][id]; ok {
				delete(h.subs[projectID], id)
				close(ch)
			}
			if len(h.subs[projectID]) == 0 {
				delete(h.subs, projectID)
			}
		})
	}
}

// Publish delivers e to every subscriber of e.ProjectID without blocking.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, ch := range h.subs[e.ProjectID] {
		select {
		case ch <- e:
		default:
			// Stuck subscriber: drop it rather than stall every writer.
			delete(h.subs[e.ProjectID], id)
			close(ch)
			h.Dropped.Add(1)
		}
	}
	if len(h.subs[e.ProjectID]) == 0 {
		delete(h.subs, e.ProjectID)
	}
}

// Subscribers reports how many streams are open for projectID (0 = all).
func (h *Hub) Subscribers(projectID int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if projectID != 0 {
		return len(h.subs[projectID])
	}
	n := 0
	for _, m := range h.subs {
		n += len(m)
	}
	return n
}
