package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

const (
	adminCookieSecure = "__Host-tgstrm_session"
	adminCookieLocal  = "tgstrm_session"
	adminSessionTTL   = 24 * time.Hour
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

type adminSession struct {
	ID, UserID, Username, Scope string
	MustChange                  bool
	ExpiresAt                   time.Time
}

func randomBytes(n int) ([]byte, error) { b := make([]byte, n); _, err := rand.Read(b); return b, err }
func tokenHash(token string) []byte     { sum := sha256.Sum256([]byte(token)); return sum[:] }

func hashPassword(password string) (string, error) {
	salt, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 32*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=32768,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory uint32
	var iterations, parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || memory < 19*1024 || iterations < 2 || parallelism < 1 {
		return false
	}
	salt64, key64 := parts[4], parts[5]
	salt, err1 := base64.RawStdEncoding.DecodeString(salt64)
	expected, err2 := base64.RawStdEncoding.DecodeString(key64)
	if err1 != nil || err2 != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (a *API) ensureDefaultAdmin(ctx context.Context) error {
	if a.db == nil {
		return errors.New("database unavailable")
	}
	var count int
	if err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM tgstrm.admin_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword("admin")
	if err != nil {
		return err
	}
	_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.admin_users(id,username,password_hash,must_change_password) VALUES($1,'admin',$2,true) ON CONFLICT DO NOTHING`, uuid.New(), hash)
	if err == nil {
		log.Printf("Cuenta administradora inicial preparada. Usuario: admin (se exigirá cambiar la contraseña inicial)")
	}
	return err
}

func validNewPassword(password, username string) error {
	if len([]byte(password)) < 8 {
		return errors.New("La contraseña debe tener al menos 8 caracteres.")
	}
	if len([]byte(password)) > 128 {
		return errors.New("La contraseña no puede superar 128 bytes.")
	}
	lower := strings.ToLower(password)
	if lower == "admin" || lower == strings.ToLower(username) || lower == "password" || lower == "contraseña" || lower == "123456789012345" {
		return errors.New("Elige una contraseña menos predecible.")
	}
	return nil
}

func loginLockExpired(locked sql.NullTime, now time.Time) bool {
	return !locked.Valid || !now.Before(locked.Time)
}

func (a *API) adminSkipInitialPassword(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil || s.Scope != "password_change" {
		fail(w, http.StatusUnauthorized, "authentication_required", "Inicia sesión para continuar.")
		return
	}
	if _, err = a.db.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET scope='full',last_seen_at=now() WHERE id=$1`, s.ID); err != nil {
		fail(w, http.StatusInternalServerError, "session_failed", "No se pudo abrir la sesión.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "skipped", "next": "/home"})
}

func parseTrustedProxyCIDRs(values []string) ([]*net.IPNet, error) {
	var networks []*net.IPNet
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q", value)
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func (a *API) requestSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, network := range a.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
func (a *API) sessionCookieName(r *http.Request) string {
	if a.requestSecure(r) {
		return adminCookieSecure
	}
	return adminCookieLocal
}
func (a *API) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: a.sessionCookieName(r), Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: a.requestSecure(r), SameSite: http.SameSiteStrictMode})
}
func clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{adminCookieSecure, adminCookieLocal} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: name == adminCookieSecure, SameSite: http.SameSiteStrictMode})
	}
}

