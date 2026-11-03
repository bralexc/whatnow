package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"
)

type oauthFlow struct {
	id      string
	expires time.Time
	finish  func(context.Context, string) error
}

func oauthRandom() string {
	var b [32]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (s *Server) startOAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RedirectURI string `json:"redirect_uri"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, err)
		return
	}
	id := r.PathValue("id")
	callbackPath := "/api/v1/integrations/" + id + "/oauth/callback"
	redirect, err := url.Parse(body.RedirectURI)
	// Only send the authorization code back to this app's host. HTTPS is required off loopback.
	if err != nil || redirect.Host != r.Host || redirect.User != nil || redirect.Path != callbackPath || redirect.RawQuery != "" || redirect.Fragment != "" ||
		(redirect.Scheme != "https" && !(redirect.Scheme == "http" && isLoopback(redirect.Host))) {
		writeJSON(w, http.StatusBadRequest, errorBody{"OAuth redirect must point to this app's callback, using HTTPS unless on localhost"})
		return
	}
	state, verifier := oauthRandom(), oauthRandom()
	target, finish, err := s.Integrations.BeginOAuth(r.Context(), id, redirect.String(), state, verifier)
	if err != nil {
		writeError(w, err)
		return
	}
	s.oauthMu.Lock()
	if s.oauthFlows == nil {
		s.oauthFlows = map[string]oauthFlow{}
	}
	for key, flow := range s.oauthFlows {
		if time.Now().After(flow.expires) {
			delete(s.oauthFlows, key)
		}
	}
	if cookie, err := r.Cookie("whatnow_oauth"); err == nil {
		delete(s.oauthFlows, cookie.Value)
	}
	if len(s.oauthFlows) >= 100 {
		s.oauthMu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, errorBody{"too many sign-in attempts; try again later"})
		return
	}
	s.oauthFlows[state] = oauthFlow{id: id, expires: time.Now().Add(10 * time.Minute), finish: finish}
	s.oauthMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "whatnow_oauth", Value: state, Path: "/api/v1/integrations/", HttpOnly: true,
		Secure: redirect.Scheme == "https", SameSite: http.SameSiteLaxMode, MaxAge: 600})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"url": target})
}

func (s *Server) finishOAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("whatnow_oauth")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		writeJSON(w, http.StatusBadRequest, errorBody{"invalid sign-in state; start again from settings"})
		return
	}
	s.oauthMu.Lock()
	flow, ok := s.oauthFlows[state]
	delete(s.oauthFlows, state)
	s.oauthMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "whatnow_oauth", Path: "/api/v1/integrations/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if !ok || flow.id != r.PathValue("id") || time.Now().After(flow.expires) {
		writeJSON(w, http.StatusBadRequest, errorBody{"sign-in expired; start again from settings"})
		return
	}
	result := "connected"
	if r.URL.Query().Get("error") != "" {
		result = "denied"
	} else if code := r.URL.Query().Get("code"); code == "" {
		result = "failed"
	} else if err := flow.finish(r.Context(), code); err != nil {
		result = "failed"
	}
	http.Redirect(w, r, "/settings/integrations?google="+result, http.StatusSeeOther)
}

func (s *Server) disconnectOAuth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.oauthMu.Lock()
	for key, flow := range s.oauthFlows {
		if flow.id == id {
			delete(s.oauthFlows, key)
		}
	}
	s.oauthMu.Unlock()
	view, err := s.Integrations.DisconnectOAuth(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
