// Package intent implements LLM-powered prompt-to-plan transformation.
//
// The core function is Interpret, which takes a natural language prompt,
// an API graph, optional domain knowledge, and an LLM client, then
// returns a validated execution plan. It uses a two-call LLM architecture:
//
//  1. Goal analysis: identify the target node and classify constraints
//  2. Plan generation: fill in values, selections, and assertions for the
//     backward-chained subgraph
//
// Deterministic post-processing fixes dependsOn, cleanup steps, and
// selection defaults before final plan validation.
package intent
