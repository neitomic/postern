package names

import (
	"errors"
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	t.Parallel()
	valid := []string{
		"a",
		"z",
		"0",
		"9",
		"a1",
		"n-1",
		"macbook",
		"nuc",
		"pi",
		"ab",
		strings.Repeat("a", 63),
		"x" + strings.Repeat("y", 61) + "z",
	}
	for _, name := range valid {
		if err := Valid(name); err != nil {
			t.Errorf("Valid(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		"A",
		"MacBook",
		"-abc",
		"abc-",
		"ab.c",
		"abc_def",
		"a/b",
		"ab c",
		" ",
		strings.Repeat("a", 64),
		"foo.bar",
		"a--", // trailing hyphen
	}
	for _, name := range invalid {
		err := Valid(name)
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("Valid(%q) = %v, want %v", name, err, ErrInvalid)
		}
	}
}

func TestValidReserved(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"postern", "postern-jump"} {
		err := Valid(name)
		if !errors.Is(err, ErrReserved) {
			t.Errorf("Valid(%q) = %v, want %v", name, err, ErrReserved)
		}
	}
}

func TestValidLoginUser(t *testing.T) {
	t.Parallel()
	valid := []string{
		"a",
		"_",
		"neo",
		"debian",
		"user_name",
		"user-name",
		"_abc",
		"a" + strings.Repeat("x", 31),
	}
	for _, user := range valid {
		if err := ValidLoginUser(user); err != nil {
			t.Errorf("ValidLoginUser(%q) = %v, want nil", user, err)
		}
	}

	invalid := []string{
		"",
		"Neo",
		"1user",
		"-user",
		"user.name",
		"user name",
		"a" + strings.Repeat("x", 32),
		"root\n",
	}
	for _, user := range invalid {
		err := ValidLoginUser(user)
		if !errors.Is(err, ErrLoginUser) {
			t.Errorf("ValidLoginUser(%q) = %v, want %v", user, err, ErrLoginUser)
		}
	}
}

func TestValidTags(t *testing.T) {
	t.Parallel()
	if err := ValidTags(nil); err != nil {
		t.Errorf("ValidTags(nil) = %v, want nil", err)
	}
	if err := ValidTags([]string{}); err != nil {
		t.Errorf("ValidTags(empty) = %v, want nil", err)
	}
	if err := ValidTags([]string{"home", "laptop"}); err != nil {
		t.Errorf("ValidTags(home,laptop) = %v, want nil", err)
	}
	eight := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	if err := ValidTags(eight); err != nil {
		t.Errorf("ValidTags(8) = %v, want nil", err)
	}
	if err := ValidTag("a" + strings.Repeat("b", 31)); err != nil {
		t.Errorf("ValidTag(32) = %v, want nil", err)
	}

	nine := append(append([]string{}, eight...), "i")
	if err := ValidTags(nine); !errors.Is(err, ErrTagCount) {
		t.Errorf("ValidTags(9) = %v, want %v", err, ErrTagCount)
	}

	invalid := []string{"Home", "-bad", "", "_no", "a" + strings.Repeat("b", 32), "tag_1", "a.b"}
	for _, tag := range invalid {
		err := ValidTag(tag)
		if !errors.Is(err, ErrTag) {
			t.Errorf("ValidTag(%q) = %v, want %v", tag, err, ErrTag)
		}
		if err := ValidTags([]string{tag}); !errors.Is(err, ErrTag) {
			t.Errorf("ValidTags(%q) = %v, want %v", tag, err, ErrTag)
		}
	}
}
