package schema

type Relation struct {
	Type       string `json:"type"`       // "belongsTo", "hasMany"
	Target     string `json:"target"`     // e.g. "User"
	ForeignKey string `json:"foreignKey"` // e.g. "UserID"
}

type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Entity struct {
	Name      string     `json:"name"`
	Fields    []Field    `json:"fields"`
	Relations []Relation `json:"relations"`
}

type SchemaFile struct {
	Entities []Entity `json:"entities"`
}
