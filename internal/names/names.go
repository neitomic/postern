package names

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const MaxTags = 8

var (
	ErrInvalid   = errors.New("invalid name")
	ErrReserved  = errors.New("reserved name")
	ErrLoginUser = errors.New("invalid login_user")
	ErrHostname  = errors.New("invalid hostname")
	ErrTag       = errors.New("invalid tag")
	ErrTagCount  = errors.New("too many tags")
)

var reserved = map[string]struct{}{
	"postern":      {},
	"postern-jump": {},
}

var (
	nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// SSH User: start letter/underscore; then a-z, digits, _, -, . (no space/newline).
	loginUserRE = regexp.MustCompile(`^[a-z_][a-z0-9_.-]{0,31}$`)
	// DNS labels / IPv4; no whitespace or ssh_config metacharacters (HostName interpolation).
	hostnameRE = regexp.MustCompile(`(?i)^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	tagRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
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

func ValidHostname(host string) error {
	if host == "" || len(host) > 253 || !hostnameRE.MatchString(host) {
		return fmt.Errorf("%w: %q", ErrHostname, host)
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

// SanitizeTag strips characters that would break an ssh_config comment.
func SanitizeTag(tag string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '#', '\n', '\r', 0:
			return -1
		default:
			return r
		}
	}, tag)
}
