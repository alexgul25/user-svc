package domain

import "errors"

var (
	ErrUserExists         = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrAlreadySubscribed  = errors.New("user already subscribed")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSelfSubscription   = errors.New("cannot subscribe to yourself")
)
