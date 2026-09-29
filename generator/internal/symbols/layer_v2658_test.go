// D106: inferLayer tested "/model/" and friends on the raw OS path, so on Windows
// every folder rule missed and Layer stayed empty. It also used Contains for
// ".ad", so pkg/x.admin.go got layer "cagen".
package symbols

import (
	"path/filepath"
	"testing"
)

func TestD106_InferLayer(t *testing.T) {
	cases := map[string]string{
		filepath.FromSlash("internal/model/user.go"):         "model",
		filepath.FromSlash("internal/repo/user.go"):          "repo",
		filepath.FromSlash("internal/service/user.go"):       "service",
		filepath.FromSlash("internal/handler/user.go"):       "handler",
		filepath.FromSlash("internal/validation/user.go"):    "validation",
		filepath.FromSlash("C:/proj/internal/model/user.go"): "model",
		filepath.FromSlash("pkg/x.admin.go"):                 "",
		filepath.FromSlash("pkg/v1.addons/util.go"):          "",
		filepath.FromSlash("screens/order.ad"):               "cagen",
		filepath.FromSlash("screens/order.cagen"):            "cagen",
	}
	for path, want := range cases {
		if got := inferLayer(path); got != want {
			t.Errorf("inferLayer(%q) = %q, want %q", path, got, want)
		}
	}
}
