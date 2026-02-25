package adapter

import (
	"testing"

	lua "github.com/yuin/gopher-lua"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- goToLua / luaToGo round-trip tests ---

func TestGoToLuaToGo_String(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, "hello")
	assert.Equal(t, "hello", luaToGo(lv))
}

func TestGoToLuaToGo_Float64(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, 42.5)
	assert.Equal(t, 42.5, luaToGo(lv))
}

func TestGoToLuaToGo_Int(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, 7)
	assert.Equal(t, float64(7), luaToGo(lv))
}

func TestGoToLuaToGo_Bool(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, true)
	assert.Equal(t, true, luaToGo(lv))
}

func TestGoToLuaToGo_Nil(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, nil)
	assert.Equal(t, lua.LNil, lv)
	assert.Nil(t, luaToGo(lv))
}

func TestGoToLuaToGo_Slice(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	input := []any{"a", float64(2), true}
	lv := goToLua(ls, input)
	got := luaToGo(lv)
	assert.Equal(t, input, got)
}

func TestGoToLuaToGo_Map(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	input := map[string]any{"name": "test", "count": float64(3)}
	lv := goToLua(ls, input)
	got := luaToGo(lv)
	assert.Equal(t, input, got)
}

func TestGoToLuaToGo_NestedStructure(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	input := map[string]any{
		"items": []any{
			map[string]any{"id": "a", "val": float64(1)},
			map[string]any{"id": "b", "val": float64(2)},
		},
		"total": float64(2),
	}
	lv := goToLua(ls, input)
	got := luaToGo(lv).(map[string]any)
	assert.Equal(t, float64(2), got["total"])
	items := got["items"].([]any)
	assert.Len(t, items, 2)
	assert.Equal(t, "a", items[0].(map[string]any)["id"])
}

func TestGoToLuaToGo_EmptySlice(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, []any{})
	got := luaToGo(lv)
	// Empty table with no string keys → treated as empty array.
	assert.Equal(t, []any{}, got)
}

func TestGoToLuaToGo_EmptyMap(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, map[string]any{})
	got := luaToGo(lv)
	// Empty table with no keys → treated as empty array ([]any{}).
	// This is acceptable since empty maps and empty arrays are
	// indistinguishable in Lua.
	assert.NotNil(t, got)
}

// --- isSequentialTable ---

func TestIsSequentialTable_Sequential(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	tbl := ls.NewTable()
	tbl.RawSetInt(1, lua.LString("a"))
	tbl.RawSetInt(2, lua.LString("b"))
	assert.True(t, isSequentialTable(tbl))
}

func TestIsSequentialTable_StringKeys(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	tbl := ls.NewTable()
	tbl.RawSetString("x", lua.LString("a"))
	tbl.RawSetString("y", lua.LString("b"))
	assert.False(t, isSequentialTable(tbl))
}

func TestIsSequentialTable_Mixed(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	tbl := ls.NewTable()
	tbl.RawSetInt(1, lua.LString("a"))
	tbl.RawSetString("key", lua.LString("val"))
	assert.False(t, isSequentialTable(tbl))
}

func TestIsSequentialTable_Empty(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	tbl := ls.NewTable()
	assert.True(t, isSequentialTable(tbl))
}

// --- json_path function ---

func TestJsonPath_Scalar(t *testing.T) {
	body := `{"name": "Alice", "age": 30}`
	result, err := runTransform(`return {name = json_path("name"), age = json_path("age")}`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, "Alice", result["name"])
	assert.Equal(t, float64(30), result["age"])
}

func TestJsonPath_NestedPath(t *testing.T) {
	body := `{"data": {"items": [{"id": "a"}, {"id": "b"}]}}`
	result, err := runTransform(`return {first = json_path("data.items.0.id")}`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, "a", result["first"])
}

func TestJsonPath_Array(t *testing.T) {
	body := `{"tags": ["go", "lua", "test"]}`
	result, err := runTransform(`
		local tags = json_path("tags")
		return {count = #tags, first = tags[1]}
	`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, float64(3), result["count"])
	assert.Equal(t, "go", result["first"])
}

func TestJsonPath_MissingPath(t *testing.T) {
	body := `{"name": "test"}`
	result, err := runTransform(`
		local val = json_path("nonexistent")
		return {found = val ~= nil}
	`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, false, result["found"])
}

