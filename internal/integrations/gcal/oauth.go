package gcal

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/jameynakama/whatnow/internal/integrations"
)

const calendarScope = "https://www.googleapis.com/auth/calendar.events.readonly"
const tokenEndpoint = "https://oauth2.googleapis.com/token"

func (c *Calendar) Authorize(cfg integrations.Config, redirect, state, verifier string) (string, error) {
	if cfg.Values["method"] != "oauth" || cfg.Values["client_id"] == "" || cfg.Values["client_secret"] == "" {
		return "", errors.New("save your OAuth client ID and client secret first")
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id": {cfg.Values["client_id"]}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {calendarScope}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + query.Encode(), nil
}

func (c *Calendar) Exchange(ctx context.Context, cfg integrations.Config, redirect, code, verifier string) (string, error) {
	tok, err := c.token(ctx, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {cfg.Values["client_id"]}, "client_secret": {cfg.Values["client_secret"]}, "code_verifier": {verifier},
	})
	if err != nil {
		return "", err
	}
	if tok.RefreshToken == "" {
		return "", errors.New("Google did not grant offline access; reconnect and allow calendar access")
	}
	return tok.RefreshToken, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (c *Calendar) token(ctx context.Context, values url.Values) (tokenResponse, error) {
	var tok tokenResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return tok, errors.New("couldn't start Google authorization")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return tok, errors.New("couldn't reach Google authorization; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return tok, errors.New("Google authorization failed; check your OAuth credentials and reconnect")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil || tok.AccessToken == "" {
		return tokenResponse{}, errors.New("couldn't read Google authorization")
	}
	if tok.Scope != "" {
		granted := false
		for _, scope := range strings.Fields(tok.Scope) {
			if scope == calendarScope {
				granted = true
			}
		}
		if !granted {
			return tokenResponse{}, errors.New("Google Calendar read access was not granted; reconnect and allow calendar access")
		}
	}
	return tok, nil
}
