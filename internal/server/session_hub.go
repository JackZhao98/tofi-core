package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// sessionEvent represents a single SSE event emitted during a chat agent run.
type sessionEvent struct {
	Seq  int    // monotonic, starts at 1
	Type string // SSE event name
	Data any    // JSON-serialisable payload
}

// sessionHub fans out SSE events from a running agent to one or more HTTP
// subscribers. Every event is buffered so that a late subscriber — e.g. a
// browser tab that refreshed while the agent was on a skill-install hold —
// can replay the full stream from any sequence number onwards.
type sessionHub struct {
	mu          sync.Mutex
	subscribers map[int]chan sessionEvent
	nextSubID   int
	events      []sessionEvent
	// Seq of the most recent hold event. Subscribers without an explicit
	// Last-Event-ID are assumed to be resuming AFTER responding to this hold
	// (that is, after calling /continue), so their replay window is clamped
	// past it — otherwise they would receive the already-handled hold again
	// and immediately close the stream.
	lastHoldSeq int
	closed      bool
}

func newSessionHub() *sessionHub {
	return &sessionHub{
		subscribers: make(map[int]chan sessionEvent),
	}
}

// subscribe returns an id (for unsubscribe) and a channel receiving every
// event with Seq > fromSeq. Already-buffered events are replayed first, in
// order; subsequent live events arrive as they are published. When the hub
// closes, the channel is closed.
//
// If fromSeq is less than the most recent hold's seq, it is raised to skip
// past the hold on replay. Callers that connect without a Last-Event-ID
// after the user has already approved the hold would otherwise receive the
// stale hold event and immediately close the stream.
func (h *sessionHub) subscribe(fromSeq int) (int, <-chan sessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	id := h.nextSubID
	h.nextSubID++

	if fromSeq < h.lastHoldSeq {
		fromSeq = h.lastHoldSeq
	}

	// Buffer: enough for replay + a live tail so publishers rarely drop.
	ch := make(chan sessionEvent, len(h.events)+64)
	for _, e := range h.events {
		if e.Seq > fromSeq {
			ch <- e
		}
	}
	if h.closed {
		close(ch)
		return id, ch
	}
	h.subscribers[id] = ch
	return id, ch
}

// unsubscribe removes a subscriber and closes its channel.
func (h *sessionHub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.subscribers[id]
	if !ok {
		return
	}
	delete(h.subscribers, id)
	close(ch)
}

// publish appends an event to the buffer and fans it out to every subscriber.
// The send is non-blocking: slow consumers drop the live event and can catch
// up via replay on reconnect.
func (h *sessionHub) publish(eventType string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	e := sessionEvent{
		Seq:  len(h.events) + 1,
		Type: eventType,
		Data: data,
	}
	h.events = append(h.events, e)
	if eventType == "hold" {
		h.lastHoldSeq = e.Seq
	}
	for id, ch := range h.subscribers {
		select {
		case ch <- e:
		default:
			// Slow consumer: event still lives in the replay buffer, so a
			// reconnect with Last-Event-ID recovers it. Log so we notice if
			// a real deployment regularly hits this path.
			log.Printf("⚠️  sessionHub: subscriber %d dropped %s (seq=%d) — will replay on reconnect", id, eventType, e.Seq)
		}
	}
}

// close marks the hub terminal and closes every subscriber channel so
// forwarders finish their loops cleanly.
func (h *sessionHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subscribers {
		close(ch)
		delete(h.subscribers, id)
	}
}

// forwardHubToSSE streams events from a subscriber channel to an HTTP SSE
// writer until either the hub closes the channel or the caller's context
// signals done. Emits `id:` so clients can reconnect with Last-Event-ID.
//
// When a `hold` event is emitted the stream is intentionally closed: the
// agent is now waiting for a POST /continue response, and keeping the SSE
// open would just hang the client. The client re-subscribes via GET /stream
// after responding.
func forwardHubToSSE(w http.ResponseWriter, flusher http.Flusher, ch <-chan sessionEvent, done <-chan struct{}) {
	// Heartbeat keeps the SSE connection alive during long-running tool
	// executions (notably tofi_sub_agent, which can block for minutes with
	// no chunks emitted). Without this, browsers / Caddy / nginx drop idle
	// SSE after ~60-90s and the frontend renders the assistant message as
	// a "Connection lost" error even though the agent is still working.
	// SSE comments (lines starting with ':') are ignored by EventSource
	// but reset proxy idle timers.
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(e.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Type, b)
			flusher.Flush()
			if e.Type == "hold" {
				return
			}
		case <-heartbeat.C:
			// SSE comment — keeps proxies + browser EventSource alive,
			// not visible to the client's event handlers.
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-done:
			return
		}
	}
}
