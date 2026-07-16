package compiler

// =========================
// IR (Intermediate Representation)
// =========================

type SchemaIR struct {
	Entities  []EntityIR
	Relations []RelationIR
}

type EntityIR struct {
	Name   string
	Fields []FieldIR
}

type FieldIR struct {
	Name      string
	Type      string
	IsPrimary bool
	IsForeign bool
	RefEntity string // for FK inference
}

type RelationIR struct {
	FromEntity string
	ToEntity   string
	FromField  string
	ToField    string
	Type       string // 1:1, 1:N, N:M
}
