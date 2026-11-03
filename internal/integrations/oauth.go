package integrations

import (
	"context"
	"fmt"
	"github.com/jameynakama/whatnow/internal/service"
	"github.com/zalando/go-keyring"
)

// OAuth keeps token exchange on the server. The refresh_token is a hidden secret field.
type OAuth interface {
	Authorize(Config, string, string, string) (string, error)
	Exchange(context.Context, Config, string, string, string) (string, error)
}

func (r *Runner) BeginOAuth(ctx context.Context, id, redirect, state, verifier string) (string, func(context.Context, string) error, error) {
	in, err := r.find(id)
	if err != nil {
		return "", nil, err
	}
	oauth, ok := in.(OAuth)
	if !ok {
		return "", nil, fmt.Errorf("%w: this integration has no OAuth connection", service.ErrInvalid)
	}
	saved, err := load(ctx, r.db, id)
	if err != nil {
		return "", nil, err
	}
	cfg, err := config(in, saved)
	if err != nil {
		return "", nil, err
	}
	target, err := oauth.Authorize(cfg, redirect, state, verifier)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s", service.ErrInvalid, err)
	}
	finish := func(ctx context.Context, code string) error {
		current, err := load(ctx, r.db, id)
		if err != nil {
			return err
		}
		currentCfg, err := config(in, current)
		if err != nil {
			return err
		}
		for _, key := range []string{"method", "client_id", "client_secret"} {
			if cfg.Values[key] != currentCfg.Values[key] {
				return fmt.Errorf("%w: connection settings changed; start sign-in again", service.ErrInvalid)
			}
		}
		token, err := oauth.Exchange(ctx, cfg, redirect, code, verifier)
		if err != nil {
			return err
		}
		return keyring.Set(keychainService, account(id, "", "refresh_token"), token)
	}
	return target, finish, nil
}

// DisconnectOAuth removes local access. Users can also revoke it in their Google account.
func (r *Runner) DisconnectOAuth(ctx context.Context, id string) (View, error) {
	in, err := r.find(id)
	if err != nil {
		return View{}, err
	}
	if _, ok := in.(OAuth); !ok {
		return View{}, fmt.Errorf("%w: this integration has no OAuth connection", service.ErrInvalid)
	}
	if err := DeleteSecrets([]string{account(id, "", "refresh_token")}); err != nil {
		return View{}, err
	}
	return r.view(ctx, in)
}
