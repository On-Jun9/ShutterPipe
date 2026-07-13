package pipeline

import "path/filepath"

type destinationIdentityResolver struct {
	identities map[string]string
	parents    map[string]string
	canonical  func(string) (string, bool)
}

func newDestinationIdentityResolver() *destinationIdentityResolver {
	return &destinationIdentityResolver{
		identities: make(map[string]string),
		parents:    make(map[string]string),
		canonical:  canonicalOpenedPath,
	}
}

// Identity returns the actual directory-entry path for an existing destination.
// It deliberately identifies names rather than inodes so distinct hard links do
// not become one overwrite group. Missing destinations use their resolved parent
// plus requested name, which keeps exact planned-path grouping deterministic.
func (r *destinationIdentityResolver) Identity(path string) string {
	if identity, ok := r.identities[path]; ok {
		return identity
	}
	if canonical, ok := r.canonical(path); ok {
		identity := filepath.Clean(canonical)
		r.identities[path] = identity
		return identity
	}

	parent := filepath.Dir(path)
	resolvedParent, ok := r.parents[parent]
	if !ok {
		resolvedParent, _ = filepath.EvalSymlinks(parent)
		if resolvedParent == "" {
			resolvedParent, _ = filepath.Abs(parent)
		}
		resolvedParent = filepath.Clean(resolvedParent)
		r.parents[parent] = resolvedParent
	}
	identity := filepath.Join(resolvedParent, filepath.Base(path))
	r.identities[path] = identity
	return identity
}
