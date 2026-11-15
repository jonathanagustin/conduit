package ports

// CONDUIT-INVENTED: apollographql.CurrentUserProjection
// Displaced-by: A public Apollo identity schema and delegation contract specifying me's type, ID mapping and unavailable fullName semantics.
// CurrentUser is a caller-bound identity snapshot. Public examples establish
// me.id and me.fullName, not their scalar/nullability or identity-value mapping.
// Conduit uses the resolved credential owner's username and leaves an absent
// display-name claim null; it never substitutes a username for a person's name.
type CurrentUser struct {
	ID       string
	FullName *string
}