func (a *API) readSession(r *http.Request) (*adminSession, error) {
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()
	if db == nil {
		return nil, sql.ErrNoRows
	}
	var cookie *http.Cookie
	for _, name := range []string{adminCookieSecure, adminCookieLocal} {
		if c, e := r.Cookie(name); e == nil {
			cookie = c
			break
		}
	}
	if cookie == nil || len(cookie.Value) < 32 {
		return nil, sql.ErrNoRows
	}
	var s adminSession
	err := db.QueryRowContext(r.Context(), `SELECT s.id::text,s.admin_user_id::text,u.username,s.scope,u.must_change_password,s.expires_at FROM tgstrm.web_sessions s JOIN tgstrm.admin_users u ON u.id=s.admin_user_id WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now()`, tokenHash(cookie.Value)).Scan(&s.ID, &s.UserID, &s.Username, &s.Scope, &s.MustChange, &s.ExpiresAt)
	if err == nil {
		_, _ = db.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET last_seen_at=now() WHERE token_hash=$1 AND last_seen_at<now()-interval '5 minutes'`, tokenHash(cookie.Value))
	}
	return &s, err
}
func (a *API) createSession(ctx context.Context, w http.ResponseWriter, r *http.Request, userID, scope string) (*adminSession, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(adminSessionTTL)
	id := uuid.New()
	ua := sha256.Sum256([]byte(r.UserAgent()))
	_, err = a.db.ExecContext(ctx, `INSERT INTO tgstrm.web_sessions(token_hash,id,admin_user_id,scope,expires_at,user_agent_hash) VALUES($1,$2,$3,$4,$5,$6)`, tokenHash(token), id, userID, scope, expires, ua[:])
	if err != nil {
		return nil, err
	}
	a.setSessionCookie(w, r, token, expires)
	return &adminSession{ID: id.String(), UserID: userID, Scope: scope, ExpiresAt: expires}, nil
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		fail(w, 400, "invalid_request", "Revisa los datos enviados.")
		return false
	}
	return true
}
func (a *API) adminSessionStatus(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		writeJSON(w, 200, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "username": s.Username, "scope": s.Scope, "must_change_password": s.MustChange, "expires_at": s.ExpiresAt})
}

func (a *API) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if !decodeAdminJSON(w, r, &in) {
		return
	}
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()
	if db == nil {
		fail(w, 503, "database_required", "La base de datos no está disponible.")
		return
	}
	var id, username, hash string
	var must bool
	var locked sql.NullTime
	var attempts int
	err := db.QueryRowContext(r.Context(), `SELECT id::text,username,password_hash,must_change_password,locked_until,failed_attempts FROM tgstrm.admin_users WHERE lower(username)=lower($1)`, in.Username).Scan(&id, &username, &hash, &must, &locked, &attempts)
	valid := err == nil && loginLockExpired(locked, time.Now()) && verifyPassword(hash, in.Password)
	if !valid {
		// Do not extend a live lock on every subsequent attempt; otherwise an
		// attacker who keeps guessing could keep the account locked forever.
		if err == nil && loginLockExpired(locked, time.Now()) {
			attempts++
			delay := time.Duration(0)
			if attempts >= 5 {
				delay = 15 * time.Minute
			}
			_, _ = db.ExecContext(r.Context(), `UPDATE tgstrm.admin_users SET failed_attempts=$2,locked_until=CASE WHEN $3::bigint>0 THEN now()+($3||' seconds')::interval ELSE locked_until END,updated_at=now() WHERE id=$1`, id, attempts, int64(delay.Seconds()))
		}
		time.Sleep(250 * time.Millisecond)
		fail(w, 401, "invalid_credentials", "No se pudo iniciar sesión con esos datos.")
		return
	}
	_, _ = db.ExecContext(r.Context(), `UPDATE tgstrm.admin_users SET failed_attempts=0,locked_until=NULL,last_login_at=now(),updated_at=now() WHERE id=$1`, id)
	scope := "full"
	if must {
		scope = "password_change"
	}
	s, err := a.createSession(r.Context(), w, r, id, scope)
	if err != nil {
		fail(w, 500, "session_failed", "No se pudo crear la sesión.")
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "username": username, "scope": s.Scope, "must_change_password": must})
}

func (a *API) adminInitialPassword(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil || s.Scope != "password_change" {
		fail(w, 401, "authentication_required", "Inicia sesión para continuar.")
		return
	}
	var in struct{ Password string }
	if !decodeAdminJSON(w, r, &in) {
		return
	}
	if err = validNewPassword(in.Password, s.Username); err != nil {
		fail(w, 422, "weak_password", err.Error())
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		fail(w, 500, "password_failed", "No se pudo guardar la contraseña.")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_users SET password_hash=$2,must_change_password=false,password_changed_at=now(),updated_at=now() WHERE id=$1`, s.UserID, hash)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET revoked_at=now() WHERE admin_user_id=$1`, s.UserID)
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		fail(w, 500, "password_failed", "No se pudo guardar la contraseña.")
		return
	}
	clearSessionCookies(w)
	writeJSON(w, 200, map[string]string{"status": "changed", "next": "/admin/login"})
}

func (a *API) adminLogout(w http.ResponseWriter, r *http.Request) {
	if s, err := a.readSession(r); err == nil {
		_, _ = a.db.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET revoked_at=now() WHERE id=$1`, s.ID)
	}
	clearSessionCookies(w)
	writeJSON(w, 200, map[string]string{"status": "signed_out"})
}

