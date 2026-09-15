package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	lua "github.com/yuin/gopher-lua"
)

// transformTimeout is the maximum duration a Lua transform script is allowed
// to run before being cancelled.
const transformTimeout = 5 * time.Second

// runTransform executes a Lua transform script against the extracted outputs,
// for a response with no headers. The script receives the outputs as a mutable
// Lua table and a json_path() function that queries the full response body via
// gjson. The script must return the (possibly modified) outputs table. print()
// writes to stderr.
func runTransform(script string, outputs map[string]any, responseBody string) (map[string]any, error) {
	return runTransformWithLog(script, outputs, responseBody, nil, os.Stderr)
}

// runTransformWithLog is runTransform with the response's headers, which the
// script reads with header(), and with print() output sent to log. The base
// library's print writes to stdout, which would corrupt --json and
// --dump-state - output, so it is replaced.
func runTransformWithLog(script string, outputs map[string]any, responseBody string, headers http.Header, log io.Writer) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), transformTimeout)
	defer cancel()

	ls := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer ls.Close()

	// Open only safe libraries (no io, os, debug, coroutine, or package).
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		ls.Push(ls.NewFunction(pair.fn))
		ls.Push(lua.LString(pair.name))
		ls.Call(1, 0)
	}

	// The base library can still load code from files and strings and reach the
	// host process; remove those functions. A kit's templates run on its users'
	// machines, and dofile() or loadfile() without an argument reads stdin, which
	// under aat mcp serve is the MCP protocol stream.
	for _, name := range []string{
		"dofile", "loadfile", "load", "loadstring", "require", "module", // load code
		"getfenv", "setfenv", // change function environments
		"collectgarbage", "newproxy", "_printregs", // act on the host process
	} {
		ls.SetGlobal(name, lua.LNil)
	}

	// Replace print, which the base library points at stdout.
	ls.SetGlobal("print", ls.NewFunction(func(ls *lua.LState) int {
		parts := make([]string, ls.GetTop())
		for i := range parts {
			parts[i] = ls.ToStringMeta(ls.Get(i + 1)).String()
		}
		_, _ = fmt.Fprintln(log, strings.Join(parts, "\t"))
		return 0
	}))

	// Set context for timeout enforcement.
	ls.SetContext(ctx)

	// Register the json_path and header global functions.
	ls.SetGlobal("json_path", makeJsonPathFn(ls, responseBody))
	ls.SetGlobal("header", makeHeaderFn(ls, headers))

	// Convert outputs to Lua table and set as global.
	ls.SetGlobal("outputs", goToLua(ls, outputs))

	// Execute the script.
	if err := ls.DoString(script); err != nil {
		return nil, fmt.Errorf("lua script error: %w", err)
	}

	// Get the return value: a table keyed by output name. An empty table (for
	// example the outputs of a template whose extract rules all missed) is an
	// empty set of outputs, not an empty array.
	tbl, ok := ls.Get(-1).(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("lua script must return a table (got %s)", ls.Get(-1).Type())
	}
	if tbl.MaxN() > 0 && isSequentialTable(tbl) {
		return nil, fmt.Errorf("lua script must return a table keyed by output name (got a list)")
	}
	return luaTableToMap(tbl), nil
}

// goToLua converts a Go value to a Lua value recursively.
func goToLua(ls *lua.LState, val any) lua.LValue {
	if val == nil {
		return lua.LNil
	}
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case float64:
		return lua.LNumber(v)
	case float32:
		return lua.LNumber(float64(v))
	case int:
		return lua.LNumber(float64(v))
	case int64:
		return lua.LNumber(float64(v))
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return lua.LNumber(f)
		}
		return lua.LString(string(v))
	case bool:
		return lua.LBool(v)
	case []any:
		tbl := ls.NewTable()
		for i, elem := range v {
			tbl.RawSetInt(i+1, goToLua(ls, elem))
		}
		return tbl
	case map[string]any:
		tbl := ls.NewTable()
		for k, elem := range v {
			tbl.RawSetString(k, goToLua(ls, elem))
		}
		return tbl
	default:
		// Fall back to string representation.
		return lua.LString(fmt.Sprintf("%v", v))
	}
}

// luaToGo converts a Lua value to a Go value recursively.
func luaToGo(val lua.LValue) any {
	switch v := val.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case *lua.LTable:
		if isSequentialTable(v) {
			return luaTableToSlice(v)
		}
		return luaTableToMap(v)
	case lua.LString:
		return string(v)
	default:
		return nil
	}
}

// isSequentialTable checks if a Lua table has only sequential integer keys
// starting at 1 with no gaps, making it array-like.
func isSequentialTable(tbl *lua.LTable) bool {
	maxN := tbl.MaxN()
	if maxN == 0 {
		// Could be an empty table or a pure map. Check for any string keys.
		hasStringKeys := false
		tbl.ForEach(func(k, v lua.LValue) {
			if _, ok := k.(lua.LString); ok {
				hasStringKeys = true
			}
		})
		// An empty table with no string keys → treat as empty array.
		// A table with only string keys → treat as map.
		return !hasStringKeys
	}

	// Count total entries. If total == maxN, all keys are 1..N.
	total := 0
	tbl.ForEach(func(k, v lua.LValue) {
		total++
	})
	return total == maxN
}

// luaTableToSlice converts a sequential Lua table to a Go slice.
func luaTableToSlice(tbl *lua.LTable) []any {
	maxN := tbl.MaxN()
	result := make([]any, maxN)
	for i := 1; i <= maxN; i++ {
		result[i-1] = luaToGo(tbl.RawGetInt(i))
	}
	return result
}

// luaTableToMap converts a Lua table with string keys to a Go map.
func luaTableToMap(tbl *lua.LTable) map[string]any {
	result := make(map[string]any)
	tbl.ForEach(func(k, v lua.LValue) {
		if key, ok := k.(lua.LString); ok {
			result[string(key)] = luaToGo(v)
		} else if num, ok := k.(lua.LNumber); ok {
			// Mixed table: include numeric keys as strings.
			result[fmt.Sprintf("%g", float64(num))] = luaToGo(v)
		}
	})
	return result
}

// makeJsonPathFn creates a Lua function that queries the response body using
// gjson paths. The function signature in Lua is: json_path(path) → value.
func makeJsonPathFn(ls *lua.LState, responseBody string) *lua.LFunction {
	return ls.NewFunction(func(ls *lua.LState) int {
		path := ls.CheckString(1)
		gpath := normalizeJSONPath(path)
		result := gjson.Get(responseBody, gpath)
		if !result.Exists() {
			ls.Push(lua.LNil)
			return 1
		}
		ls.Push(goToLua(ls, result.Value()))
		return 1
	})
}

// makeHeaderFn creates a Lua function that reads a response header, matched in
// any case. The function signature in Lua is: header(name) → string, or nil
// when the response has no such header. A header sent more than once gives its
// values joined with ", ".
func makeHeaderFn(ls *lua.LState, headers http.Header) *lua.LFunction {
	return ls.NewFunction(func(ls *lua.LState) int {
		values := headerValues(headers, ls.CheckString(1))
		if len(values) == 0 {
			ls.Push(lua.LNil)
			return 1
		}
		ls.Push(lua.LString(strings.Join(values, ", ")))
		return 1
	})
}
