//go:build integration

package integration

import "github.com/google/uuid"

func uuidNew() string { return uuid.New().String() }
