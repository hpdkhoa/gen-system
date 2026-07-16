// generator/internal/ai/continue_prompt_v2324_test.go — v23.24 (task 4A):
// parser tolerance tests for the continue-delta contract. No model is involved.
package ai

import "testing"

func TestParseContinueDelta_Envelope_v2324(t *testing.T) {
	resp := `{"changes":[{"kind":"NEW","entity":"Customer","fields":{"email":"string"}}]}`
	d, err := ParseContinueDelta(resp)
	if err != nil {
		t.Fatalf("envelope parse failed: %v", err)
	}
	if len(d.Changes) != 1 || d.Changes[0].Entity != "Customer" {
		t.Fatalf("unexpected parse: %+v", d.Changes)
	}
}

func TestParseContinueDelta_BareArray_v2324(t *testing.T) {
	resp := `[{"kind":"ADD_FIELD","entity":"Order","field":"email","field_type":"string"}]`
	d, err := ParseContinueDelta(resp)
	if err != nil {
		t.Fatalf("bare-array parse failed: %v", err)
	}
	if len(d.Changes) != 1 || d.Changes[0].Kind != "ADD_FIELD" || d.Changes[0].Field != "email" {
		t.Fatalf("unexpected parse: %+v", d.Changes)
	}
}

func TestParseContinueDelta_Fenced_v2324(t *testing.T) {
	resp := "Here you go:\n```json\n{\"changes\":[{\"kind\":\"NEW_OP\",\"entity\":\"Order\",\"op\":\"Refund\"}]}\n```\n"
	d, err := ParseContinueDelta(resp)
	if err != nil {
		t.Fatalf("fenced parse failed: %v", err)
	}
	if len(d.Changes) != 1 || d.Changes[0].Op != "Refund" {
		t.Fatalf("unexpected parse: %+v", d.Changes)
	}
}

func TestParseContinueDelta_Garbage_v2324(t *testing.T) {
	if _, err := ParseContinueDelta("not json at all"); err == nil {
		t.Fatal("expected error on unparseable response")
	}
}
