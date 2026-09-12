package config

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// varPattern matches ${varName} placeholders in strings.
var varPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// varNamePattern matches a whole var name, as it may appear inside ${...}.
var varNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ParseVars parses KEY=VALUE pairs, such as repeated --var flags, into a map.
// A later pair for the same key wins. The value may contain '=' and may be
// empty; the key must be a valid var name.
func ParseVars(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	vars := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid var %q: expected KEY=VALUE", pair)
		}
		if !varNamePattern.MatchString(key) {
			return nil, fmt.Errorf("invalid var %q: %q is not a valid variable name", pair, key)
		}
		vars[key] = value
	}
	return vars, nil
}

// checkExternalVars reports vars set from outside the file (such as --var
// flags) that the environment file never mentions: no environment, shared
// section, or include declares the key or references it with ${key}. Such a
// var would silently change nothing, which is almost always a typo.
func checkExternalVars(mef *MultiEnvironmentFile, vars map[string]string) error {
	known := make(map[string]bool)
	collect := func(p EnvironmentPartial) {
		for key := range p.Vars {
			known[key] = true
		}
		for name := range referencedVars(&p) {
			known[name] = true
		}
	}
	collect(mef.Shared)
	for _, p := range mef.Environments {
		collect(p)
	}

	var unknown []string
	for key := range vars {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return unusableVarsError(fmt.Sprintf("unknown var(s) %s: not declared or referenced in the environment file", strings.Join(unknown, ", ")))
	}
	return nil
}

// substituteVars replaces ${key} placeholders in every string of the partial,
// map values and override values included, using the partial's vars. Map keys
// and the vars themselves are left as written. It returns an error naming any
// placeholder that remains unresolved.
func substituteVars(p *EnvironmentPartial) error {
	vars := p.Vars
	p.Vars = nil
	defer func() { p.Vars = vars }()

	if len(vars) > 0 {
		visitStrings(reflect.ValueOf(p).Elem(), func(s string) string {
			return substituteString(s, vars)
		})
	}

	var unresolved []string
	seen := make(map[string]bool)
	visitStrings(reflect.ValueOf(p).Elem(), func(s string) string {
		for _, m := range varPattern.FindAllString(s, -1) {
			if !seen[m] {
				seen[m] = true
				unresolved = append(unresolved, m)
			}
		}
		return s
	})
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		return fmt.Errorf("unresolved variable(s): %s", strings.Join(unresolved, ", "))
	}
	return nil
}

// referencedVars returns the names of the vars that ${...} placeholders in the
// partial refer to, outside the vars map itself.
func referencedVars(p *EnvironmentPartial) map[string]bool {
	vars := p.Vars
	p.Vars = nil
	defer func() { p.Vars = vars }()

	names := make(map[string]bool)
	visitStrings(reflect.ValueOf(p).Elem(), func(s string) string {
		for _, m := range varPattern.FindAllStringSubmatch(s, -1) {
			names[m[1]] = true
		}
		return s
	})
	return names
}

// substituteString replaces ${key} placeholders with values from vars.
func substituteString(s string, vars map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1] // strip ${ and }
		if val, ok := vars[key]; ok {
			return val
		}
		return match // leave unresolved for error checking
	})
}

// visitStrings calls fn on every string reachable from v, through pointers,
// interfaces, exported struct fields, slices, arrays, and map values, and
// stores the result back. Map keys are not visited. v must be settable.
func visitStrings(v reflect.Value, fn func(string) string) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(fn(v.String()))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			visitStrings(v.Elem(), fn)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := v.Elem()
		cp := reflect.New(inner.Type()).Elem()
		cp.Set(inner)
		visitStrings(cp, fn)
		if v.CanSet() {
			v.Set(cp)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				visitStrings(v.Field(i), fn)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			visitStrings(v.Index(i), fn)
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			elem := v.MapIndex(key)
			cp := reflect.New(elem.Type()).Elem()
			cp.Set(elem)
			visitStrings(cp, fn)
			v.SetMapIndex(key, cp)
		}
	}
}
