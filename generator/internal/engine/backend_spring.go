// generator/internal/engine/backend_spring.go — v23.14 (Phase 6): generates a
// Spring Boot 3.2 / Java 17 backend as an ALTERNATIVE target to the Go backend.
// Opt in with GEN_BACKEND=spring (mirrors GEN_BACKEND=postgres). Produces a
// standard Maven project: JPA @Entity per schema entity, a Spring Data
// repository, a @RestController with full CRUD, a DTO/validation layer, and an
// application bootstrap — plus a pom.xml pinned to Spring Boot 3.2.x on Java 17.
//
// The Go backend remains the in-sandbox compile guarantee. For Spring, the Java
// SOURCES are generated deterministically and (this session) compile-checked
// with a real javac 17 against minimal stubs; the full `mvn package` that
// resolves Spring Boot from Maven Central is the documented verify-on-box step
// (Maven Central is not reachable from the offline build box the way the Go
// toolchain debs were).
//
// GENERATE-side only — must never be linked by understand-server (archtest
// enforces). Lives in package engine alongside the Go/postgres generators and
// reuses their identifier/sanitiser helpers (exportName, splitIdent, jsonTag,
// fieldsForEntity, relationFields, plural, routeName, writeFileMk).
package engine

import (
	"errors"
	"os"
	"strings"

	"github.com/hpdkhoa/gen-system/generator/internal/schema"
)

// springEnabled reports whether Spring Boot generation is selected.
func springEnabled() bool {
	return strings.EqualFold(os.Getenv("GEN_BACKEND"), "spring")
}

const springGroupID = "com.gensystem.app"
const springBootVersion = "3.2.5"
const springJavaVersion = "17"

// springPkgPath is the on-disk package directory under src/main/java.
const springPkgPath = "backend/src/main/java/com/gensystem/app"

// ─── Java type + identifier helpers ──────────────────────────
//
// We deliberately do NOT reuse goType (Go types). Java needs boxed reference
// types for JPA columns. javaFieldName produces lowerCamelCase; javaClassName
// reuses exportName (PascalCase). columnName reuses jsonTag (snake_case) so the
// DB columns match the Go/Postgres generator's convention.

func javaType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "int", "integer":
		return "Integer"
	case "long", "bigint":
		return "Long"
	case "bool", "boolean":
		return "Boolean"
	case "float", "float64", "double", "decimal":
		return "Double"
	case "time", "datetime", "date", "timestamp":
		return "java.time.Instant"
	default:
		return "String"
	}
}

// javaFieldName → lowerCamelCase valid Java identifier (e.g. "due date" ->
// "dueDate", "UserID" via splitIdent -> "userid"? no: splitIdent("UserID")=
// ["UserID"], so we route relation FK names through splitCamel first — done by
// relationFields — and entity fields through here).
func javaFieldName(s string) string {
	parts := splitIdent(s)
	if len(parts) == 0 {
		return "field"
	}
	var b strings.Builder
	for i, p := range parts {
		lp := strings.ToLower(p)
		if i == 0 {
			b.WriteString(lp)
		} else {
			b.WriteString(strings.ToUpper(lp[:1]) + lp[1:])
		}
	}
	out := b.String()
	if out[0] >= '0' && out[0] <= '9' {
		out = "f" + out
	}
	if javaReserved[out] {
		out = out + "_"
	}
	return out
}

func javaClassName(s string) string { return exportName(s) }

// javaGetter / javaSetter produce accessor names.
func javaGetter(field string) string {
	return "get" + strings.ToUpper(field[:1]) + field[1:]
}
func javaSetter(field string) string {
	return "set" + strings.ToUpper(field[:1]) + field[1:]
}

