package runner

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
)

var errMaxEvents = errors.New("max events reached")

// stream forwards server-sent events as NDJSON lines and ends with one result
// object once --max-events is reached, the deadline passes or the server closes
// the stream.
func (r *Runner) stream(ctx context.Context, eng *engine.Engine, req engine.Request, o Options, result *contract.Result) (*contract.Result, error) {
	op := o.Op
	count := 0
	req.OnEvent = func(ev engine.Event) error {
		count++
		line := &contract.Event{Type: "event", Operation: op.ID, Seq: count}
		payload := ev.Payload
		if m, ok := payload.(map[string]any); ok {
			if name, ok := m["event"].(string); ok {
				line.Event = name
				payload = m["data"]
			}
			if id, ok := m["id"].(string); ok {
				line.ID = id
			}
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		line.Data = data
		if o.OnEvent != nil {
			if err := o.OnEvent(line); err != nil {
				return err
			}
		}
		if o.MaxEvents > 0 && count >= o.MaxEvents {
			return errMaxEvents
		}
		return nil
	}
	if o.MaxEvents > 0 {
		// One extra so the engine stops on our signal, after the last event is out.
		req.MaxEvents = o.MaxEvents + 1
	}
	res, err := eng.Do(ctx, req)
	ended := "server_closed"
	switch {
	case err == nil:
	case errors.Is(err, errMaxEvents):
		ended = "max_events"
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		ended = "deadline"
	default:
		return nil, transportFailure(op, err, "")
	}
	if res != nil {
		r.trace(op, res)
		result.Status = res.Status
		result.RequestID = requestID(res)
		if res.Status < 200 || res.Status > 299 {
			return nil, httpFailure(op, res)
		}
	}
	result.OK = true
	result.Stream = &contract.StreamSummary{Events: count, Ended: ended}
	return result, nil
}
