package api

import (
	"net/http"

	"trackstar/internal/auth"
)

// Personal API tokens. These routes require a browser session (see
// requireSession): a token cannot list, create or revoke tokens.

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.Auth.Tokens(r.Context(), currentUser(r.Context()).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// handleCreateToken returns the token secret exactly once, in "token".
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var in auth.CreateTokenInput
	if err := decode(w, r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	t, err := s.Auth.CreateToken(r.Context(), currentUser(r.Context()).ID, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.RevokeToken(r.Context(), currentUser(r.Context()).ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