var javaReserved = map[string]bool{
	"abstract": true, "assert": true, "boolean": true, "break": true, "byte": true,
	"case": true, "catch": true, "char": true, "class": true, "const": true,
	"continue": true, "default": true, "do": true, "double": true, "else": true,
	"enum": true, "extends": true, "final": true, "finally": true, "float": true,
	"for": true, "goto": true, "if": true, "implements": true, "import": true,
	"instanceof": true, "int": true, "interface": true, "long": true, "native": true,
	"new": true, "package": true, "private": true, "protected": true, "public": true,
	"return": true, "short": true, "static": true, "strictfp": true, "super": true,
	"switch": true, "synchronized": true, "this": true, "throw": true, "throws": true,
	"transient": true, "try": true, "void": true, "volatile": true, "while": true,
}

// ─── project skeleton: pom.xml + application bootstrap ────────

// GenerateSpringApp writes the whole Spring Boot project for the entity set.
// Called from GenerateBackendApp when springEnabled(). It does NOT emit the Go
// files (the caller gates those out in Spring mode).
//
// v26.59 (N07): every writeFileMk error was dropped, so a file that could not
// be written was missing and the run still reported success. The function now
// tries every write and returns the errors joined, as D029 did for the Go
// backend.
func GenerateSpringApp(entities []schema.Entity, path string) error {
	errs := []error{
		writeFileMk(path+"/backend/pom.xml", springPOM()),
		writeFileMk(path+"/"+springPkgPath+"/Application.java", springApplicationJava()),
		writeFileMk(path+"/backend/src/main/resources/application.properties", springAppProps()),
	}
	for _, e := range entities {
		errs = append(errs,
			genSpringEntity(e, path),
			genSpringRepository(e, path),
			genSpringDTO(e, path),
			genSpringService(e, path),
			genSpringController(e, path),
		)
	}
	errs = append(errs, genSpringErrorHandler(path))
	return errors.Join(errs...)
}

func springPOM() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>` + springBootVersion + `</version>
    <relativePath/>
  </parent>

  <groupId>` + springGroupID + `</groupId>
  <artifactId>generated-backend</artifactId>
  <version>0.1.0</version>
  <packaging>jar</packaging>

  <properties>
    <java.version>` + springJavaVersion + `</java.version>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
  </properties>

  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-validation</artifactId>
    </dependency>
    <!-- In-memory H2 by default so the app runs with no external DB; swap the
         datasource in application.properties for Postgres. -->
    <dependency>
      <groupId>com.h2database</groupId>
      <artifactId>h2</artifactId>
      <scope>runtime</scope>
    </dependency>
    <dependency>
      <groupId>org.postgresql</groupId>
      <artifactId>postgresql</artifactId>
      <scope>runtime</scope>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-test</artifactId>
      <scope>test</scope>
    </dependency>
  </dependencies>

  <build>
    <plugins>
      <plugin>
        <groupId>org.springframework.boot</groupId>
        <artifactId>spring-boot-maven-plugin</artifactId>
      </plugin>
    </plugins>
  </build>
</project>
`
}

func springApplicationJava() string {
	return `package com.gensystem.app;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication
public class Application {
    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }
}
`
}

func springAppProps() string {
	return `# Generated by gen-system. Defaults to an in-memory H2 database so the app
# boots with no external dependency. For Postgres, comment the H2 block and set
# the spring.datasource.* values for your server.
spring.datasource.url=jdbc:h2:mem:appdb;DB_CLOSE_DELAY=-1
spring.datasource.driverClassName=org.h2.Driver
spring.datasource.username=sa
spring.datasource.password=
spring.jpa.database-platform=org.hibernate.dialect.H2Dialect
spring.jpa.hibernate.ddl-auto=update
server.port=8080

