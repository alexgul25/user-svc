package jwtmanager_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	jwtmanager "github.com/alexgul25/user-svc/internal/lib/jwt"
)

const (
	secret = "test-secret"
	userID = "user-1"
	ttl    = 30 * time.Minute
)

// parse разбирает токен, проверяя подпись указанным секретом.
func parse(token, secret string) (*jwtmanager.TokenClaims, error) {
	var claims jwtmanager.TokenClaims
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	return &claims, err
}

func TestNewToken_Claims(t *testing.T) {
	before := time.Now()

	token, err := jwtmanager.New([]byte(secret), ttl).NewToken(userID)
	if err != nil {
		t.Fatalf("NewToken returned error: %v", err)
	}

	claims, err := parse(token, secret)
	if err != nil {
		t.Fatalf("token is not valid: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("user_id = %q, want %q", claims.UserID, userID)
	}
	if claims.Issuer != "user-svc" {
		t.Errorf("issuer = %q, want %q", claims.Issuer, "user-svc")
	}

	// Время в JWT хранится с точностью до секунды, поэтому сравниваем с допуском.
	const tolerance = 2 * time.Second
	if diff := claims.IssuedAt.Time.Sub(before); diff < -tolerance || diff > tolerance {
		t.Errorf("issued at %v, want about %v", claims.IssuedAt.Time, before)
	}
	wantExpiry := before.Add(ttl)
	if diff := claims.ExpiresAt.Time.Sub(wantExpiry); diff < -tolerance || diff > tolerance {
		t.Errorf("expires at %v, want about %v", claims.ExpiresAt.Time, wantExpiry)
	}
}

func TestNewToken_RejectedWithWrongSecret(t *testing.T) {
	token, err := jwtmanager.New([]byte(secret), ttl).NewToken(userID)
	if err != nil {
		t.Fatalf("NewToken returned error: %v", err)
	}

	if _, err := parse(token, "another-secret"); err == nil {
		t.Error("token signed with one secret was accepted with another")
	}
}

func TestNewToken_Expired(t *testing.T) {
	// Отрицательный TTL даёт токен, срок действия которого уже истёк.
	token, err := jwtmanager.New([]byte(secret), -time.Minute).NewToken(userID)
	if err != nil {
		t.Fatalf("NewToken returned error: %v", err)
	}

	if _, err := parse(token, secret); err == nil {
		t.Error("expired token was accepted")
	}
}
