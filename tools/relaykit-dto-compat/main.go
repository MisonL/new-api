// Command relaykit-dto-compat compares the local dto package against the
// relaykit dto package across four dimensions that a field-name diff misses:
//
//  1. field names        - which fields exist on same-named structs
//  2. JSON tags          - same name, different serialized key or omitempty
//  3. Go types           - same name, different type
//  4. embedded fields    - ast.Field.Names is empty for these, so they are
//     invisible to the name and type diffs, and function signatures, which a
//     name-only method diff never compares
//
// Usage:
//
//	go run . -local <dir> -kit <dir> [-format text|markdown] [-strict]
//
// Each directory holds the flattened .go sources of one side (non-test files).
// Use scripts/relaykit-dto-compat.sh to produce them from git.
//
// The expected result at the time of writing is recorded in
// docs/superpowers/specs/2026-09-30-relaykit-migration-design.md section 12.0.6.
// -strict turns that expectation into a gate: it exits non-zero unless the
// counts match, so a routine bump of upstream relaykit cannot silently widen
// the gap.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type fieldInfo struct {
	typ string
	tag string
}

// scan parses every .go file in dir and indexes, per named struct type, its
// named fields (with type and JSON tag) and its embedded field types; and,
// per function or method key, its signature.
func scan(dir string) (structs map[string]map[string]fieldInfo, embeds map[string][]string, sigs map[string]string, err error) {
	structs = map[string]map[string]fieldInfo{}
	embeds = map[string][]string{}
	sigs = map[string]string{}
	fset := token.NewFileSet()

	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return nil, nil, nil, fmt.Errorf("parse %s: %w", e.Name(), perr)
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.TYPE {
					continue
				}
				for _, spec := range decl.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					fields := map[string]fieldInfo{}
					for _, fl := range st.Fields.List {
						if len(fl.Names) == 0 {
							embeds[ts.Name.Name] = append(embeds[ts.Name.Name], render(fset, fl.Type))
							continue
						}
						tag := ""
						if fl.Tag != nil {
							tag = reflect.StructTag(strings.Trim(fl.Tag.Value, "`")).Get("json")
						}
						for _, nm := range fl.Names {
							fields[nm.Name] = fieldInfo{typ: render(fset, fl.Type), tag: tag}
						}
					}
					structs[ts.Name.Name] = fields
				}
			case *ast.FuncDecl:
				key := decl.Name.Name
				if decl.Recv != nil {
					key = strings.TrimPrefix(render(fset, decl.Recv.List[0].Type), "*") + "." + decl.Name.Name
				}
				sigs[key] = render(fset, decl.Type)
			}
		}
	}
	for k := range embeds {
		sort.Strings(embeds[k])
	}
	return structs, embeds, sigs, nil
}

func render(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	printer.Fprint(&sb, fset, n)
	return sb.String()
}

type report struct {
	fieldSetDiff   []string // types whose field-name sets differ
	tagDiff        []string
	typeDiff       []string
	embedDiff      []string
	sigDiff        []string
	sharedTypes    int
	sharedFuncs    int
	tagEquivalents []string // interface{} vs any, and similar spellings
}

// spellingsEquivalent reports whether two Go type spellings denote the same
// type in practice. The only pair that occurs here is interface{} vs any.
func spellingsEquivalent(a, b string) bool {
	norm := func(s string) string {
		s = strings.ReplaceAll(s, "interface{}", "any")
		return strings.ReplaceAll(s, " ", "")
	}
	return norm(a) == norm(b)
}

