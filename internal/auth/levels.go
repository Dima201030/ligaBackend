package auth

var allowedLevels = map[string]struct{}{
	"masters":    {},
	"challenger": {},
	"futures":    {},
	"rookies":    {},
	"beginners":  {},
	"newcomers":  {},
}

// ValidLevel reports whether slug is an adult CategorySlug.
func ValidLevel(slug string) bool {
	_, ok := allowedLevels[slug]
	return ok
}
