// Package types holds small data types shared across packages that must not
// import each other (middleware, services, handlers). No logic beyond
// methods on the types themselves.
package types

// AuthUser is the authenticated caller of a request.
type AuthUser struct {
	Sub     string
	Email   string
	Roles   []string
	IsAdmin bool
}

// HasRole reports whether the user holds role (exact match).
func (u AuthUser) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}
