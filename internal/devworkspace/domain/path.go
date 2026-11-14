package domain

import "path"

// Confine keeps a request path inside one workspace. The cleaned path is the
// only value a store may join to its root.
func Confine(request string) (string, error) {
	if request == "" || request == "." {
		return ".", nil
	}
	if request[0] == '/' || request[0] == '\\' {
		return "", ErrPath
	}
	for _, runeValue := range request {
		if runeValue == '\\' || runeValue == 0 {
			return "", ErrPath
		}
	}
	cleaned := path.Clean(request)
	if cleaned == ".." || len(cleaned) >= 3 && cleaned[:3] == "../" {
		return "", ErrPath
	}
	if path.IsAbs(cleaned) || cleaned == "." {
		return "", ErrPath
	}
	return cleaned, nil
}