func recoveryCode() (string, error) {
	b, e := randomBytes(10)
	if e != nil {
		return "", e
	}
	raw := strings.ToUpper(hex.EncodeToString(b))
	return raw[:5] + "-" + raw[5:10] + "-" + raw[10:15] + "-" + raw[15:], nil
}
func (a *API) adminRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()
	if db == nil {
		writeJSON(w, 202, map[string]string{"message": "Si existe una cuenta, el código se mostrará en la consola del servidor."})
		return
	}
	// Serialize requests on the administrator row so simultaneous unauthenticated
	// requests cannot bypass the one-per-minute limit or create multiple live codes.
	tx, err := db.BeginTx(r.Context(), nil)
	if err == nil {
		var id, username string
		err = tx.QueryRowContext(r.Context(), `SELECT id::text,username FROM tgstrm.admin_users ORDER BY created_at LIMIT 1 FOR UPDATE`).Scan(&id, &username)
		if err == nil {
			var recent bool
			err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM tgstrm.admin_recovery_challenges
				WHERE admin_user_id=$1 AND created_at>now()-interval '1 minute')`, id).Scan(&recent)
			if err == nil && !recent {
				var code string
				code, err = recoveryCode()
				if err == nil {
					sum := sha256.Sum256([]byte(strings.ToUpper(strings.ReplaceAll(code, "-", ""))))
					_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_recovery_challenges SET consumed_at=now()
						WHERE admin_user_id=$1 AND consumed_at IS NULL`, id)
					if err == nil {
						_, err = tx.ExecContext(r.Context(), `INSERT INTO tgstrm.admin_recovery_challenges
							(id,admin_user_id,code_hash,expires_at) VALUES($1,$2,$3,now()+interval '10 minutes')`, uuid.New(), id, sum[:])
					}
					if err == nil {
						err = tx.Commit()
						if err == nil {
							log.Printf("RECUPERACIÓN ADMINISTRADORA — usuario: %s — código: %s — caduca en 10 minutos", username, code)
						}
					}
				}
			} else if err == nil {
				err = tx.Commit()
			}
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}
	writeJSON(w, 202, map[string]string{"message": "Si existe una cuenta, el código se mostrará en la consola del servidor."})
}

