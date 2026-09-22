package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"trackstar/internal/apperr"
	"trackstar/internal/user"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxUser
	ctxBearer
)

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func currentUser(ctx context.Context) user.User {
	u, _ := ctx.Value(ctxUser).(user.User)
	return u
}

// viaBearer reports whether the request was authenticated with an API token
// rather than a browser session.
func viaBearer(ctx context.Context) bool {
	b, _ := ctx.Value(ctxBearer).(bool)
	return b
}

// bearerToken returns the API token from an Authorization header, or "".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush and SetWriteDeadline on
// the underlying writer (needed by the SSE stream).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests assigns a request id and writes one structured line per request.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		raw := make([]byte, 8)
		rand.Read(raw)
		id := hex.EncodeToString(raw)
		w.Header().Set("X-Request-ID", id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), ctxRequestID, id)))

		level := slog.LevelInfo
		if r.URL.Path == "/health" {
			level = slog.LevelDebug // polled constantly; keep the log quiet
		}
		s.Logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
			"request_id", id,
			"remote_ip", s.clientIP(r),
		)
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Logger.Error("panic", "value", v, "request_id", requestID(r.Context()), "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// checkOrigin rejects cross-site state-changing requests. Browsers always
// send Origin on those; it must match the public URL or the Host the request
// was addressed to (covers LAN access and the Vite dev proxy). Requests
// without Origin come from non-browser clients, which carry no ambient cookie.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !(u.Host == r.Host || (u.Host == s.PublicURL.Host && u.Scheme == s.PublicURL.Scheme)) {
					writeError(w, http.StatusForbidden, "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser authenticates the request: an Authorization: Bearer API token
// wins over the session cookie, so a script never accidentally acts as
// whoever is signed in to the browser on the same machine. Bearer requests
// carry no password, so they bypass the login rate limiter; a miss is one
// indexed lookup of an HMAC.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var (
			u   user.User
			err error
		)
		if tok := bearerToken(r); tok != "" {
			u, err = s.Auth.AuthenticateToken(ctx, tok)
			ctx = context.WithValue(ctx, ctxBearer, true)
		} else {
			u, err = s.Auth.Authenticate(ctx, sessionToken(r))
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxUser, u)))
	})
}

// requireSession is requireUser for account management: an API token may
// not change passwords, manage accounts or mint further tokens, so a leaked
// token is contained to the project data its owner can reach.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return s.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if viaBearer(r.Context()) {
			s.fail(w, r, apperr.Forbidden("sign in with a browser session to manage accounts and tokens"))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// clientIP returns the address of the real client. Forwarding headers are
// honoured only when the TCP peer is a configured trusted proxy; otherwise
// anyone on the internet could spoof their address.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if !s.isTrusted(host) {
		return host
	}
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		if _, err := netip.ParseAddr(ip); err == nil {
			return ip
		}
	}
	// Walk X-Forwarded-For from the right: the first hop that is not one of
	// our proxies is the client.
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if _, err := netip.ParseAddr(hop); err != nil {
			break
		}
		if !s.isTrusted(hop) {
			return hop
		}
	}
	return host
}

func (s *Server) isTrusted(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range s.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
