package ast

import (
	"strings"
	"testing"
)

func tsFE() Frontend { return ForLanguage(LangTS) }

func TestTSFrontend_Available(t *testing.T) {
	fe := tsFE()
	if !fe.Available() {
		t.Error("TSFrontend.Available() should always return true (pure Go)")
	}
}

func TestTSFrontend_Extensions(t *testing.T) {
	fe := tsFE()
	exts := fe.Extensions()
	if !contains(exts, ".ts") || !contains(exts, ".tsx") {
		t.Errorf("Extensions() = %v, want [.ts .tsx]", exts)
	}
}

// ─── Imports ──────────────────────────────────────────────────

func TestTSFrontend_ParseNamedImport(t *testing.T) {
	src := `import { Injectable, Inject } from '@angular/core'`
	m := parseTSMust(t, src)
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) != 1 {
		t.Fatalf("expected 1 import, got %d", len(imports))
	}
	if imports[0].ImportPath != "@angular/core" {
		t.Errorf("ImportPath = %q, want @angular/core", imports[0].ImportPath)
	}
	if !containsStr(imports[0].ImportNames, "Injectable") {
		t.Errorf("missing Injectable in ImportNames: %v", imports[0].ImportNames)
	}
}

func TestTSFrontend_ParseDefaultImport(t *testing.T) {
	src := `import React from 'react'`
	m := parseTSMust(t, src)
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) == 0 {
		t.Fatal("expected import")
	}
	if imports[0].ImportPath != "react" {
		t.Errorf("ImportPath = %q, want react", imports[0].ImportPath)
	}
}

func TestTSFrontend_ParseNamespaceImport(t *testing.T) {
	src := `import * as path from 'path'`
	m := parseTSMust(t, src)
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) == 0 {
		t.Fatal("expected import")
	}
	if !imports[0].ImportAll {
		t.Error("namespace import should set ImportAll=true")
	}
}

func TestTSFrontend_ParseSideEffectImport(t *testing.T) {
	src := `import './styles.css'`
	m := parseTSMust(t, src)
	imports := m.TopLevelByKind(NodeImport)
	if len(imports) == 0 {
		t.Fatal("expected side-effect import")
	}
}

// ─── Interfaces ───────────────────────────────────────────────

func TestTSFrontend_ParseInterface(t *testing.T) {
	src := `export interface User {
  id: number
  name: string
  email?: string
  createdAt: Date
}
`
	m := parseTSMust(t, src)
	ifaces := m.TopLevelByKind(NodeInterface)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	iface := ifaces[0]
	if iface.Name != "User" {
		t.Errorf("interface name = %q, want User", iface.Name)
	}
	if !iface.Exported {
		t.Error("User interface should be exported")
	}
	if len(iface.Fields) < 3 {
		t.Errorf("expected at least 3 fields, got %d: %v", len(iface.Fields), iface.Fields)
	}
	// email is optional
	for _, f := range iface.Fields {
		if f.Name == "email" && !f.Optional {
			t.Error("email field should be optional")
		}
	}
}

func TestTSFrontend_ParseInterfaceExtends(t *testing.T) {
	src := `export interface AdminUser extends User {
  role: string
}
`
	m := parseTSMust(t, src)
	ifaces := m.TopLevelByKind(NodeInterface)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if !containsStr(ifaces[0].Implements, "User") {
		t.Errorf("AdminUser should extend User, Implements=%v", ifaces[0].Implements)
	}
}

// ─── Type aliases ─────────────────────────────────────────────

func TestTSFrontend_ParseTypeAlias(t *testing.T) {
	src := `export type UserID = string
export type Status = 'active' | 'inactive' | 'pending'
`
	m := parseTSMust(t, src)
	aliases := m.TopLevelByKind(NodeTypeAlias)
	if len(aliases) != 2 {
		t.Fatalf("expected 2 type aliases, got %d", len(aliases))
	}
	names := map[string]bool{}
	for _, a := range aliases {
		names[a.Name] = true
	}
	if !names["UserID"] || !names["Status"] {
		t.Errorf("missing type alias, got names: %v", names)
	}
}

// ─── Classes ──────────────────────────────────────────────────

