package tui

import (
	"context"
	"sync"
)

type directRequestStamp struct {
	generation uint64
	id         uint64
}

type directRequest struct {
	stamp directRequestStamp
	ctx   context.Context
}

type directRequestSlot struct {
	request directRequest
	cancel  context.CancelFunc
}

type directRequests struct {
	mu     sync.Mutex
	serial uint64
	slots  map[string]directRequestSlot
}

func newDirectRequests() *directRequests {
	return &directRequests{slots: make(map[string]directRequestSlot)}
}

func (r *directRequests) begin(slot string, generation uint64) (directRequest, context.CancelFunc) {
	return r.beginWithContext(slot, generation, context.Background())
}

func (r *directRequests) beginWithContext(slot string, generation uint64, parent context.Context) (directRequest, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	if old, ok := r.slots[slot]; ok && old.cancel != nil {
		old.cancel()
	}
	r.serial++
	request := directRequest{stamp: directRequestStamp{generation: generation, id: r.serial}, ctx: ctx}
	r.slots[slot] = directRequestSlot{request: request, cancel: cancel}
	r.mu.Unlock()
	return request, func() {
		cancel()
		r.mu.Lock()
		if current, ok := r.slots[slot]; ok && current.request.stamp == request.stamp {
			delete(r.slots, slot)
		}
		r.mu.Unlock()
	}
}

func (r *directRequests) current(request directRequestStamp) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, slot := range r.slots {
		if slot.request.stamp == request {
			return slot.request.ctx.Err() == nil
		}
	}
	return false
}

func (r *directRequests) cancelSlot(slot string) {
	r.mu.Lock()
	current, ok := r.slots[slot]
	if ok {
		delete(r.slots, slot)
	}
	r.mu.Unlock()
	if ok && current.cancel != nil {
		current.cancel()
	}
}

func (r *directRequests) cancelAll() {
	r.mu.Lock()
	slots := r.slots
	r.slots = make(map[string]directRequestSlot)
	r.mu.Unlock()
	for _, slot := range slots {
		if slot.cancel != nil {
			slot.cancel()
		}
	}
}
