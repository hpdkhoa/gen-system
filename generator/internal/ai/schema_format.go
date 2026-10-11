// generator/internal/ai/schema_format.go, v23.1 (round 3): the JSON Schema we
// pass to Ollama's `format` parameter so the model is constrained to emit a
// valid {entities:[{name, fields[]}]} document. This is the root-cause fix:
// with a schema-constrained decode the model cannot produce the bracket-
// mismatched, trailing-comma, tab-storm garbage that free-form prompting did.
// Fields are constrained to the array-of-{name,type} shape the engine wants,
// removing the object-vs-array ambiguity at the source.
package ai

import "encoding/json"

// entitiesSchema is a JSON Schema (draft-07 style, as Ollama accepts) for the
// schema-generation step.
var entitiesSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "entities": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": { "type": "string" },
          "fields": {
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "name": { "type": "string" },
                "type": { "type": "string", "enum": ["int", "string", "bool", "time", "float"] }
              },
              "required": ["name", "type"]
            }
          }
        },
        "required": ["name", "fields"]
      }
    }
  },
  "required": ["entities"]
}`)