func TestTSFrontend_ParseClass(t *testing.T) {
	src := `export class UserRepository implements Repository<User> {
  private items: User[] = []

  findById(id: string): User | null {
    return this.items.find(u => u.id === id) || null
  }

  save(entity: User): void {
    this.items.push(entity)
  }
}
`
	m := parseTSMust(t, src)
	classes := m.TopLevelByKind(NodeClass)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(classes))
	}
	cls := classes[0]
	if cls.Name != "UserRepository" {
		t.Errorf("class name = %q, want UserRepository", cls.Name)
	}
	if !cls.Exported {
		t.Error("UserRepository should be exported")
	}
	if !containsStr(cls.Implements, "Repository") {
		t.Errorf("UserRepository should implement Repository, Implements=%v", cls.Implements)
	}
	// Should have methods as children
	if len(cls.Children) < 2 {
		t.Errorf("expected at least 2 method children, got %d", len(cls.Children))
	}
}

func TestTSFrontend_ParseAbstractClass(t *testing.T) {
	src := `export abstract class BaseService {
  abstract process(): void
}`
	m := parseTSMust(t, src)
	classes := m.TopLevelByKind(NodeClass)
	if len(classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(classes))
	}
	if !classes[0].Abstract {
		t.Error("class should be abstract")
	}
}

// ─── Functions ────────────────────────────────────────────────

func TestTSFrontend_ParseFunction(t *testing.T) {
	src := `export async function fetchUser(id: string): Promise<User> {
  const res = await fetch('/api/users/' + id)
  return res.json()
}
`
	m := parseTSMust(t, src)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(funcs))
	}
	fn := funcs[0]
	if fn.Name != "fetchUser" {
		t.Errorf("function name = %q, want fetchUser", fn.Name)
	}
	if !fn.Exported {
		t.Error("fetchUser should be exported")
	}
	if !fn.Async {
		t.Error("fetchUser should be async")
	}
}

// ─── Enums ────────────────────────────────────────────────────

func TestTSFrontend_ParseEnum(t *testing.T) {
	src := `export enum Direction {
  Up = 'UP',
  Down = 'DOWN',
  Left = 'LEFT',
  Right = 'RIGHT'
}
`
	m := parseTSMust(t, src)
	enums := m.TopLevelByKind(NodeEnum)
	if len(enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(enums))
	}
	enum := enums[0]
	if enum.Name != "Direction" {
		t.Errorf("enum name = %q, want Direction", enum.Name)
	}
	if len(enum.EnumMembers) != 4 {
		t.Errorf("expected 4 members, got %d", len(enum.EnumMembers))
	}
}

// ─── Variables ────────────────────────────────────────────────

func TestTSFrontend_ParseConst(t *testing.T) {
	src := `export const MAX_RETRIES = 3
export const API_URL: string = 'https://api.example.com'
`
	m := parseTSMust(t, src)
	vars := m.TopLevelByKind(NodeVariable)
	if len(vars) != 2 {
		t.Fatalf("expected 2 const variables, got %d", len(vars))
	}
}

// ─── NodeUnknown bodies ───────────────────────────────────────

func TestTSFrontend_FunctionBodyIsUnknown(t *testing.T) {
	src := `export function complex() {
  const x = someComplexExpression()
  return x
}`
	m := parseTSMust(t, src)
	funcs := m.TopLevelByKind(NodeFunction)
	if len(funcs) == 0 {
		t.Fatal("expected function")
	}
	// Body should be represented as NodeUnknown child
	var hasUnknown bool
	for _, child := range funcs[0].Children {
		if child.Kind == NodeUnknown && child.UnknownReason == "body_not_parsed" {
			hasUnknown = true
			break
		}
	}
	if !hasUnknown {
		t.Error("function body should produce NodeUnknown with reason=body_not_parsed")
	}
}

// ─── Checksum ─────────────────────────────────────────────────

func TestTSFrontend_Checksum(t *testing.T) {
	fe := tsFE()
	m1, _ := fe.Parse("a.ts", []byte("export const x = 1"))
	m2, _ := fe.Parse("a.ts", []byte("export const x = 2"))
	if m1.Checksum == "" {
		t.Error("checksum should be set")
	}
	if m1.Checksum == m2.Checksum {
		t.Error("different source should produce different checksums")
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func parseTSMust(t *testing.T, src string) *Module {
	t.Helper()
	fe := tsFE()
	m, err := fe.Parse("test.ts", []byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if m == nil {
		t.Fatal("parse returned nil module")
	}
	return m
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}
