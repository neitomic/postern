package names

import (
	"errors"
	"fmt"
	"regexp"
)

const MaxTags = 8

var (
	ErrInvalid   = errors.New("invalid name")
	ErrReserved  = errors.New("reserved name")
	ErrLoginUser = errors.New("invalid login_user")
	ErrTag       = errors.New("invalid tag")
	ErrTagCount  = errors.New("too many tags")
)

var reserved = map[string]struct{}{
	"postern":      {},
	"postern-jump": {},
}

var (
	nameRE      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	loginUserRE = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	tagRE       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

func Valid(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalid, name)
	}
	if _, ok := reserved[name]; ok {
		return fmt.Errorf("%w: %q", ErrReserved, name)
	}
	return nil
}

func ValidLoginUser(user string) error {
	if !loginUserRE.MatchString(user) {
		return fmt.Errorf("%w: %q", ErrLoginUser, user)
	}
	return nil
}

func ValidTag(tag string) error {
	if !tagRE.MatchString(tag) {
		return fmt.Errorf("%w: %q", ErrTag, tag)
	}
	return nil
}

func ValidTags(tags []string) error {
	if len(tags) > MaxTags {
		return fmt.Errorf("%w: %d (max %d)", ErrTagCount, len(tags), MaxTags)
	}
	for _, tag := range tags {
		if err := ValidTag(tag); err != nil {
			return err
		}
	}
	return nil
}
