package application

import (
	"context"
	"errors"
	"sync"
	"time"
)

// requestOwner owns work for one session. Completed and canceled slots retain
// their latest ID so a delayed older effect cannot restart a superseded RPC.
type requestOwner struct {
	mu      sync.Mutex
	session context.Context
	slots   map[string]requestSlot
	stop    context.CancelFunc
}

type requestSlot struct {
	id     uint64
	cancel context.CancelFunc
}

func newRequestOwner(ctx context.Context) *requestOwner {
	ctx, cancel := context.WithCancel(ctx)
	return &requestOwner{session: ctx, stop: cancel, slots: make(map[string]requestSlot)}
}

// begin replaces the work for key. A positive timeout bounds a unary request;
// zero adds no deadline. Call finish when unary work ends or a stream open fails.
// A successful stream open keeps the slot until cancel, replacement, or close.
func (o *requestOwner) begin(ctx context.Context, key string, id uint64, timeout time.Duration) (context.Context, func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := o.session.Err(); err != nil {
		return nil, nil, err
	}
	prior, exists := o.slots[key]
	if exists && id <= prior.id {
		return nil, nil, errors.New("request superseded")
	}
	if prior.cancel != nil {
		prior.cancel()
	}
	var requestCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		requestCtx, cancel = context.WithCancel(ctx)
	}
	stopSession := context.AfterFunc(o.session, cancel)
	release := func() { stopSession(); cancel() }
	o.slots[key] = requestSlot{id: id, cancel: release}
	finish := func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		slot := o.slots[key]
		if slot.id == id && slot.cancel != nil {
			slot.cancel()
			slot.cancel = nil
			o.slots[key] = slot
		}
	}
	return requestCtx, finish, nil
}

func (o *requestOwner) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stop()
	for key, slot := range o.slots {
		if slot.cancel != nil {
			slot.cancel()
			slot.cancel = nil
			o.slots[key] = slot
		}
	}
}

// cancelThrough also invalidates work that was scheduled but has not begun.
func (o *requestOwner) cancelThrough(key string, id uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	slot := o.slots[key]
	if slot.id > id {
		return
	}
	if slot.cancel != nil {
		slot.cancel()
	}
	o.slots[key] = requestSlot{id: id}
}
