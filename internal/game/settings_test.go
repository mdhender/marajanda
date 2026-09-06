// Copyright (c) 2026 Michael D Henderson.

package game

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// knobsNotInSettings are constants in world.go that do not shape terrain, so
// they have no place in a record of how to rebuild a world.
var knobsNotInSettings = map[string]string{}

// Settings has to name every constant that shapes a world, or the manifest it
// feeds is a record of some of the recipe, which is worse than none: it looks
// complete. Nothing stops a new constant being added without one, so this reads
// the source and checks.
func TestSettingsCoversEveryGeneratorConstant(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "world.go", nil, 0)
	if err != nil {
		t.Fatalf("parse world.go: %v", err)
	}

	recorded := make(map[string]struct{}, len(Settings()))
	for _, setting := range Settings() {
		recorded[setting.Name] = struct{}{}
	}

	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if name.Name == "_" {
					continue
				}
				if reason, excused := knobsNotInSettings[name.Name]; excused {
					if _, listed := recorded[name.Name]; listed {
						t.Errorf("%s is in Settings but excused as %s", name.Name, reason)
					}
					continue
				}
				if _, listed := recorded[name.Name]; !listed {
					t.Errorf("world.go declares %s but Settings does not record it; "+
						"add it, or excuse it in knobsNotInSettings with a reason",
						name.Name)
				}
			}
		}
	}
}

// Every recorded knob needs a value and an explanation, or the manifest is a
// list of numbers nobody can act on.
func TestSettingsAreDescribed(t *testing.T) {
	seen := make(map[string]struct{}, len(Settings()))
	for _, setting := range Settings() {
		if setting.Name == "" || setting.Value == nil {
			t.Errorf("setting %+v is missing a name or value", setting)
		}
		if strings.TrimSpace(setting.Note) == "" {
			t.Errorf("setting %q has no note saying what it does", setting.Name)
		}
		if _, duplicate := seen[setting.Name]; duplicate {
			t.Errorf("setting %q is recorded twice", setting.Name)
		}
		seen[setting.Name] = struct{}{}
	}
}
