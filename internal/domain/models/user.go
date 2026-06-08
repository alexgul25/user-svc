package models

import "time"

type User struct {
	ID           string // UUID
	DisplayName  string
	Email        string
	PasswordHash []byte
	CreatedAt    time.Time
}

type Follower struct {
	ID          string // UUID
	DisplayName string
	Email       string
}
