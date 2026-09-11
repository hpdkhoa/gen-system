// generator/internal/bench/springgen.go — v23.14 (Phase 6): a benchmark suite
// for the Spring Boot / Java 17 backend generator. It generates the project for
// a fixed schema and asserts structural correctness: the expected per-entity
// files exist (entity, repository, DTO, service, controller), a belongsTo
// relation becomes an FK field, the controller carries the REST mapping, and
// the pom targets Java 17 / Spring Boot 3.x. It does NOT run `mvn` (Maven
// Central isn't reachable from an offline bench box) — the Java sources'
// compile-correctness is verified separately with javac against stubs; full
// `mvn package` is the documented verify-on-box step.
package bench

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hpdkhoa/gen-system/generator/internal/engine"
	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

func SpringGeneration() Suite {
	t0 := time.Now()
	s := Suite{Name: "spring_generation"}

	tmp, err := os.MkdirTemp("", "genbench-spring-*")
	if err != nil {
		s.Cases = append(s.Cases, Case{Name: "mktemp", Pass: false, Note: err.Error()})
		s.Duration = time.Since(t0)
		return s
	}
	defer os.RemoveAll(tmp)

	prev, had := os.LookupEnv("GEN_BACKEND")
	_ = os.Setenv("GEN_BACKEND", "spring")
	defer func() {
		if had {
			_ = os.Setenv("GEN_BACKEND", prev)
		} else {
			_ = os.Unsetenv("GEN_BACKEND")
		}
	}()

	entities := []schema.Entity{
		{Name: "Order", Fields: []schema.Field{
			{Name: "total", Type: "float"},
			{Name: "status", Type: "string"},
		}, Relations: []schema.Relation{
			{Type: "belongsTo", Target: "Customer", ForeignKey: "CustomerID"},
		}},
		{Name: "Customer", Fields: []schema.Field{
			{Name: "name", Type: "string"},
		}},
	}

	engine.GenerateBackendApp(entities, tmp)

	pkg := filepath.Join(tmp, "backend", "src", "main", "java", "com", "gensystem", "app")

	// ── per-entity files exist ──────────────────────────────────────
	perEntity := []string{"%s.java", "%sRepository.java", "%sRequest.java", "%sService.java", "%sController.java"}
	for _, e := range entities {
		for _, tpl := range perEntity {
			rel := strings.Replace(tpl, "%s", e.Name, 1)
			_, statErr := os.Stat(filepath.Join(pkg, rel))
			s.Cases = append(s.Cases, Case{
				Name: "exists: " + rel,
				Pass: statErr == nil,
				Want: "present",
				Got:  boolWord(statErr == nil, "present", "missing"),
			})
		}
	}

	// project-level files
	for _, f := range []struct{ name, rel string }{
		{"pom.xml", "backend/pom.xml"},
		{"Application.java", "backend/src/main/java/com/gensystem/app/Application.java"},
		{"application.properties", "backend/src/main/resources/application.properties"},
		{"ValidationErrorHandler.java", "backend/src/main/java/com/gensystem/app/ValidationErrorHandler.java"},
		{"Dockerfile", "Dockerfile"},
		{"build-all.sh", "build-all.sh"},
	} {
		_, statErr := os.Stat(filepath.Join(tmp, f.rel))
		s.Cases = append(s.Cases, Case{
			Name: "exists: " + f.name,
			Pass: statErr == nil,
			Want: "present",
			Got:  boolWord(statErr == nil, "present", "missing"),
		})
	}

	// ── content assertions ──────────────────────────────────────────
	if b, e := os.ReadFile(filepath.Join(pkg, "Order.java")); e == nil {
		fk := strings.Contains(string(b), "customer_id") && strings.Contains(string(b), "customerId")
		s.Cases = append(s.Cases, Case{
			Name: "belongsTo → FK field (customerId)",
			Pass: fk,
			Want: "customerId + column customer_id",
			Got:  boolWord(fk, "present", "missing"),
		})
		idCol := strings.Contains(string(b), "@GeneratedValue") && strings.Contains(string(b), "private Long id;")
		s.Cases = append(s.Cases, Case{
			Name: "entity has @GeneratedValue id",
			Pass: idCol, Want: "auto id", Got: boolWord(idCol, "present", "missing"),
		})
	}
	if b, e := os.ReadFile(filepath.Join(pkg, "OrderController.java")); e == nil {
		routed := strings.Contains(string(b), `@RequestMapping("/orders")`) &&
			strings.Contains(string(b), "@PostMapping") &&
			strings.Contains(string(b), "@Valid @RequestBody")
		s.Cases = append(s.Cases, Case{
			Name: "controller REST mapping + validation",
			Pass: routed,
			Want: "/orders, @PostMapping, @Valid",
			Got:  boolWord(routed, "present", "missing"),
		})
	}
	if b, e := os.ReadFile(filepath.Join(tmp, "backend", "pom.xml")); e == nil {
		ok := strings.Contains(string(b), "<java.version>17</java.version>") &&
			strings.Contains(string(b), "spring-boot-starter-parent") &&
			strings.Contains(string(b), "3.2.")
		s.Cases = append(s.Cases, Case{
			Name: "pom targets Java 17 / Spring Boot 3.2.x",
			Pass: ok, Want: "java 17 + boot 3.2", Got: boolWord(ok, "ok", "mismatch"),
		})
	}

	s.Notes = "Java sources compile-checked with javac 17 against stubs (see notes); full `mvn package` is verify-on-box (Maven Central not reachable offline)."
	s.Duration = time.Since(t0)
	return s
}
