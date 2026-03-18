package utils

import "errors"

type ContextKey string

func AuthorizeUser(userRole string, allowedRoles ...string) (bool, error) {
	for _, allallowedRole := range allowedRoles {
		if userRole == allallowedRole {
			return true, nil
		}
	}

	return false, errors.New("user not authorized")
}
