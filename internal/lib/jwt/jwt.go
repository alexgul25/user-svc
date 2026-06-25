package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenClaims struct {
	jwt.RegisteredClaims
	UserID string `json:"user_id"`
}

type JWTManager struct {
	secret   []byte
	tokenTTL time.Duration
}

func NewJWTManager(secret []byte, tokenTTL time.Duration) *JWTManager {
	return &JWTManager{secret: secret, tokenTTL: tokenTTL}
}

func (mngr *JWTManager) NewToken(userID string) (string, error) {
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
