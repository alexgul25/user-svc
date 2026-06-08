package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/alexgul25/user-svc/internal/domain/models"
)

type JWTManager struct {
	secret   string
	tokenTTL time.Duration
}

func NewJWTManager(secret string, tokenTTL time.Duration) *JWTManager {
	return &JWTManager{secret: secret, tokenTTL: tokenTTL}
}

func (m *JWTManager) NewToken(user models.User) (string, error) {
	token := jwt.New(jwt.SigningMethodHS256)

	claims := token.Claims.(jwt.MapClaims)
	claims["user_id"] = user.ID
	claims["email"] = user.Email
	claims["exp"] = time.Now().Add(m.tokenTTL).Unix()

	tokenStr, err := token.SignedString([]byte(m.secret))
	if err != nil {
		return "", err
	}

	return tokenStr, nil
}
