package ai

import (
	"errors"
	"strings"
)

func ValidateFunctionBody(body string) error {

	if strings.TrimSpace(body) == "" {
		return errors.New("empty AI output")
	}

	if strings.Contains(body, "func ") {
		return errors.New("AI returned full function")
	}

	if strings.Contains(body, "```") {
		return errors.New("AI returned markdown")
	}

	if strings.Contains(body, "package ") {
		return errors.New("invalid code block")
	}

	if !strings.Contains(body, "\n") {
		return errors.New("too short")
	}

	return nil
}
