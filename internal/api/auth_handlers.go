package api

import (
	"net/http"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/auth"
	"trackstar/internal/user"
)

const sessionCookie = "trackstar_session"

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// setSessionCookie writes (or, with an empty token, clears) the cookie. Secure
// follows TRACKSTAR_PUBLIC_URL rather than forwarded headers, so it is correct
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

// handleUpdateMe changes the caller's display name and/or password.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName     *string `json:"display_name"`
		CurrentPassword string  `json:"current_password"`
		NewPassword     string  `json:"new_password"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	me := currentUser(r.Context())
	if in.NewPassword != "" {
		if err := s.Auth.ChangePassword(r.Context(), me.ID, in.CurrentPassword, in.NewPassword, sessionToken(r)); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if in.DisplayName != nil {
		u, err := s.Users.Rename(r.Context(), me.ID, *in.DisplayName)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		me = u
	}
	writeJSON(w, http.StatusOK, me)
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !currentUser(r.Context()).IsAdmin {
		s.fail(w, r, apperr.Forbidden("administrators only"))
		return false
	}
	return true
}

// handleUpdateUser lets an administrator rename, promote/demote or
// (de)activate an account.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in user.AdminUpdate
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Users.Update(r.Context(), currentUser(r.Context()).ID, id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// handleSetUserPassword is the administrator's password reset: the new
// password is set and the user is signed out everywhere.
func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.SetPasswordByID(r.Context(), id, in.Password); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