# Postgres (uncomment + edit):
# spring.datasource.url=jdbc:postgresql://localhost:5432/app
# spring.datasource.username=app
# spring.datasource.password=secret
# spring.jpa.database-platform=org.hibernate.dialect.PostgreSQLDialect
`
}

// ─── per-entity: JPA @Entity ─────────────────────────────────

func genSpringEntity(e schema.Entity, path string) error {
	cls := javaClassName(e.Name)
	table := tableName(e)
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import jakarta.persistence.*;\n\n")
	sb.WriteString("@Entity\n")
	sb.WriteString("@Table(name = \"" + table + "\")\n")
	sb.WriteString("public class " + cls + " {\n\n")

	// id (always present; backend-assigned)
	sb.WriteString("    @Id\n")
	sb.WriteString("    @GeneratedValue(strategy = GenerationType.IDENTITY)\n")
	sb.WriteString("    private Long id;\n\n")

	for _, f := range fieldsForEntity(e) {
		jt := javaType(f.Type)
		fn := javaFieldName(f.Name)
		col := jsonTag(f.Name)
		sb.WriteString("    @Column(name = \"" + col + "\")\n")
		sb.WriteString("    private " + jt + " " + fn + ";\n\n")
	}

	// accessors
	sb.WriteString("    public Long getId() { return id; }\n")
	sb.WriteString("    public void setId(Long id) { this.id = id; }\n\n")
	for _, f := range fieldsForEntity(e) {
		jt := javaType(f.Type)
		fn := javaFieldName(f.Name)
		sb.WriteString("    public " + jt + " " + javaGetter(fn) + "() { return " + fn + "; }\n")
		sb.WriteString("    public void " + javaSetter(fn) + "(" + jt + " " + fn + ") { this." + fn + " = " + fn + "; }\n\n")
	}
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/"+cls+".java", sb.String())
}

// ─── per-entity: Spring Data repository ──────────────────────

func genSpringRepository(e schema.Entity, path string) error {
	cls := javaClassName(e.Name)
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import org.springframework.data.jpa.repository.JpaRepository;\n")
	sb.WriteString("import org.springframework.stereotype.Repository;\n\n")
	sb.WriteString("@Repository\n")
	sb.WriteString("public interface " + cls + "Repository extends JpaRepository<" + cls + ", Long> {\n")
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/"+cls+"Repository.java", sb.String())
}

// ─── per-entity: request DTO with validation ─────────────────

func genSpringDTO(e schema.Entity, path string) error {
	cls := javaClassName(e.Name)
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import jakarta.validation.constraints.*;\n\n")
	sb.WriteString("// Request body for create/update. id is server-assigned and not accepted here.\n")
	sb.WriteString("public class " + cls + "Request {\n\n")
	for _, f := range fieldsForEntity(e) {
		jt := javaType(f.Type)
		fn := javaFieldName(f.Name)
		// String fields get @NotNull so a missing required field is a 400.
		if jt == "String" {
			sb.WriteString("    @NotNull(message = \"" + fn + " is required\")\n")
		}
		sb.WriteString("    private " + jt + " " + fn + ";\n\n")
	}
	for _, f := range fieldsForEntity(e) {
		jt := javaType(f.Type)
		fn := javaFieldName(f.Name)
		sb.WriteString("    public " + jt + " " + javaGetter(fn) + "() { return " + fn + "; }\n")
		sb.WriteString("    public void " + javaSetter(fn) + "(" + jt + " " + fn + ") { this." + fn + " = " + fn + "; }\n\n")
	}
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/"+cls+"Request.java", sb.String())
}

// ─── per-entity: service ─────────────────────────────────────

func genSpringService(e schema.Entity, path string) error {
	cls := javaClassName(e.Name)
	repo := lowerFirstSpring(cls) + "Repository"
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import org.springframework.stereotype.Service;\n")
	sb.WriteString("import java.util.List;\n")
	sb.WriteString("import java.util.Optional;\n\n")
	sb.WriteString("@Service\n")
	sb.WriteString("public class " + cls + "Service {\n\n")
	sb.WriteString("    private final " + cls + "Repository " + repo + ";\n\n")
	sb.WriteString("    public " + cls + "Service(" + cls + "Repository " + repo + ") {\n")
	sb.WriteString("        this." + repo + " = " + repo + ";\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    public List<" + cls + "> findAll() { return " + repo + ".findAll(); }\n\n")
	sb.WriteString("    public Optional<" + cls + "> findById(Long id) { return " + repo + ".findById(id); }\n\n")

	// create from request
	sb.WriteString("    public " + cls + " create(" + cls + "Request req) {\n")
	sb.WriteString("        " + cls + " e = new " + cls + "();\n")
	sb.WriteString(applyRequestToEntity(e))
	sb.WriteString("        return " + repo + ".save(e);\n")
	sb.WriteString("    }\n\n")

	// update
	sb.WriteString("    public Optional<" + cls + "> update(Long id, " + cls + "Request req) {\n")
	sb.WriteString("        return " + repo + ".findById(id).map(e -> {\n")
	sb.WriteString(applyRequestToEntityIndented(e))
	sb.WriteString("            return " + repo + ".save(e);\n")
	sb.WriteString("        });\n")
	sb.WriteString("    }\n\n")

	// delete
	sb.WriteString("    public boolean delete(Long id) {\n")
	sb.WriteString("        if (!" + repo + ".existsById(id)) { return false; }\n")
	sb.WriteString("        " + repo + ".deleteById(id);\n")
	sb.WriteString("        return true;\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/"+cls+"Service.java", sb.String())
}

func applyRequestToEntity(e schema.Entity) string {
	var sb strings.Builder
	for _, f := range fieldsForEntity(e) {
		fn := javaFieldName(f.Name)
		sb.WriteString("        e." + javaSetter(fn) + "(req." + javaGetter(fn) + "());\n")
	}
	return sb.String()
}

func applyRequestToEntityIndented(e schema.Entity) string {
	var sb strings.Builder
	for _, f := range fieldsForEntity(e) {
		fn := javaFieldName(f.Name)
		sb.WriteString("            e." + javaSetter(fn) + "(req." + javaGetter(fn) + "());\n")
	}
	return sb.String()
}

// ─── per-entity: REST controller ─────────────────────────────

func genSpringController(e schema.Entity, path string) error {
	cls := javaClassName(e.Name)
	svc := lowerFirstSpring(cls) + "Service"
	coll := plural(routeName(e.Name)) // e.g. "orders"
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import org.springframework.http.ResponseEntity;\n")
	sb.WriteString("import org.springframework.web.bind.annotation.*;\n")
	sb.WriteString("import jakarta.validation.Valid;\n")
	sb.WriteString("import java.util.List;\n\n")
	sb.WriteString("@RestController\n")
	sb.WriteString("@RequestMapping(\"/" + coll + "\")\n")
	sb.WriteString("public class " + cls + "Controller {\n\n")
	sb.WriteString("    private final " + cls + "Service " + svc + ";\n\n")
	sb.WriteString("    public " + cls + "Controller(" + cls + "Service " + svc + ") {\n")
	sb.WriteString("        this." + svc + " = " + svc + ";\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    @GetMapping\n")
	sb.WriteString("    public List<" + cls + "> list() { return " + svc + ".findAll(); }\n\n")

	sb.WriteString("    @GetMapping(\"/{id}\")\n")
	sb.WriteString("    public ResponseEntity<" + cls + "> get(@PathVariable Long id) {\n")
	sb.WriteString("        return " + svc + ".findById(id)\n")
	sb.WriteString("            .map(ResponseEntity::ok)\n")
	sb.WriteString("            .orElse(ResponseEntity.notFound().build());\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    @PostMapping\n")
	sb.WriteString("    public ResponseEntity<" + cls + "> create(@Valid @RequestBody " + cls + "Request req) {\n")
	sb.WriteString("        " + cls + " saved = " + svc + ".create(req);\n")
	sb.WriteString("        return ResponseEntity.status(201).body(saved);\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    @PutMapping(\"/{id}\")\n")
	sb.WriteString("    public ResponseEntity<" + cls + "> update(@PathVariable Long id, @Valid @RequestBody " + cls + "Request req) {\n")
	sb.WriteString("        return " + svc + ".update(id, req)\n")
	sb.WriteString("            .map(ResponseEntity::ok)\n")
	sb.WriteString("            .orElse(ResponseEntity.notFound().build());\n")
	sb.WriteString("    }\n\n")

	sb.WriteString("    @DeleteMapping(\"/{id}\")\n")
	sb.WriteString("    public ResponseEntity<Void> delete(@PathVariable Long id) {\n")
	sb.WriteString("        return " + svc + ".delete(id)\n")
	sb.WriteString("            ? ResponseEntity.noContent().build()\n")
	sb.WriteString("            : ResponseEntity.notFound().build();\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/"+cls+"Controller.java", sb.String())
}

// ─── global validation error handler (400 on @Valid failure) ─

func genSpringErrorHandler(path string) error {
	var sb strings.Builder
	sb.WriteString("package com.gensystem.app;\n\n")
	sb.WriteString("import org.springframework.http.HttpStatus;\n")
	sb.WriteString("import org.springframework.http.ResponseEntity;\n")
	sb.WriteString("import org.springframework.web.bind.MethodArgumentNotValidException;\n")
	sb.WriteString("import org.springframework.web.bind.annotation.ExceptionHandler;\n")
	sb.WriteString("import org.springframework.web.bind.annotation.RestControllerAdvice;\n")
	sb.WriteString("import java.util.HashMap;\n")
	sb.WriteString("import java.util.Map;\n\n")
	sb.WriteString("@RestControllerAdvice\n")
	sb.WriteString("public class ValidationErrorHandler {\n\n")
	sb.WriteString("    @ExceptionHandler(MethodArgumentNotValidException.class)\n")
	sb.WriteString("    public ResponseEntity<Map<String, String>> handle(MethodArgumentNotValidException ex) {\n")
	sb.WriteString("        Map<String, String> errors = new HashMap<>();\n")
	sb.WriteString("        ex.getBindingResult().getFieldErrors().forEach(fe ->\n")
	sb.WriteString("            errors.put(fe.getField(), fe.getDefaultMessage()));\n")
	sb.WriteString("        return ResponseEntity.status(HttpStatus.BAD_REQUEST).body(errors);\n")
	sb.WriteString("    }\n")
	sb.WriteString("}\n")
	return writeFileMk(path+"/"+springPkgPath+"/ValidationErrorHandler.java", sb.String())
}

func lowerFirstSpring(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// ─── Spring ship scripts + Dockerfile (Maven-based) ──────────

// writeSpringShipScripts writes build/bundle scripts that use Maven instead of
// `go build`. The actual `mvn package` (which resolves Spring Boot from Maven
// Central) is a verify-on-box step; the scripts are written + syntax-checkable
// here. Produces an executable Spring Boot fat-jar.
//
// v26.59 (N07): returns the joined write errors instead of dropping them.
func writeSpringShipScripts(path string) error {
	return errors.Join(
		writeFileMk(path+"/VERSION", "0.1.0\n"),
		writeFileMk(path+"/build-all.sh", springBuildAllSh),
		writeFileMk(path+"/bundle.sh", springBundleSh),
		writeEnvExampleIfAbsent(path+"/.env.example", springEnvExample), // v26.59 (N08): GenerateEnvFiles owns .env.example
		writeFileMk(path+"/deploy/generated-backend.service", springSystemdUnit),
		writeFileMk(path+"/deploy/run.sh", springRunSh),
	)
}

const springBuildAllSh = `#!/usr/bin/env bash
# build-all.sh — build the Spring Boot backend (Java 17) into an executable jar.
# Requires a JDK 17 + Maven on the build box. This script BUILDS only.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

