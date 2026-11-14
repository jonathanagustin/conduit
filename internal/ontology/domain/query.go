package domain

import (
	"sort"
	"strconv"
)

const (
	defaultPageSize = 100
	maxPageSize     = 500
)

// Resolve evaluates a base, filter, or search-around set against the catalog.
func Resolve(catalog Catalog, set ObjectSet) ([]Object, error) {
	switch set.Type {
	case "base":
		if !KnownType(set.ObjectType) {
			return nil, ErrUnknownType
		}
		out := make([]Object, 0)
		for _, object := range catalog.Objects {
			if object.Type == set.ObjectType {
				out = append(out, object)
			}
		}
		sortObjects(out)
		return out, nil
	case "filter":
		if set.Inner == nil || set.Where == nil {
			return nil, ErrBadQuery
		}
		inner, err := Resolve(catalog, *set.Inner)
		if err != nil {
			return nil, err
		}
		out := make([]Object, 0)
		for _, object := range inner {
			ok, err := match(object, *set.Where)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, object)
			}
		}
		return out, nil
	case "searchAround":
		if set.Inner == nil || set.Link == "" {
			return nil, ErrBadQuery
		}
		if !KnownLink(set.Link) {
			return nil, ErrBadQuery
		}
		inner, err := Resolve(catalog, *set.Inner)
		if err != nil {
			return nil, err
		}
		seen := map[string]Object{}
		for _, object := range inner {
			for _, link := range catalog.Links {
				if link.FromType != object.Type || link.FromKey != object.PrimaryKey || link.APIName != set.Link {
					continue
				}
				target, ok := findObject(catalog, link.ToType, link.ToKey)
				if !ok {
					continue
				}
				seen[target.Type+"\x00"+target.PrimaryKey] = target
			}
		}
		out := make([]Object, 0, len(seen))
		for _, object := range seen {
			out = append(out, object)
		}
		sortObjects(out)
		return out, nil
	default:
		return nil, ErrBadQuery
	}
}

// PageOf slices a resolved set. An empty token starts at the first record.
// pageSize 0 selects the default. The next token is the next offset, omitted
// by the caller when empty.
func PageOf(objects []Object, pageSize int, token string) (Page, error) {
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return Page{}, ErrBadQuery
	}
	offset := 0
	if token != "" {
		parsed, err := strconv.Atoi(token)
		if err != nil || parsed < 0 {
			return Page{}, ErrBadQuery
		}
		offset = parsed
	}
	if offset > len(objects) {
		offset = len(objects)
	}
	end := offset + pageSize
	if end > len(objects) {
		end = len(objects)
	}
	next := ""
	if end < len(objects) {
		next = strconv.Itoa(end)
	}
	return Page{Objects: objects[offset:end], NextPageToken: next, TotalCount: len(objects)}, nil
}

func match(object Object, where Where) (bool, error) {
	switch where.Type {
	case "eq":
		if where.Field == "" {
			return false, ErrBadQuery
		}
		if where.Field == objectPrimaryField(object) || where.Field == "__primaryKey" {
			if object.PrimaryKey == where.Value {
				return true, nil
			}
		}
		return object.Properties[where.Field] == where.Value, nil
	case "and":
		for _, clause := range where.Clauses {
			ok, err := match(object, clause)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case "or":
		if len(where.Clauses) == 0 {
			return false, nil
		}
		for _, clause := range where.Clauses {
			ok, err := match(object, clause)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		if len(where.Clauses) != 1 {
			return false, ErrBadQuery
		}
		ok, err := match(object, where.Clauses[0])
		if err != nil {
			return false, err
		}
		return !ok, nil
	default:
		return false, ErrBadQuery
	}
}

func objectPrimaryField(object Object) string {
	spec, ok := typeSpec(object.Type)
	if !ok {
		return ""
	}
	return spec.PrimaryKey
}

func findObject(catalog Catalog, apiName, key string) (Object, bool) {
	for _, object := range catalog.Objects {
		if object.Type == apiName && object.PrimaryKey == key {
			return object, true
		}
	}
	return Object{}, false
}

func sortObjects(objects []Object) {
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Type != objects[j].Type {
			return objects[i].Type < objects[j].Type
		}
		return objects[i].PrimaryKey < objects[j].PrimaryKey
	})
}