func (a *API) adminAccount(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		fail(w, 401, "authentication_required", "Inicia sesión para continuar.")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"username": s.Username, "session_expires_at": s.ExpiresAt})
		return
	}
	var in struct{ Username, CurrentPassword, NewPassword string }
	if !decodeAdminJSON(w, r, &in) {
		return
	}
	if !usernamePattern.MatchString(in.Username) {
		fail(w, 422, "invalid_username", "El usuario debe tener entre 3 y 32 caracteres y usar letras, números, punto, guion o guion bajo.")
		return
	}
	var stored string
	if a.db.QueryRowContext(r.Context(), `SELECT password_hash FROM tgstrm.admin_users WHERE id=$1`, s.UserID).Scan(&stored) != nil || !verifyPassword(stored, in.CurrentPassword) {
		time.Sleep(250 * time.Millisecond)
		fail(w, 401, "invalid_credentials", "La contraseña actual no es válida.")
		return
	}
	hash := stored
	if in.NewPassword != "" {
		if err = validNewPassword(in.NewPassword, in.Username); err != nil {
			fail(w, 422, "weak_password", err.Error())
			return
		}
		hash, err = hashPassword(in.NewPassword)
		if err != nil {
			fail(w, 500, "account_failed", "No se pudo actualizar la cuenta.")
			return
		}
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_users SET username=$2,password_hash=$3,password_changed_at=CASE WHEN $4 THEN now() ELSE password_changed_at END,updated_at=now() WHERE id=$1`, s.UserID, in.Username, hash, in.NewPassword != "")
	}
	if err == nil && in.NewPassword != "" {
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET revoked_at=now() WHERE admin_user_id=$1 AND id<>$2`, s.UserID, s.ID)
	}
	if err == nil {
		err = tx.Commit()
	} else if tx != nil {
		_ = tx.Rollback()
	}
	if err != nil {
		fail(w, 422, "account_failed", "No se pudo actualizar la cuenta; comprueba que el usuario esté disponible.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "updated", "username": in.Username})
}

func (a *API) adminSessions(w http.ResponseWriter, r *http.Request) {
	s, err := a.readSession(r)
	if err != nil {
		fail(w, 401, "authentication_required", "Inicia sesión para continuar.")
		return
	}
	if r.Method == http.MethodDelete {
		_, err = a.db.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET revoked_at=now() WHERE admin_user_id=$1 AND id<>$2 AND revoked_at IS NULL`, s.UserID, s.ID)
		if err != nil {
			fail(w, 500, "sessions_failed", "No se pudieron cerrar las demás sesiones.")
			return
		}
		writeJSON(w, 200, map[string]string{"status": "revoked"})
		return
	}
	rows, err := a.db.QueryContext(r.Context(), `SELECT id::text,created_at,last_seen_at,expires_at,(id=$2) FROM tgstrm.web_sessions WHERE admin_user_id=$1 AND revoked_at IS NULL AND expires_at>now() ORDER BY last_seen_at DESC`, s.UserID, s.ID)
	if err != nil {
		fail(w, 500, "sessions_failed", "No se pudieron consultar las sesiones.")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id string
		var created, last, expires time.Time
		var current bool
		if rows.Scan(&id, &created, &last, &expires, &current) == nil {
			items = append(items, map[string]any{"id": id, "created_at": created, "last_seen_at": last, "expires_at": expires, "current": current})
		}
	}
	writeJSON(w, 200, map[string]any{"sessions": items})
}
func (a *API) adminRecoveryComplete(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, Username, Password string }
	if !decodeAdminJSON(w, r, &in) {
		return
	}
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()
	if db == nil {
		fail(w, 422, "invalid_recovery", "El código o los datos no son válidos.")
		return
	}
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(in.Code), "-", ""))
	sum := sha256.Sum256([]byte(normalized))
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "recovery_failed", "No se pudo completar la recuperación.")
		return
	}
	var challengeID, userID string
	var storedHash []byte
	var attempts int
	err = tx.QueryRowContext(r.Context(), `SELECT id::text,admin_user_id::text,code_hash,attempts FROM tgstrm.admin_recovery_challenges WHERE consumed_at IS NULL AND expires_at>now() ORDER BY created_at DESC LIMIT 1 FOR UPDATE`).Scan(&challengeID, &userID, &storedHash, &attempts)
	if err != nil || attempts >= 5 {
		_ = tx.Rollback()
		fail(w, 422, "invalid_recovery", "El código o los datos no son válidos.")
		return
	}
	if subtle.ConstantTimeCompare(storedHash, sum[:]) != 1 {
		attempts++
		_, err = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_recovery_challenges SET attempts=$2,consumed_at=CASE WHEN $2>=5 THEN now() ELSE consumed_at END WHERE id=$1`, challengeID, attempts)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err != nil {
			fail(w, 500, "recovery_failed", "No se pudo validar el código.")
			return
		}
		fail(w, 422, "invalid_recovery", "El código o los datos no son válidos.")
		return
	}
	if !usernamePattern.MatchString(in.Username) {
		_ = tx.Rollback()
		fail(w, 422, "invalid_recovery", "El código o los datos no son válidos.")
		return
	}
	if err = validNewPassword(in.Password, in.Username); err != nil {
		_ = tx.Rollback()
		fail(w, 422, "weak_password", err.Error())
		return
	}
	hash, e := hashPassword(in.Password)
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_users SET username=$2,password_hash=$3,must_change_password=false,failed_attempts=0,locked_until=NULL,password_changed_at=now(),updated_at=now() WHERE id=$1`, userID, in.Username, hash)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE tgstrm.admin_recovery_challenges SET consumed_at=now() WHERE id=$1`, challengeID)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), `UPDATE tgstrm.web_sessions SET revoked_at=now() WHERE admin_user_id=$1`, userID)
	}
	if e == nil {
		e = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if e != nil {
		fail(w, 422, "recovery_failed", "No se pudo completar la recuperación. Revisa que el usuario esté disponible.")
		return
	}
	clearSessionCookies(w)
	writeJSON(w, 200, map[string]string{"status": "recovered", "next": "/admin/login"})
}

func (a *API) adminGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// The browser application itself must remain reachable in order to show
		// login, setup and recovery. Its data is exclusively exposed by /api,
		// where authentication is enforced below.
		if !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		public := p == "/api/v1/health/live" || p == "/api/v1/setup/status" || p == "/api/v1/setup/database/test" || p == "/api/v1/setup/database" || p == "/api/v1/admin/login" || p == "/api/v1/admin/session" || p == "/api/v1/admin/recovery/request" || p == "/api/v1/admin/recovery/complete" || strings.HasPrefix(p, "/stream/")
		if public {
			next.ServeHTTP(w, r)
			return
		}
		s, err := a.readSession(r)
		if err != nil {
			clearSessionCookies(w)
			fail(w, 401, "authentication_required", "Inicia sesión con la cuenta administradora.")
			return
		}
		if s.Scope == "password_change" && p != "/api/v1/admin/password/initial" && p != "/api/v1/admin/password/skip" && p != "/api/v1/admin/logout" {
			fail(w, 403, "password_change_required", "Debes cambiar la contraseña inicial.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/stream/") {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		} else {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; connect-src 'self' ws: wss:; img-src 'self' data: https://image.tmdb.org; media-src 'self'; font-src 'self'; style-src 'self'")
		}
		next.ServeHTTP(w, r)
	})
}