DIST="dist"
mkdir -p "$DIST"

echo "→ Backend (Spring Boot, Java 17)…"
( cd backend && mvn -B -q clean package -DskipTests )
# Spring Boot repackages an executable fat-jar under backend/target/*.jar
JAR="$(ls backend/target/*.jar | grep -v '\.original$' | head -n1)"
cp "$JAR" "$DIST/server.jar"
echo "  built $DIST/server.jar"

if [ -d web ] && [ -f web/package.json ]; then
  echo "→ Web (ReactJS)…"
  ( cd web && npm ci && npm run build )
  if [ -d web/build ]; then cp -r web/build "$DIST/web"; elif [ -d web/dist ]; then cp -r web/dist "$DIST/web"; fi
fi

echo "✓ build-all complete. Artifacts in ./$DIST/"
`

const springBundleSh = `#!/usr/bin/env bash
# bundle.sh — assemble a deployable tarball from ./dist/.
# Usage: ./bundle.sh [--version X.Y.Z]
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

APP="generated-backend"
VERSION=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done
if [ -z "$VERSION" ]; then
  if [ -f VERSION ]; then VERSION="$(tr -d '[:space:]' < VERSION)"; else VERSION="0.0.0"; fi
fi

if [ ! -f dist/server.jar ]; then
  echo "✗ dist/server.jar not found — run ./build-all.sh first." >&2
  exit 1
