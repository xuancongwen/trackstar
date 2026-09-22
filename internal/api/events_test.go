package api

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"
)

// stream opens the SSE endpoint with c's cookies and returns a line reader.
func (c *client) stream(t *testing.T, path string) (*bufio.Reader, func()) {
	t.Helper()
	req, _ := http.NewRequest("GET", c.base+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: status %d, content-type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return bufio.NewReader(resp.Body), func() { resp.Body.Close() }
}

// nextEvent reads until an "event:" block and returns its name and data.
func nextEvent(t *testing.T, r *bufio.Reader) (string, string) {
	t.Helper()
	done := make(chan [2]string, 1)
	go func() {
		var name, data string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				done <- [2]string{"error", err.Error()}
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && name != "":
				done <- [2]string{name, data}
				return
			}
		}
	}()
	select {
	case ev := <-done:
		return ev[0], ev[1]
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
		return "", ""
	}
}

func TestEventsStreamDeliversChanges(t *testing.T) {
	srv, ts := newServer(t, true)
	c := newClient(t, ts)
	c.register("sam@example.com")
	c.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Apollo"}, nil)
	c.must(http.StatusCreated, "POST", "/api/projects", map[string]any{"name": "Other"}, nil)

	r, closeStream := c.stream(t, "/api/projects/apollo/events")
	defer closeStream()
	// The subscription is registered before the first bytes are written, so
	// once the preamble has arrived nothing can be missed.
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, "retry:") {
		t.Fatalf("preamble = %q", line)
	}
	if srv.Events.Subscribers(1) != 1 {
		t.Fatalf("subscribers = %d", srv.Events.Subscribers(1))
	}

	c.must(http.StatusCreated, "POST", "/api/projects/1/stories", map[string]any{"title": "A"}, nil)
	name, data := nextEvent(t, r)
	if name != "stories" || !strings.Contains(data, `"project_id":1`) || !strings.Contains(data, `"story_id":1`) {
		t.Fatalf("event = %s %s", name, data)
	}

	// Another project's change must not arrive; the next event we see is our move.
	c.must(http.StatusCreated, "POST", "/api/projects/2/stories", map[string]any{"title": "elsewhere"}, nil)
	if status := c.do("POST", "/api/stories/1/move", map[string]any{"section": "backlog"}, nil, clientHeader, "tab-42"); status != http.StatusOK {
		t.Fatalf("move: %d", status)
	}
	name, data = nextEvent(t, r)
	if name != "stories" || !strings.Contains(data, `"story_id":1`) || !strings.Contains(data, `"client":"tab-42"`) {
		t.Fatalf("event = %s %s", name, data)
	}

	c.must(http.StatusNoContent, "DELETE", "/api/stories/1", nil, nil)
	if name, data = nextEvent(t, r); name != "stories" || !strings.Contains(data, `"story_id":1`) {
		t.Fatalf("delete event = %s %s", name, data)
	}
	c.must(http.StatusOK, "PATCH", "/api/projects/1", map[string]any{"name": "Apollo 2"}, nil)
	if name, _ = nextEvent(t, r); name != "project" {
		t.Fatalf("project event = %s", name)
	}

	closeStream()
	deadline := time.Now().Add(2 * time.Second)
	for srv.Events.Subscribers(1) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := srv.Events.Subscribers(1); n != 0 {
		t.Fatalf("subscribers after close = %d", n)
	}
}

func TestEventsStreamRequiresAuthAndProject(t *testing.T) {
	_, ts := newServer(t, true)
	anon := newClient(t, ts)
	anon.must(http.StatusUnauthorized, "GET", "/api/projects/1/events", nil, nil)
	c := newClient(t, ts)
	c.register("sam@example.com")
	c.must(http.StatusNotFound, "GET", "/api/projects/nope/events", nil, nil)
}
