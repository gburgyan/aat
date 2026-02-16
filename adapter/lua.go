package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/tidwall/gjson"
	lua "github.com/yuin/gopher-lua"
)

// transformTimeout is the maximum duration a Lua transform script is allowed
// to run before being cancelled.
const transformTimeout = 5 * time.Second

// runTransform executes a Lua transform script against the extracted outputs.
// The script receives the outputs as a mutable Lua table and a json_path()
// function that queries the full response body via gjson. The script must
// return the (possibly modified) outputs table.
func runTransform(script string, outputs map[string]any, responseBody string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), transformTimeout)
	defer cancel()

	ls := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer ls.Close()

	// Open only safe libraries (no io, os, debug, coroutine).
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage}, // for require() basics
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		ls.Push(ls.NewFunction(pair.fn))
		ls.Push(lua.LString(pair.name))
		ls.Call(1, 0)
	}

	// Set context for timeout enforcement.
	ls.SetContext(ctx)

	// Register json_path global function.
	ls.SetGlobal("json_path", makeJsonPathFn(ls, responseBody))

	// Convert outputs to Lua table and set as global.
	ls.SetGlobal("outputs", goToLua(ls, outputs))

	// Execute the script.
	if err := ls.DoString(script); err != nil {
		return nil, fmt.Errorf("lua script error: %w", err)
	}

	// Get the return value.
	retVal := ls.Get(-1)
	if retVal == lua.LNil {
		return nil, fmt.Errorf("lua script must return a table (got nil)")
	}

	result := luaToGo(retVal)
	resultMap, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("lua script must return a table (got %T)", result)
	}

	return resultMap, nil
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
