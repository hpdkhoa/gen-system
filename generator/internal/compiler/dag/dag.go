package dag

// Minimal IR (decoupled from compiler)
type Entity struct {
	Name string
}

type Relation struct {
	FromEntity string
	ToEntity   string
	Type       string
}

type Schema struct {
	Entities  []Entity
	Relations []Relation
}

type Node struct {
	Name string
	Deps map[string]bool
}

// Build dependency graph
func Build(ir Schema) map[string]*Node {

	graph := map[string]*Node{}

	// create nodes
	for _, e := range ir.Entities {
		graph[e.Name] = &Node{
			Name: e.Name,
			Deps: map[string]bool{},
		}
	}

	// resolve dependencies
	for _, r := range ir.Relations {
		if r.Type == "N:1" {
			if node, ok := graph[r.FromEntity]; ok {
				node.Deps[r.ToEntity] = true
			}
		}
	}

	return graph
}

// topological ordering
func Order(graph map[string]*Node) []string {
	visited := map[string]bool{}
	var result []string

	var visit func(string)

	visit = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true

		for dep := range graph[n].Deps {
			visit(dep)
		}

		result = append(result, n)
	}

	for k := range graph {
		visit(k)
	}

	return result
}