fi

STAGE="$(mktemp -d)"
ROOT="$STAGE/$APP"
mkdir -p "$ROOT"
cp dist/server.jar "$ROOT/server.jar"
[ -d dist/web ] && cp -r dist/web "$ROOT/web" || true
[ -f .env.example ] && cp .env.example "$ROOT/" || true
[ -d deploy ] && cp -r deploy "$ROOT/deploy" || true

TARBALL="$APP-$VERSION-jar.tar.gz"
tar -C "$STAGE" -czf "$TARBALL" "$APP"
rm -rf "$STAGE"
echo "✓ bundled $TARBALL (run with: java -jar server.jar)"
`

const springEnvExample = `# Copy to .env and edit. Spring reads these via the systemd unit / run.sh.
# Defaults to in-memory H2 (see application.properties). For Postgres set:
# SPRING_DATASOURCE_URL=jdbc:postgresql://localhost:5432/app
# SPRING_DATASOURCE_USERNAME=app
# SPRING_DATASOURCE_PASSWORD=secret
# SERVER_PORT=8080
`

const springSystemdUnit = `[Unit]
Description=generated-backend (gen-system / Spring Boot)
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/generated-backend
ExecStart=/usr/bin/java -jar /opt/generated-backend/server.jar
Restart=on-failure
EnvironmentFile=-/opt/generated-backend/.env

[Install]
WantedBy=multi-user.target
`

const springRunSh = `#!/usr/bin/env bash
# run.sh — start the Spring Boot jar from an unpacked bundle.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[ -f .env ] && set -a && . ./.env && set +a || true
exec java -jar ./server.jar
`

// GenerateSpringDockerfile writes a multi-stage Dockerfile that builds the jar
// with Maven and runs it on a JRE base. docker build is verify-on-box.
//
// v26.59 (N07): returns the joined write errors instead of dropping them.
func GenerateSpringDockerfile(path string) error {
	return errors.Join(
		writeFileMk(path+"/Dockerfile", springDockerfile),
		writeFileMk(path+"/.dockerignore", "dist/\nbackend/target/\nweb/node_modules/\n*.tar.gz\n.git/\n"),
	)
}

const springDockerfile = `# Multi-stage build for the generated Spring Boot backend (Java 17).
FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /src
COPY backend/ ./backend/
WORKDIR /src/backend
RUN mvn -B -q clean package -DskipTests

FROM eclipse-temurin:17-jre
WORKDIR /app
COPY --from=build /src/backend/target/*.jar /app/server.jar
EXPOSE 8080
ENTRYPOINT ["java", "-jar", "/app/server.jar"]
`
