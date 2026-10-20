package application

import "sync"

type mutationRegistry struct {
	mu      sync.Mutex
	known   map[string]uint32
	unknown uint32
}

var processMutationRegistry = mutationRegistry{known: make(map[string]uint32)}

func (r *mutationRegistry) acquire(scope string) (func(), error) {
	r.mu.Lock()
	if r.unknown > 0 || scope == "" && len(r.known) > 0 || scope != "" && r.known[scope] > 0 {
		r.mu.Unlock()
		return nil, ErrMutationBusy
	}
	if scope == "" {
		r.unknown++
	} else {
		r.known[scope]++
	}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if scope == "" {
				r.unknown--
			} else if r.known[scope] <= 1 {
				delete(r.known, scope)
			} else {
				r.known[scope]--
			}
			r.mu.Unlock()
		})
	}, nil
}
