package application

import (
	"context"
	"errors"
	"io"

	"github.com/m11s-io/t9s/internal/domain"
	"github.com/m11s-io/t9s/internal/ports"
)

type ToggleEventFollow struct{}

func (ToggleEventFollow) applicationMessage() {}

type StopEventFollow struct{}

func (StopEventFollow) applicationMessage() {}

type eventFollowOpened struct {
	Generation, StreamGeneration uint64
	Stream                       ports.EventStream
	Err                          error
}

func (eventFollowOpened) applicationMessage() {}

type eventFollowBatch struct {
	Generation, StreamGeneration uint64
	Batch                        domain.EventSet
	Err                          error
}

func (eventFollowBatch) applicationMessage() {}

func updateEventFollow(model Model, message Message) (Model, Effect, bool) {
	switch msg := message.(type) {
	case ToggleEventFollow:
		if model.Events.Following {
			next, effect := stopEventFollow(model)
			return next, effect, true
		}
		model.eventGeneration++
		model.Events.Following = true
		model.Events.FollowAttempted = true
		model.Events.Err = ""
		model.Events.Value.Problems = nil
		model.Events.Status = Ready
		if excess := len(model.Events.Value.Events) - 2000; excess > 0 {
			model.Events.Value.Events = append([]domain.EventSnapshot(nil), model.Events.Value.Events[excess:]...)
		}
		generation, streamGeneration, reader := model.Generation, model.eventGeneration, model.eventReader
		return model, func(ctx context.Context, _ Dependencies) Message {
			if reader == nil {
				return eventFollowOpened{Generation: generation, StreamGeneration: streamGeneration, Err: errors.New("event reader unavailable")}
			}
			stream, err := reader.Open(ctx)
			return eventFollowOpened{Generation: generation, StreamGeneration: streamGeneration, Stream: stream, Err: err}
		}, true
	case StopEventFollow:
		next, effect := stopEventFollow(model)
		return next, effect, true
	case eventFollowOpened:
		if msg.Generation != model.Generation || msg.StreamGeneration != model.eventGeneration || !model.Events.Following {
			return model, closeEventStream(msg.Stream), true
		}
		if msg.Err != nil || msg.Stream == nil {
			model.Events.Following = false

			model.Events.Err = "event stream unavailable"
			model.Events.Status = Partial
			return model, closeEventStream(msg.Stream), true
		}
		model.eventStream = msg.Stream
		return model, readEventFollow(msg.Stream, model.Generation, model.eventGeneration), true
	case eventFollowBatch:
		if msg.Generation != model.Generation || msg.StreamGeneration != model.eventGeneration || !model.Events.Following {
			return model, nil, true
		}
		events := append(model.Events.Value.Events, msg.Batch.Events...)
		if excess := len(events) - 2000; excess > 0 {
			events = append([]domain.EventSnapshot(nil), events[excess:]...)
		}
		model.Events.Value.Events = events
		for _, problem := range msg.Batch.Problems {
			// Keep a single normalized failure per member for this follow session.
			found := false
			for _, old := range model.Events.Value.Problems {
				if old.Node == problem.Node {
					found = true
					break
				}
			}
			if !found {
				model.Events.Value.Problems = append(model.Events.Value.Problems, problem)
			}
		}
		if len(model.Events.Value.Problems) > 0 {
			model.Events.Status = Partial
		}
		if msg.Err != nil {

			model.Events.Following = false
			if !errors.Is(msg.Err, io.EOF) {
				model.Events.Err = "event stream unavailable"
				model.Events.Status = Partial
			}
			stream := model.eventStream
			model.eventStream = nil
			return model, closeEventStream(stream), true
		}
		return model, readEventFollow(model.eventStream, model.Generation, model.eventGeneration), true
	}
	return model, nil, false
}
func stopEventFollow(model Model) (Model, Effect) {

	stream := model.eventStream
	model.eventStream = nil
	model.eventGeneration++
	model.Events.Following = false
	return model, closeEventStream(stream)
}
func closeEventStream(stream ports.EventStream) Effect {
	if stream == nil {
		return nil
	}
	return func(context.Context, Dependencies) Message { _ = stream.Close(); return nil }
}
func readEventFollow(stream ports.EventStream, generation, streamGeneration uint64) Effect {
	return func(ctx context.Context, _ Dependencies) Message {
		batch, err := stream.Next(ctx)
		return eventFollowBatch{Generation: generation, StreamGeneration: streamGeneration, Batch: batch, Err: err}
	}
}