func build(localDir, kitDir string) (*report, error) {
	ls, le, lf, err := scan(localDir)
	if err != nil {
		return nil, err
	}
	ks, ke, kf, err := scan(kitDir)
	if err != nil {
		return nil, err
	}
	r := &report{}
	for name, lfields := range ls {
		kfields, ok := ks[name]
		if !ok {
			continue
		}
		r.sharedTypes++
		var nameSet, tagSet, typeSet []string
		for fn, lf := range lfields {
			kfi, ok := kfields[fn]
			if !ok {
				nameSet = append(nameSet, fn+" (local only)")
				continue
			}
			if lf.tag != kfi.tag {
				tagSet = append(tagSet, fmt.Sprintf("%s.%s local=%q kit=%q", name, fn, lf.tag, kfi.tag))
			}
			if lf.typ != kfi.typ {
				line := fmt.Sprintf("%s.%s local=%s kit=%s", name, fn, lf.typ, kfi.typ)
				if spellingsEquivalent(lf.typ, kfi.typ) {
					r.tagEquivalents = append(r.tagEquivalents, line)
				} else {
					typeSet = append(typeSet, line)
				}
			}
		}
		for fn := range kfields {
			if _, ok := lfields[fn]; !ok {
				nameSet = append(nameSet, fn+" (kit only)")
			}
		}
		if len(nameSet) > 0 {
			sort.Strings(nameSet)
			r.fieldSetDiff = append(r.fieldSetDiff, fmt.Sprintf("%s: %s", name, strings.Join(nameSet, ", ")))
		}
		if len(tagSet) > 0 {
			sort.Strings(tagSet)
			r.tagDiff = append(r.tagDiff, tagSet...)
		}
		if len(typeSet) > 0 {
			sort.Strings(typeSet)
			r.typeDiff = append(r.typeDiff, typeSet...)
		}
	}
	// embeds only gets a map entry when a struct has at least one embedded
	// field, so iterate the union: a type that is embedded on one side and
	// not the other is exactly the case worth reporting.
	embedNames := map[string]bool{}
	for name := range le {
		embedNames[name] = true
	}
	for name := range ke {
		embedNames[name] = true
	}
	for name := range embedNames {
		lemb := strings.Join(le[name], "|")
		kemb := strings.Join(ke[name], "|")
		if lemb != kemb {
			r.embedDiff = append(r.embedDiff, fmt.Sprintf("%s: local=%v kit=%v", name, le[name], ke[name]))
		}
	}
	for key, lsig := range lf {
		ksig, ok := kf[key]
		if !ok {
			continue
		}
		r.sharedFuncs++
		if lsig != ksig {
			line := fmt.Sprintf("%s\n  local%s\n  kit  %s", key, lsig, ksig)
			if spellingsEquivalent(lsig, ksig) {
				r.tagEquivalents = append(r.tagEquivalents, line)
			} else {
				r.sigDiff = append(r.sigDiff, line)
			}
		}
	}
	sort.Strings(r.fieldSetDiff)
	sort.Strings(r.embedDiff)
	sort.Strings(r.sigDiff)
	sort.Strings(r.tagEquivalents)
	return r, nil
}

func main() {
	local := flag.String("local", "", "directory with the local dto .go sources")
	kit := flag.String("kit", "", "directory with the relaykit dto .go sources")
	format := flag.String("format", "text", "output format: text or markdown")
	strict := flag.Bool("strict", false, "exit non-zero unless the counts match the recorded expectation")
	flag.Parse()
	if *local == "" || *kit == "" {
		fmt.Fprintln(os.Stderr, "both -local and -kit are required")
		os.Exit(2)
	}
	r, err := build(*local, *kit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "relaykit-dto-compat:", err)
		os.Exit(1)
	}

	md := *format == "markdown"
	section := func(title string, lines []string) {
		if md {
			fmt.Printf("\n### %s (%d)\n\n", title, len(lines))
		} else {
			fmt.Printf("\n### %s (%d)\n", title, len(lines))
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	}

	if md {
		fmt.Printf("同名类型 %d，同名函数/方法 %d\n", r.sharedTypes, r.sharedFuncs)
	} else {
		fmt.Printf("shared types=%d shared funcs=%d\n", r.sharedTypes, r.sharedFuncs)
	}
	section("字段名集合不同", r.fieldSetDiff)
	section("JSON tag 不同", r.tagDiff)
	section("Go 类型不同（含 interface{}/any 等价项）", r.typeDiff)
	section("嵌入字段不同", r.embedDiff)
	section("函数/方法签名不同", r.sigDiff)
	section("等价拼写（interface{} vs any），仅供参考", r.tagEquivalents)

	fmt.Printf("\n计数: fieldSet=%d tag=%d type=%d embed=%d sig=%d\n",
		len(r.fieldSetDiff), len(r.tagDiff), len(r.typeDiff), len(r.embedDiff), len(r.sigDiff))

	// Recorded expectation as of the 15th review round (2026-10-04), against
	// HEAD:dto and upstream/main:relaykit/dto. This is a point-in-time
	// baseline: when the local dto changes (another session commits) or the
	// pinned relaykit commit moves, these numbers change and -strict fails
	// until the baseline is re-recorded deliberately. That is the point -- it
	// is a gate against silent drift, not a permanent invariant.
	const (
		wantFieldSet = 33 // types whose field-name sets differ
		wantTag      = 5
		wantType     = 6  // excludes interface{}/any spellings, reported separately
		wantEmbed    = 0
		wantSig      = 12 // all IsStream; NewNotify's interface{}/any is a spelling
	)
	if *strict {
		got := []int{len(r.fieldSetDiff), len(r.tagDiff), len(r.typeDiff), len(r.embedDiff), len(r.sigDiff)}
		want := []int{wantFieldSet, wantTag, wantType, wantEmbed, wantSig}
		for i := range want {
			if got[i] != want[i] {
				fmt.Fprintf(os.Stderr, "strict: dimension %d changed: got %d want %d\n", i, got[i], want[i])
				os.Exit(1)
			}
		}
		fmt.Println("strict: counts match the recorded expectation")
	}
}
