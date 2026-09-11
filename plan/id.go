package plan

import (
	"crypto/rand"
	"regexp"
	"strings"
)

const DefaultIDLength = 4

var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ValidID(prefix, id string) bool {
	return regexp.MustCompile("^" + regexp.QuoteMeta(prefix) + `-plan-[a-z0-9]+$`).MatchString(id)
}
func ValidSlug(slug string) bool           { return slugRE.MatchString(slug) }
func DirectoryName(id, slug string) string { return id + "-" + slug }
func NewID(prefix string, exists func(string) bool, length int) string {
	if length <= 0 {
		length = DefaultIDLength
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	for tries := 0; ; tries++ {
		if tries > 0 && tries%8 == 0 {
			length++
		}
		b := make([]byte, length)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		for i := range b {
			b[i] = alphabet[int(b[i])%len(alphabet)]
		}
		id := prefix + "-plan-" + string(b)
		if exists == nil || !exists(id) {
			return id
		}
	}
}
func Slug(title string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		return "plan"
	}
	return s
}
