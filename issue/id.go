package issue

import (
	"crypto/rand"
	"regexp"
	"strings"
)

// idAlphabet is the character set of generated id hashes.
const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// DefaultIDLength is the hash length used when the hub config sets none.
const DefaultIDLength = 4

// idRe is the accepted id grammar. Imports keep foreign ids, so the prefix may
// contain dashes and children may carry dotted suffixes (beans-ceh.15).
var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-[a-z0-9]+(\.[0-9]+)*$`)

// ValidID reports whether s matches the id grammar.
func ValidID(s string) bool { return idRe.MatchString(s) }

// NewID draws a random id <prefix>-<hash> whose hash has length characters
// from [a-z0-9]. exists reports whether an id is already taken; after 8
// collisions the hash grows by one character.
func NewID(prefix string, exists func(string) bool, length int) string {
	if length <= 0 {
		length = DefaultIDLength
	}
	for attempt := 0; ; attempt++ {
		if attempt >= 8 {
			length++
			attempt = 0
		}
		id := prefix + "-" + randomHash(length)
		if exists == nil || !exists(id) {
			return id
		}
	}
}

func randomHash(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = idAlphabet[int(b)%len(idAlphabet)]
	}
	return string(out)
}

// SlugMaxLen is the cut point for slugs, applied without splitting a word
// when possible.
const SlugMaxLen = 60

// Slug lowercases title, replaces every run of characters outside [a-z0-9]
// with "-", trims "-", and cuts at SlugMaxLen. A title with no [a-z0-9]
// characters yields "".
func Slug(title string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) <= SlugMaxLen {
		return s
	}
	cut := s[:SlugMaxLen]
	if i := strings.LastIndexByte(cut, '-'); i > 0 && len(s) > SlugMaxLen && s[SlugMaxLen] != '-' {
		cut = cut[:i]
	}
	return strings.Trim(cut, "-")
}

// Filename returns "<id>-<slug>.md", or "<id>.md" when the slug is empty.
func Filename(id, slug string) string {
	if slug == "" {
		return id + ".md"
	}
	return id + "-" + slug + ".md"
}
