package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"trackstar/internal/events"
)

const (
	// clientHeader carries a random per-tab id on mutating requests; it is
	// echoed in events so the originating tab can skip refetching its own change.
	clientHeader = "X-Trackstar-Client"

	// heartbeat keeps proxies from closing an idle stream (Cloudflare cuts
	// streams idle for 100 s) and lets the server notice dead peers.
	heartbeat = 30 * time.Second
)

// clientID returns the tab id from the request, capped to something sane.
func clientID(r *http.Request) string {
	id := strings.TrimSpace(r.Header.Get(clientHeader))
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

func (s *Server) publish(r *http.Request, typ string, projectID, storyID int64) {
	if s.Events == nil {
		return
	}
	s.Events.Publish(events.Event{Type: typ, ProjectID: projectID, StoryID: storyID, Client: clientID(r)})
}

// handleEvents streams project changes as Server-Sent Events. Events carry no
// payload beyond ids; the client refetches the story list, which is a few KB.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	p, err := s.projectFromPath(r, readAccess)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rc := http.NewResponseController(w)
	// Streams outlive the server-wide WriteTimeout; extend it per write instead.
	if err := rc.SetWriteDeadline(time.Now().Add(heartbeat * 2)); err != nil {
		s.fail(w, r, fmt.Errorf("streaming unsupported: %w", err))
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer
	w.WriteHeader(http.StatusOK)

	send := func(line string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(heartbeat * 2))
		if _, err := fmt.Fprint(w, line); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("retry: 2000\n: connected\n\n") {
		return
	}

	ch, cancel := s.Events.Subscribe(p.ID)
	defer cancel()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	token := sessionToken(r)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			// A signed-out user should not keep receiving a project's events.
			if _, err := s.Auth.Authenticate(r.Context(), token); err != nil {
				return
			}
			if !send(": ping\n\n") {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return // dropped by the hub: the client reconnects and refetches
			}
			data, err := json.Marshal(e)
			if err != nil {
				return
			}
			if !send("event: " + e.Type + "\ndata: " + string(data) + "\n\n") {
				return
			}
		}
	}
}
