// Command relaykit-dto-compat compares the local dto package against the
// relaykit dto package across the dimensions a field-name diff misses, and
// reports one section per dimension:
//
//  1. field names        - which fields exist on same-named structs
//  2. JSON tags          - same name, different serialized key or omitempty
//  3. Go types           - same name, different type
//  4. embedded fields    - ast.Field.Names is empty for these, so they are
//     invisible to the name and type diffs
//  5. declaration order  - encoding/json emits struct fields in declaration
//     order, so two sides with identical names/tags/types still serialize to
//     different bytes when a shared field is declared in a different position
//  6. function signatures - a name-only method diff never compares parameters
//     or return values
//  7. spelling equivalences - interface{} vs any is the same type spelled two
//     ways, so it is reported separately rather than counted as a difference
//
// Every section is gated by -strict: the printed sections and the expected
// counts come from one table (reportGroups), so a section cannot be printed
// without also being gated.
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
// named fields (with type and JSON tag), its declaration order, and its
// embedded field types; and, per function or method key, its signature.
//
// The field map loses declaration order, which is exactly what dimension 5
// needs, so the order is recorded separately as a slice.
func scan(dir string) (structs map[string]map[string]fieldInfo, orders map[string][]string, embeds map[string][]string, sigs map[string]string, err error) {
	structs = map[string]map[string]fieldInfo{}
	orders = map[string][]string{}
	embeds = map[string][]string{}
	sigs = map[string]string{}
	fset := token.NewFileSet()

	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return nil, nil, nil, nil, fmt.Errorf("parse %s: %w", e.Name(), perr)
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
					var order []string
					for _, fl := range st.Fields.List {
						if len(fl.Names) == 0 {
							// An embedded field still occupies a position in the
							// serialized output (under its tag, or its type name),
							// so it belongs in the order slice too -- otherwise a
							// moved embed would change the bytes unobserved.
							embeds[ts.Name.Name] = append(embeds[ts.Name.Name], render(fset, fl.Type))
							order = append(order, render(fset, fl.Type))
							continue
						}
						tag := ""
						if fl.Tag != nil {
							tag = reflect.StructTag(strings.Trim(fl.Tag.Value, "`")).Get("json")
						}
						for _, nm := range fl.Names {
							fields[nm.Name] = fieldInfo{typ: render(fset, fl.Type), tag: tag}
							order = append(order, nm.Name)
						}
					}
					structs[ts.Name.Name] = fields
					orders[ts.Name.Name] = order
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
	return structs, orders, embeds, sigs, nil
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
	orderDiff      []string // shared fields declared in a different position
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

// sharedOrderDiff reports whether the fields the two sides share are declared
// in a different relative order. encoding/json emits struct fields in
// declaration order, so same names, tags and types still serialize to different
// bytes when this differs -- which is what makes a golden snapshot comparison
// fail for a reason that is not a behaviour change.
//
// Fields present on only one side are skipped rather than counted: they are
// already the fieldSetDiff dimension, and including them would report every
// type that gained or lost a field as an order problem too.
func sharedOrderDiff(local, kit []string) (string, bool) {
	inKit := map[string]bool{}
	for _, f := range kit {
		inKit[f] = true
	}
	inLocal := map[string]bool{}
	for _, f := range local {
		inLocal[f] = true
	}
	var lo, ko []string
	for _, f := range local {
		if inKit[f] {
			lo = append(lo, f)
		}
	}
	for _, f := range kit {
		if inLocal[f] {
			ko = append(ko, f)
		}
	}
	if len(lo) < 2 || len(ko) < 2 {
		return "", false
	}
	for i := range lo {
		if lo[i] != ko[i] {
			return fmt.Sprintf("local=%v kit=%v", lo, ko), true
		}
	}
	return "", false
}

func build(localDir, kitDir string) (*report, error) {
	ls, lo, le, lf, err := scan(localDir)
	if err != nil {
		return nil, err
	}
	ks, ko, ke, kf, err := scan(kitDir)
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
		if o, ok := sharedOrderDiff(lo[name], ko[name]); ok {
			r.orderDiff = append(r.orderDiff, fmt.Sprintf("%s: %s", name, o))
		}
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
	sort.Strings(r.orderDiff)
	sort.Strings(r.sigDiff)
	sort.Strings(r.tagEquivalents)
	return r, nil
}

// group is one reported section: the title it prints under, the lines it
// prints, and the count -strict expects. Printing and gating both walk the
// same table, so a section cannot be printed without also being gated -- the
// 30th review round found the spelling group printed for many rounds while
// -strict silently ignored it, which is what two parallel lists eventually do.
type group struct {
	key   string // name in the counts line
	title string
	lines []string
	want  int
}

// reportGroups is the single table behind both the report and -strict. The
// recorded expectations are a point-in-time baseline (the 15th review round,
// 2026-10-04, against HEAD:dto and upstream/main:relaykit/dto): when the local
// dto changes (another session commits) or the pinned relaykit commit moves,
// the numbers change and -strict fails until the baseline is re-recorded
// deliberately. That is the point -- it is a gate against silent drift, not a
// permanent invariant.
func reportGroups(r *report) []group {
	const (
		wantFieldSet = 33 // types whose field-name sets differ
		wantTag      = 5
		wantType     = 6 // excludes interface{}/any spellings, reported separately
		wantEmbed    = 0
		wantOrder    = 4 // package-wide; only 2 of them (ClaudeMediaMessage,
		// FunctionRequest) are reachable from the 3 golden output roots -- see
		// the 31st review round in the design doc
		wantSig   = 12 // all IsStream; NewNotify's interface{}/any is a spelling
		wantEquiv = 21 // 20 fields + NewNotify's one parameter, in its own group
	)
	return []group{
		{"fieldSet", "字段名集合不同", r.fieldSetDiff, wantFieldSet},
		{"tag", "JSON tag 不同", r.tagDiff, wantTag},
		{"type", "Go 类型不同", r.typeDiff, wantType},
		{"embed", "嵌入字段不同", r.embedDiff, wantEmbed},
		{"order", "共有字段声明顺序不同", r.orderDiff, wantOrder},
		{"sig", "函数/方法签名不同", r.sigDiff, wantSig},
		{"equiv", "等价拼写（interface{} vs any），不计为差异", r.tagEquivalents, wantEquiv},
	}
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

	groups := reportGroups(r)
	for _, g := range groups {
		section(g.title, g.lines)
	}

	counts := make([]string, len(groups))
	for i, g := range groups {
		counts[i] = fmt.Sprintf("%s=%d", g.key, len(g.lines))
	}
	fmt.Printf("\n计数: %s\n", strings.Join(counts, " "))

	if *strict {
		for _, g := range groups {
			if len(g.lines) != g.want {
				fmt.Fprintf(os.Stderr, "strict: %s changed: got %d want %d\n", g.key, len(g.lines), g.want)
				os.Exit(1)
			}
		}
		fmt.Println("strict: counts match the recorded expectation")
	}
}
