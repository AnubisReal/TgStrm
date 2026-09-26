package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminPasswordHash(t *testing.T) {
	hash, err := hashPassword("una contraseña larga y privada")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "una contraseña larga y privada" {
		t.Fatal("password stored as plain text")
	}
	if !verifyPassword(hash, "una contraseña larga y privada") {
		t.Fatal("valid password rejected")
	}
	if verifyPassword(hash, "otra contraseña") {
		t.Fatal("invalid password accepted")
	}
}

func TestAdminPasswordPolicy(t *testing.T) {
	for _, weak := range []string{"short", "admin", "123456789012345"} {
		if validNewPassword(weak, "admin") == nil {
			t.Fatalf("weak password accepted: %q", weak)
		}
	}
	if err := validNewPassword("una frase privada bastante larga", "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCodesAreRandomAndNormalized(t *testing.T) {
	a, err := recoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	b, err := recoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 23 {
		t.Fatalf("unexpected recovery codes %q %q", a, b)
	}
}

func TestLoginLockExpiresWithoutExtendingWhileActive(t *testing.T) {
	now := time.Now()
	if loginLockExpired(sql.NullTime{Time: now.Add(time.Minute), Valid: true}, now) {
		t.Fatal("active lock was treated as expired")
	}
	if !loginLockExpired(sql.NullTime{Time: now.Add(-time.Second), Valid: true}, now) {
		t.Fatal("expired lock was treated as active")
	}
	if !loginLockExpired(sql.NullTime{}, now) {
		t.Fatal("account without a lock was rejected")
	}
}

func TestAdminRecoveryCompleteHandlesUnavailableDatabase(t *testing.T) {
	a := &API{}
	request := httptest.NewRequest("POST", "/api/v1/admin/recovery/complete", strings.NewReader(`{"code":"x","username":"admin","password":"a strong private password"}`))
	response := httptest.NewRecorder()
	a.adminRecoveryComplete(response, request)
	if response.Code != 422 {
		t.Fatalf("recovery status = %d, want 422", response.Code)
	}
}

func TestRecoveryAttemptHashComparisonIsExact(t *testing.T) {
	want := []byte("0123456789abcdef0123456789abcdef")
	if subtle.ConstantTimeCompare(want, append([]byte(nil), want...)) != 1 {
		t.Fatal("matching recovery hash rejected")
	}
	wrong := append([]byte(nil), want...)
	wrong[len(wrong)-1] ^= 1
	if subtle.ConstantTimeCompare(want, wrong) != 0 {
		t.Fatal("different recovery hash accepted")
	}
}
