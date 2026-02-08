// Package domain provides domain knowledge: concepts, types, and value pools.
//
// A KnowledgeBase describes API-specific terminology, data types, and
// representative values that downstream packages (intent, engine, validate)
// use for plan generation, value resolution, and semantic validation.
//
// Domain knowledge is loaded from YAML files via Parse/ParseFile and can
// be merged from multiple sources via Merge. The FormatForPrompt methods
// serialize knowledge into structured text suitable for LLM prompt context.
//
// This is a leaf package with zero AAT imports.
package domain
