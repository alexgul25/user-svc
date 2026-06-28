package jwtmanager

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenClaims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

type Manager struct {
	secret   []byte
	tokenTTL time.Duration
}

func New(secret []byte, tokenTTL time.Duration) *Manager {
	return &Manager{secret: secret, tokenTTL: tokenTTL}
}

func (mngr *Manager) NewToken(userID string) (string, error) {
	claims := TokenClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(mngr.tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "user-svc",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(mngr.secret)
}