func TestJsonPath_NormalizesPath(t *testing.T) {
	body := `{"data": {"items": [{"id": "first"}]}}`
	result, err := runTransform(`return {id = json_path("$.data.items[0].id")}`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, "first", result["id"])
}

func TestJsonPath_Object(t *testing.T) {
	body := `{"user": {"name": "Bob", "age": 25}}`
	result, err := runTransform(`
		local user = json_path("user")
		return {name = user.name, age = user.age}
	`, map[string]any{}, body)
	require.NoError(t, err)
	assert.Equal(t, "Bob", result["name"])
	assert.Equal(t, float64(25), result["age"])
}

// --- runTransform ---

func TestRunTransform_Identity(t *testing.T) {
	outputs := map[string]any{"x": "hello", "y": float64(42)}
	result, err := runTransform("return outputs", outputs, "{}")
	require.NoError(t, err)
	assert.Equal(t, "hello", result["x"])
	assert.Equal(t, float64(42), result["y"])
}

func TestRunTransform_ModifyOutputs(t *testing.T) {
	outputs := map[string]any{"name": "Alice"}
	result, err := runTransform(`
		outputs.greeting = "Hello, " .. outputs.name .. "!"
		return outputs
	`, outputs, "{}")
	require.NoError(t, err)
	assert.Equal(t, "Hello, Alice!", result["greeting"])
	assert.Equal(t, "Alice", result["name"])
}

func TestRunTransform_EnrichFromBody(t *testing.T) {
	body := `{"extra": {"color": "blue"}}`
	outputs := map[string]any{"id": "123"}
	result, err := runTransform(`
		outputs.color = json_path("extra.color")
		return outputs
	`, outputs, body)
	require.NoError(t, err)
	assert.Equal(t, "123", result["id"])
	assert.Equal(t, "blue", result["color"])
}

func TestRunTransform_ArrayEnrichment(t *testing.T) {
	body := `{"refs": {"p1": {"name": "Product A"}, "p2": {"name": "Product B"}}}`
	outputs := map[string]any{
		"items": []any{
			map[string]any{"id": "1", "ref": "p1"},
			map[string]any{"id": "2", "ref": "p2"},
		},
	}
	result, err := runTransform(`
		local refs = json_path("refs")
		for i, item in ipairs(outputs.items) do
			local ref = refs[item.ref]
			if ref then
				item.productName = ref.name
			end
		end
		return outputs
	`, outputs, body)
	require.NoError(t, err)
	items := result["items"].([]any)
	require.Len(t, items, 2)
	assert.Equal(t, "Product A", items[0].(map[string]any)["productName"])
	assert.Equal(t, "Product B", items[1].(map[string]any)["productName"])
}

