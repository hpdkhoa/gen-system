// generator/internal/deps/registry.go — v23.11 (Phase 1): stable lib/pq pin
// raised v1.10.9 → v1.12.3 to match the version emitGoMod now writes and the
// version gen-system itself vendors (a go.sum for it is therefore always
// available offline). Keeps `gen deps stable` from reintroducing the
// missing-go.sum build failure.
package deps

type Versions struct {
	Frontend map[string]string
	Mobile   map[string]string
	Backend  map[string]string
}

var Registry = map[string]Versions{

	"stable": {
		Frontend: map[string]string{
			"react":                "18.2.0",
			"react-dom":            "18.2.0",
			"vite":                 "5.4.21",
			"@vitejs/plugin-react": "4.2.1",
		},
		Mobile: map[string]string{
			"expo":                           "51.0.28",
			"react":                          "18.2.0",
			"react-native":                   "0.74.5",
			"@react-navigation/native":       "6.1.17",
			"@react-navigation/native-stack": "6.9.26",
			"react-native-screens":           "3.31.1",
			"react-native-safe-area-context": "4.10.5",
		},
		Backend: map[string]string{
			"github.com/lib/pq": "v1.12.3",
		},
	},

	"latest": {
		Frontend: map[string]string{
			"react":                "latest",
			"react-dom":            "latest",
			"vite":                 "latest",
			"@vitejs/plugin-react": "latest",
		},
		Mobile: map[string]string{
			"expo":         "latest",
			"react":        "latest",
			"react-native": "latest",
		},
		Backend: map[string]string{
			"github.com/lib/pq": "latest",
		},
	},
}
