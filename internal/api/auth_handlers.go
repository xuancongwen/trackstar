package api

import (
	"net/http"
	"time"

	"tracker/internal/apperr"
	"tracker/internal/auth"
)

const sessionCookie = "tracker_session"

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie writes (or, with an empty token, clears) the cookie. Secure
// follows TRACKER_PUBLIC_URL rather than forwarded headers, so it is correct
// behind a TLS-terminating proxy or tunnel without trusting client input.
func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	c := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.PublicURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL / time.Second),
	}
	if token == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	open, err := s.Auth.RegistrationOpen(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"allow_registration": open, "version": s.Version, "timezone": s.Timezone})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(s.clientIP(r)) {
		s.fail(w, r, apperr.RateLimited("too many attempts, try again in a few minutes"))
		return
	}
	var in auth.RegisterInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Auth.Register(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	token, err := s.Auth.StartSession(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(s.clientIP(r)) {
		s.fail(w, r, apperr.RateLimited("too many attempts, try again in a few minutes"))
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	token, u, err := s.Auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.Logout(r.Context(), sessionToken(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSessionCookie(w, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r.Context()))
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}
