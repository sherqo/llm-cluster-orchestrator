package lib

import (
	"github.com/google/uuid"
)

// NewRequestIDUUID7 generates a UUID v7 using the official package.
func NewRequestIDUUID7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
