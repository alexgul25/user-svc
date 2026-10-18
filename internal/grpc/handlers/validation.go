package handlersgrpc

import (
	"context"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alexgul25/user-svc/internal/grpc/interceptors"
)

// maxPasswordBytes - bcrypt использует только первые 72 байта пароля:
// более длинный пароль нельзя сохранить, а при входе его хвост игнорировался бы.
const maxPasswordBytes = 72

var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// normalizeUUID проверяет, что строка - UUID в каноническом виде, и приводит её к нижнему регистру.
// PostgreSQL не различает регистр в UUID, а бизнес-логика сравнивает ID как строки:
// без приведения "ABC..." и "abc..." считались бы разными пользователями.
func normalizeUUID(s string) (string, bool) {
	if !uuidRegexp.MatchString(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

// requireUserID возвращает ID пользователя из заголовка x-user-id, который Gateway
// передаёт после проверки JWT. Без него или с некорректным значением вызов не аутентифицирован.
func requireUserID(ctx context.Context) (string, error) {
	raw, ok := interceptors.GetUserIDFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "user id is required")
	}

	userID, ok := normalizeUUID(raw)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "invalid user id")
	}

	return userID, nil
}

// validateText проверяет обязательное текстовое поле.
// Символ NUL PostgreSQL не хранит в текстовых полях: запрос упал бы с внутренней ошибкой.
func validateText(field, value string) error {
	if value == "" {
		return status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	if strings.ContainsRune(value, 0) {
		return status.Errorf(codes.InvalidArgument, "%s contains invalid characters", field)
	}
	return nil
}

// validatePassword проверяет пароль при регистрации и входе.
func validatePassword(password string) error {
	if password == "" {
		return status.Error(codes.InvalidArgument, "password is required")
	}
	if len(password) > maxPasswordBytes {
		return status.Errorf(codes.InvalidArgument, "password must be at most %d bytes", maxPasswordBytes)
	}
	return nil
}

// validateID проверяет обязательное поле с ID пользователя и возвращает его в нижнем регистре.
func validateID(field, value string) (string, error) {
	if value == "" {
		return "", status.Errorf(codes.InvalidArgument, "%s is required", field)
	}

	id, ok := normalizeUUID(value)
	if !ok {
		return "", status.Errorf(codes.InvalidArgument, "%s must be a UUID", field)
	}

	return id, nil
}
