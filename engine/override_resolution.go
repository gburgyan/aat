package engine

// recordOverrideValue records that an environment or overlay override set input
// to value, replacing the resolution record of the value it overwrote, so the
// archive's decision trail shows what the request sent and where it came from.
func recordOverrideValue(resolutions []ValueResolution, input string, value any) []ValueResolution {
	record := ValueResolution{
		InputName:  input,
		Source:     "override_value",
		RawValue:   value,
		FinalValue: value,
		PoolIndex:  -1,
	}
	for i := range resolutions {
		if resolutions[i].InputName == input {
			resolutions[i] = record
			return resolutions
		}
	}
	return append(resolutions, record)
}