func TestRunTransform_SyntaxError(t *testing.T) {
	_, err := runTransform("this is not valid lua!!!", map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lua script error")
}

func TestRunTransform_RuntimeError(t *testing.T) {
	_, err := runTransform(`
		local x = nil
		x.foo = "bar"  -- attempt to index nil
		return outputs
	`, map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lua script error")
}

func TestRunTransform_NilReturn(t *testing.T) {
	_, err := runTransform("return nil", map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must return a table")
}

func TestRunTransform_WrongReturnType(t *testing.T) {
	_, err := runTransform(`return "not a table"`, map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must return a table")
}

func TestRunTransform_NoReturn(t *testing.T) {
	// A Lua chunk that doesn't return pushes nothing → nil on stack.
	_, err := runTransform(`outputs.x = 1`, map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must return a table")
}

func TestRunTransform_Timeout(t *testing.T) {
	_, err := runTransform(`
		while true do end
		return outputs
	`, map[string]any{}, "{}")
	require.Error(t, err)
	// Context cancellation surfaces as an error.
	assert.Contains(t, err.Error(), "lua script error")
}

func TestRunTransform_NoIOAccess(t *testing.T) {
	_, err := runTransform(`
		local f = io.open("/etc/passwd", "r")
		return outputs
	`, map[string]any{}, "{}")
	require.Error(t, err)
	// io is not loaded, so this should fail.
	assert.Contains(t, err.Error(), "lua script error")
}

func TestRunTransform_NoOSAccess(t *testing.T) {
	_, err := runTransform(`
		os.execute("echo pwned")
		return outputs
	`, map[string]any{}, "{}")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lua script error")
}

// --- ExtractOutputs integration ---

func TestExtractOutputs_WithTransform(t *testing.T) {
	tmpl := Template{
		Adapter:  "test",
		Protocol: "http",
		Request:  TemplateRequest{Method: "GET", Path: "/test"},
		Response: TemplateResponse{
			Extract: map[string]ExtractRule{
				"name": {Path: "name"},
				"age":  {Path: "age"},
			},
			Transform: `
				outputs.greeting = "Hello, " .. outputs.name
				return outputs
			`,
		},
	}
	adapter := NewTemplateAdapter(tmpl)
	resp := &Response{
		StatusCode: 200,
		Body:       []byte(`{"name": "Alice", "age": 30}`),
	}
	outputs, err := adapter.ExtractOutputs(resp)
	require.NoError(t, err)
	assert.Equal(t, "Alice", outputs["name"])
	assert.Equal(t, float64(30), outputs["age"])
	assert.Equal(t, "Hello, Alice", outputs["greeting"])
}

func TestExtractOutputs_WithoutTransform(t *testing.T) {
	tmpl := Template{
		Adapter:  "test",
		Protocol: "http",
		Request:  TemplateRequest{Method: "GET", Path: "/test"},
		Response: TemplateResponse{
			Extract: map[string]ExtractRule{
				"name": {Path: "name"},
			},
		},
	}
	adapter := NewTemplateAdapter(tmpl)
	resp := &Response{
		StatusCode: 200,
		Body:       []byte(`{"name": "Bob"}`),
	}
	outputs, err := adapter.ExtractOutputs(resp)
	require.NoError(t, err)
	assert.Equal(t, "Bob", outputs["name"])
	_, hasGreeting := outputs["greeting"]
	assert.False(t, hasGreeting)
}

func TestExtractOutputs_TransformError(t *testing.T) {
	tmpl := Template{
		Adapter:  "test",
		Protocol: "http",
		Request:  TemplateRequest{Method: "GET", Path: "/test"},
		Response: TemplateResponse{
			Extract: map[string]ExtractRule{
				"name": {Path: "name"},
			},
			Transform: `error("intentional failure")`,
		},
	}
	adapter := NewTemplateAdapter(tmpl)
	resp := &Response{
		StatusCode: 200,
		Body:       []byte(`{"name": "Alice"}`),
	}
	_, err := adapter.ExtractOutputs(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "transform")
}

func TestExtractOutputs_TransformWithJsonPath(t *testing.T) {
	tmpl := Template{
		Adapter:  "test",
		Protocol: "http",
		Request:  TemplateRequest{Method: "GET", Path: "/test"},
		Response: TemplateResponse{
			Extract: map[string]ExtractRule{
				"itemId": {Path: "item.id"},
			},
			Transform: `
				local detail = json_path("details." .. outputs.itemId)
				if detail then
					outputs.description = detail.desc
				end
				return outputs
			`,
		},
	}
	adapter := NewTemplateAdapter(tmpl)
	resp := &Response{
		StatusCode: 200,
		Body:       []byte(`{"item": {"id": "x1"}, "details": {"x1": {"desc": "Widget"}}}`),
	}
	outputs, err := adapter.ExtractOutputs(resp)
	require.NoError(t, err)
	assert.Equal(t, "x1", outputs["itemId"])
	assert.Equal(t, "Widget", outputs["description"])
}

// --- HasTransform ---

func TestHasTransform(t *testing.T) {
	tmplWith := Template{Response: TemplateResponse{Transform: "return outputs"}}
	tmplWithout := Template{Response: TemplateResponse{}}
	assert.True(t, tmplWith.HasTransform())
	assert.False(t, tmplWithout.HasTransform())
}

// --- Travelport-style GDS reference resolution ---

func TestRunTransform_GDSReferenceResolution(t *testing.T) {
	// Simulates GDS response where CatalogProductOfferings contain
	// productRef strings that reference Products in a ReferenceList.
	// ReferenceList is a sibling of CatalogProductOfferings (not nested).
	// Products reference flights via FlightRef (not inline Flight objects).
	body := `{
		"CatalogProductOfferingsResponse": {
			"CatalogProductOfferings": {
				"CatalogProductOffering": [
					{
						"id": "offer1",
						"ProductBrandOptions": [{"flightRefs": ["s1"], "ProductBrandOffering": [{"Product": [{"productRef": "p0"}]}]}]
					}
				]
			},
			"ReferenceList": [
				{
					"@type": "ReferenceListProduct",
					"Product": [
						{
							"@type": "ProductAir",
							"id": "p0",
							"FlightSegment": [
								{
									"@type": "FlightSegment",
									"Flight": {"@type": "FlightID", "FlightRef": "s1"}
								}
							],
							"PassengerFlight": [
								{
									"FlightProduct": [
										{"cabin": "Economy", "classOfService": "Y"}
									]
								}
							]
						}
					]
				},
				{
					"@type": "ReferenceListFlight",
					"Flight": [
						{
							"@type": "FlightDetail",
							"id": "s1",
							"carrier": "QF",
							"number": "422",
							"Departure": {"date": "2026-03-01", "time": "08:00:00"},
							"Arrival": {"time": "10:30:00"}
						}
					]
				}
			]
		}
	}`

	outputs := map[string]any{
		"catalogProductOfferings": []any{
			map[string]any{
				"offeringId": "offer1",
				"productId":  "p0",
			},
		},
	}

	script := `
		local refLists = json_path("CatalogProductOfferingsResponse.ReferenceList")
		if not refLists then return outputs end

		local products = {}
		local flights = {}
		for _, refList in ipairs(refLists) do
			if refList["@type"] == "ReferenceListProduct" and refList.Product then
				for _, prod in ipairs(refList.Product) do
					if prod.id then products[prod.id] = prod end
				end
			elseif refList["@type"] == "ReferenceListFlight" and refList.Flight then
				for _, fl in ipairs(refList.Flight) do
					if fl.id then flights[fl.id] = fl end
				end
			end
		end

		local function resolveFlight(seg)
			if not seg or not seg.Flight then return nil end
			local fl = seg.Flight
			if type(fl) == "table" and fl.carrier then return fl end
			if type(fl) == "table" and fl.FlightRef and flights[fl.FlightRef] then
				return flights[fl.FlightRef]
			end
			return nil
		end

		if outputs.catalogProductOfferings then
			for _, offering in ipairs(outputs.catalogProductOfferings) do
				local pid = offering.productId
				if pid and products[pid] then
					local prod = products[pid]
					if prod.FlightSegment and prod.FlightSegment[1] then
						local flight = resolveFlight(prod.FlightSegment[1])
						if flight then
							if not offering.carrier then offering.carrier = flight.carrier end
							if not offering.flightNumber then offering.flightNumber = flight.number end
							if flight.Departure then
								if not offering.departureDate then offering.departureDate = flight.Departure.date end
								if not offering.departureTime then offering.departureTime = flight.Departure.time end
							end
							if flight.Arrival then
								if not offering.arrivalTime then offering.arrivalTime = flight.Arrival.time end
							end
						end
					end
					if prod.PassengerFlight and prod.PassengerFlight[1] then
						local pf = prod.PassengerFlight[1]
						if pf.FlightProduct and pf.FlightProduct[1] then
							local fp = pf.FlightProduct[1]
							if not offering.cabin then offering.cabin = fp.cabin end
							if not offering.classOfService then offering.classOfService = fp.classOfService end
						end
					end
				end
			end
		end

		return outputs
	`

	result, err := runTransform(script, outputs, body)
	require.NoError(t, err)

	offerings := result["catalogProductOfferings"].([]any)
	require.Len(t, offerings, 1)
	offer := offerings[0].(map[string]any)

	assert.Equal(t, "offer1", offer["offeringId"])
	assert.Equal(t, "p0", offer["productId"])
	assert.Equal(t, "QF", offer["carrier"])
	assert.Equal(t, "422", offer["flightNumber"])
	assert.Equal(t, "2026-03-01", offer["departureDate"])
	assert.Equal(t, "08:00:00", offer["departureTime"])
	assert.Equal(t, "10:30:00", offer["arrivalTime"])
	assert.Equal(t, "Economy", offer["cabin"])
	assert.Equal(t, "Y", offer["classOfService"])
}

func TestRunTransform_GDS_PreservesExistingValues(t *testing.T) {
	// When inline values already exist (full-payload mode), the transform
	// should not overwrite them.
	body := `{
		"CatalogProductOfferingsResponse": {
			"ReferenceList": [
				{
					"@type": "ReferenceListProduct",
					"Product": [{"id": "p0", "FlightSegment": [{"Flight": {"carrier": "VA", "number": "100"}}]}]
				}
			]
		}
	}`

	outputs := map[string]any{
		"catalogProductOfferings": []any{
			map[string]any{
				"offeringId":   "offer1",
				"productId":    "p0",
				"carrier":      "QF",         // Already set from inline data
				"flightNumber": float64(401), // Already set
			},
		},
	}

	script := `
		local refLists = json_path("CatalogProductOfferingsResponse.ReferenceList")
		if not refLists then return outputs end
		local products = {}
		for _, rl in ipairs(refLists) do
			if rl["@type"] == "ReferenceListProduct" and rl.Product then
				for _, p in ipairs(rl.Product) do
					if p.id then products[p.id] = p end
				end
			end
		end
		if outputs.catalogProductOfferings then
			for _, off in ipairs(outputs.catalogProductOfferings) do
				local pid = off.productId
				if pid and products[pid] then
					local prod = products[pid]
					if prod.FlightSegment and prod.FlightSegment[1] and prod.FlightSegment[1].Flight then
						local fl = prod.FlightSegment[1].Flight
						if not off.carrier then off.carrier = fl.carrier end
						if not off.flightNumber then off.flightNumber = fl.number end
					end
				end
			end
		end
		return outputs
	`

	result, err := runTransform(script, outputs, body)
	require.NoError(t, err)

	offer := result["catalogProductOfferings"].([]any)[0].(map[string]any)
	assert.Equal(t, "QF", offer["carrier"])              // Preserved, not overwritten
	assert.Equal(t, float64(401), offer["flightNumber"]) // Preserved, not overwritten
}

// --- Type conversion edge cases ---

func TestGoToLua_Float32(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, float32(3.14))
	num, ok := lv.(lua.LNumber)
	assert.True(t, ok)
	assert.InDelta(t, 3.14, float64(num), 0.001)
}

func TestGoToLua_Int64(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	lv := goToLua(ls, int64(9999))
	assert.Equal(t, float64(9999), luaToGo(lv))
}

func TestGoToLua_UnknownType(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	type custom struct{ x int }
	lv := goToLua(ls, custom{x: 5})
	// Falls back to string representation.
	_, ok := lv.(lua.LString)
	assert.True(t, ok)
}

func TestLuaToGo_LuaFunction(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	fn := ls.NewFunction(func(ls *lua.LState) int { return 0 })
	// Unknown Lua types return nil.
	assert.Nil(t, luaToGo(fn))
}

// --- Table with string and numeric keys (mixed) ---

func TestRunTransform_MixedTable(t *testing.T) {
	result, err := runTransform(`
		local t = {name = "test"}
		t[1] = "first"
		return t
	`, map[string]any{}, "{}")
	require.NoError(t, err)
	// Mixed table has both string and numeric keys → map output.
	m, ok := result["name"]
	assert.True(t, ok)
	assert.Equal(t, "test", m)
}

// --- String library usage in transform ---

func TestRunTransform_StringLibrary(t *testing.T) {
	result, err := runTransform(`
		return {upper = string.upper("hello"), sub = string.sub("abcdef", 1, 3)}
	`, map[string]any{}, "{}")
	require.NoError(t, err)
	assert.Equal(t, "HELLO", result["upper"])
	assert.Equal(t, "abc", result["sub"])
}

// --- Math library usage ---

func TestRunTransform_MathLibrary(t *testing.T) {
	result, err := runTransform(`
		return {pi = math.pi, ceil = math.ceil(3.2)}
	`, map[string]any{}, "{}")
	require.NoError(t, err)
	assert.InDelta(t, 3.14159, result["pi"].(float64), 0.001)
	assert.Equal(t, float64(4), result["ceil"])
}

// --- Table library usage ---

func TestRunTransform_TableLibrary(t *testing.T) {
	result, err := runTransform(`
		local t = {3, 1, 2}
		table.sort(t)
		return {sorted = t}
	`, map[string]any{}, "{}")
	require.NoError(t, err)
	sorted := result["sorted"].([]any)
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, sorted)
}
