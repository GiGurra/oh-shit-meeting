package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func awaitAuth(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authentication did not resume after another flow succeeded")
	}
}

func TestAuthSuccessReleasesOverlappingFlows(t *testing.T) {
	var flows authFlows
	started := make(chan struct{}, 2)
	done := make(chan error, 2)
	saves := 0
	save := func(tok *oauth2.Token) error {
		saves++
		if tok.AccessToken != "winner" {
			t.Error("late flow overwrote winning token")
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		go func() {
			done <- flows.run(func(ctx context.Context) (*oauth2.Token, error) {
				started <- struct{}{}
				<-ctx.Done()
				// Even a callback which finishes exchanging after cancellation must
				// not overwrite the token already saved by the successful flow.
				return &oauth2.Token{AccessToken: "late"}, nil
			}, save)
		}()
	}
	<-started
	<-started
	if err := flows.run(func(context.Context) (*oauth2.Token, error) {
		return &oauth2.Token{AccessToken: "winner"}, nil
	}, save); err != nil {
		t.Fatal(err)
	}
	awaitAuth(t, done)
	awaitAuth(t, done)
	if saves != 1 {
		t.Fatalf("saved %d tokens, want 1", saves)
	}
}

func TestAuthFailureLeavesOtherFlowWaiting(t *testing.T) {
	for _, saveFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "exchange", true: "save"}[saveFails], func(t *testing.T) {
			var flows authFlows
			started := make(chan context.Context, 1)
			done := make(chan error, 1)
			go func() {
				done <- flows.run(func(ctx context.Context) (*oauth2.Token, error) {
					started <- ctx
					<-ctx.Done()
					return nil, ctx.Err()
				}, func(*oauth2.Token) error { return nil })
			}()
			ctx := <-started
			failure := errors.New("failed")
			err := flows.run(func(context.Context) (*oauth2.Token, error) {
				if !saveFails {
					return nil, failure
				}
				return &oauth2.Token{}, nil
			}, func(*oauth2.Token) error { return failure })
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v", err)
			}
			if ctx.Err() != nil {
				t.Error("failed authentication cancelled the waiting flow")
			}
			if err := flows.run(func(context.Context) (*oauth2.Token, error) {
				return &oauth2.Token{}, nil
			}, func(*oauth2.Token) error { return nil }); err != nil {
				t.Fatal(err)
			}
			awaitAuth(t, done)
		})
	}
}

func TestNewAuthAfterSuccessStartsFreshRound(t *testing.T) {
	var flows authFlows
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- flows.run(func(ctx context.Context) (*oauth2.Token, error) {
			close(started)
			<-ctx.Done()
			<-release
			return nil, ctx.Err()
		}, func(*oauth2.Token) error { return nil })
	}()
	<-started
	saves := 0
	for i := 0; i < 2; i++ {
		err := flows.run(func(ctx context.Context) (*oauth2.Token, error) {
			if ctx.Err() != nil {
				t.Error("new flow inherited cancellation")
			}
			return &oauth2.Token{}, nil
		}, func(*oauth2.Token) error { saves++; return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	awaitAuth(t, done)
	if saves != 2 {
		t.Fatalf("saved %d tokens, want 2", saves)
	}
}

// Reproduce the reported sequence with real callback listeners and a local
// token endpoint: leave the automatic browser untouched, finish the UI flow.
func TestBrowserAuthSecondCallbackReleasesFirstListener(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"winner","token_type":"Bearer","expires_in":3600}`)
	}))
	defer endpoint.Close()
	var flows authFlows
	urls := make(chan string, 2)
	done := make(chan error, 2)
	saved := ""
	start := func() {
		go func() {
			cfg := &oauth2.Config{ClientID: "test", Endpoint: oauth2.Endpoint{
				AuthURL: endpoint.URL + "/auth", TokenURL: endpoint.URL + "/token",
			}}
			done <- flows.run(func(ctx context.Context) (*oauth2.Token, error) {
				return browserAuthToken(ctx, cfg, func(authURL string) { urls <- authURL })
			}, func(tok *oauth2.Token) error { saved = tok.AccessToken; return nil })
		}()
	}
	callback := func() string {
		t.Helper()
		select {
		case raw := <-urls:
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			return u.Query().Get("redirect_uri")
		case <-time.After(time.Second):
			t.Fatal("browser did not open")
			return ""
		}
	}
	start()
	first := callback()
	start()
	second := callback()
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(second + "?code=valid")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	awaitAuth(t, done)
	awaitAuth(t, done)
	if saved != "winner" {
		t.Fatalf("saved token = %q", saved)
	}
	for _, cb := range []string{first, second} {
		resp, err := client.Get(cb)
		if err == nil {
			resp.Body.Close()
			t.Error("completed flow left its callback listener open")
		}
	}
}
