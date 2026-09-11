package calendar

import (
	"context"
	"sync"

	"golang.org/x/oauth2"
)

var reAuthFlows authFlows

// authFlows lets users start another browser flow while a previous one waits
// in the wrong browser. The first successfully saved result completes all
// overlapping flows; cancelled flows cannot overwrite the winning token.
type authFlows struct {
	mu    sync.Mutex
	round *authRound
}

type authRound struct {
	ctx    context.Context
	cancel context.CancelFunc
	active int
}

func (f *authFlows) run(authorize func(context.Context) (*oauth2.Token, error), save func(*oauth2.Token) error) error {
	f.mu.Lock()
	if f.round == nil || f.round.active == 0 || f.round.ctx.Err() != nil {
		ctx, cancel := context.WithCancel(context.Background())
		f.round = &authRound{ctx: ctx, cancel: cancel}
	}
	round := f.round
	ctx := round.ctx
	round.active++
	f.mu.Unlock()

	tok, err := authorize(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	defer func() {
		round.active--
		if round.active == 0 {
			round.cancel()
		}
	}()
	if ctx.Err() != nil {
		// Another flow has already persisted usable credentials.
		return nil
	}
	if err != nil {
		return err
	}
	if err := save(tok); err != nil {
		return err
	}
	round.cancel()
	return nil
}
