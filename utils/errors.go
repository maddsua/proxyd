package utils

import (
	"errors"
	"strings"
)

func JoinInlineErrors(errList ...error) error {

	var messages []string

	for _, err := range errList {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}

	if len(messages) == 0 {
		return nil
	}

	return errors.New(strings.Join(messages, "; "))
}
