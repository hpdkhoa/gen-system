package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Event struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type Broker struct {
	mu      sync.RWMutex
	clients map[chan string]bool
}

func NewBroker() *Broker {
	return &Broker{clients: make(map[chan string]bool)}
}

func (b *Broker) subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.clients[ch] = true
	b.mu.Unlock()
	return ch
}

func (b *Broker) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *Broker) Publish(eventType string, payload interface{}) {
	data, _ := json.Marshal(Event{Type: eventType, Payload: payload})
	msg := fmt.Sprintf("data: %s\n\n", data)
	b.mu.RLock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
		}
	}
	b.mu.RUnlock()
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := b.subscribe()
	defer b.unsubscribe(ch)

	fmt.Fprintf(w, "data: {\"type\":\"connected\"}\n\n")
	fl.Flush()

	for {
		select {
		case msg, open := <-ch:
			if !open {
				return
			}
			fmt.Fprint(w, msg)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
