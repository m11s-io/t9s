package application

import "context"

// EffectBatch describes independent work. A frontend schedules its effects
// concurrently and submits each result through Update.
type EffectBatch []Effect

func (EffectBatch) applicationMessage() {}

func batch(effects ...Effect) Effect {
	active := make(EffectBatch, 0, len(effects))
	for _, effect := range effects {
		if effect != nil {
			active = append(active, effect)
		}
	}
	switch len(active) {
	case 0:
		return nil
	case 1:
		return active[0]
	default:
		return func(context.Context, Dependencies) Message { return active }
	}
}
