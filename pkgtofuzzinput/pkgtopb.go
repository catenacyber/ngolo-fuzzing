package pkgtofuzzinput

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"go/ast"

	"golang.org/x/tools/go/packages"
)

type PkgFuncArgClass uint8

const (
	PkgFuncArgClassProto     PkgFuncArgClass = 0
	PkgFuncArgClassUnhandled PkgFuncArgClass = 1
	PkgFuncArgClassPkgGen    PkgFuncArgClass = 2
	PkgFuncArgClassProtoGen  PkgFuncArgClass = 3
	PkgFuncArgClassUnknown   PkgFuncArgClass = 4
)

type PkgFuncArg struct {
	Name      string
	FieldType string
	Proto     PkgFuncArgClass
	Prefix    string
}

type PkgFuncResult struct {
	FieldType string
	Used      bool
	Prefix    string
	Suffix    string
	//form : star, array...
}

type PkgFunction struct {
	Name    string
	Recv    string
	Args    []PkgFuncArg
	Returns []PkgFuncResult
}

type PkgType struct {
	Name string
	//maybe more fields will come if we need to build this type out of its exported fields...
}

type PkgDescription struct {
	Functions []PkgFunction
	Types     []PkgType
}

var ProtoGenerators = map[string]string{
	"io.RuneReader": "strings.NewReader",
	"io.ReaderAt":   "bytes.NewReader",
	"io.Reader":     "bytes.NewReader",
	"io.Writer":     "bytes.NewBuffer",
	"bufio.Reader":  "bytes.NewBuffer",
	"net.Conn":      "CreateFuzzingConn",
	"int":           "int",
	"rune":          "GetRune",
	"byte":          "byte",
	"uint8":         "uint8",
	"[]int":         "ConvertIntArray",
}

var ProtoGenerated = map[string]string{
	"io.RuneReader": "string",
	"io.ReaderAt":   "bytes",
	"io.Reader":     "bytes",
	"io.Writer":     "bytes",
	"bufio.Reader":  "bytes",
	"net.Conn":      "bytes",
	"int":           "int64",
	"rune":          "string",
	"byte":          "uint32",
	"uint8":         "uint32",
	"[]int":         "repeated int64",
}

func GolangArgumentClassName(e ast.Expr) (PkgFuncArgClass, string) {
	//TODO complete
	switch i := e.(type) {
	case *ast.Ident:
		switch i.Name {
		case "uint32", "int32", "string", "bool", "int64", "uint64":
			return PkgFuncArgClassProto, i.Name
		case "float32":
			return PkgFuncArgClassProto, "float"
		case "float64":
			return PkgFuncArgClassProto, "double"
		case "int", "rune", "byte", "uint8":
			return PkgFuncArgClassProtoGen, i.Name
		}
		//case *ast.StarExpr:
	case *ast.FuncType:
		return PkgFuncArgClassUnhandled, ""
	case *ast.Ellipsis:
		return PkgFuncArgClassUnhandled, ""
	case *ast.InterfaceType:
		return PkgFuncArgClassUnhandled, ""
	case *ast.ChanType:
		return PkgFuncArgClassUnhandled, ""
	case *ast.MapType:
		kc, kn := GolangArgumentClassName(i.Key)
		if kc == PkgFuncArgClassProto {
			vc, vn := GolangArgumentClassName(i.Value)
			if vc == PkgFuncArgClassProto {
				return PkgFuncArgClassProto, fmt.Sprintf("map<%s, %s>", kn, vn)
			}
		}
		return PkgFuncArgClassUnhandled, ""
	case *ast.ArrayType:
		switch i2 := i.Elt.(type) {
		case *ast.Ident:
			switch i2.Name {
			case "byte":
				return PkgFuncArgClassProto, "bytes"
			case "int":
				return PkgFuncArgClassProtoGen, "[]int"
			case "float64":
				return PkgFuncArgClassProto, "repeated float64"
			case "string":
				return PkgFuncArgClassProto, "repeated string"
			}
		}
	case *ast.SelectorExpr:
		switch i2 := i.X.(type) {
		case *ast.Ident:
			se := fmt.Sprintf("%s.%s", i2.Name, i.Sel.Name)
			switch se {
			case "io.RuneReader", "io.ReaderAt", "io.Reader", "io.Writer", "bufio.Reader", "net.Conn":
				return PkgFuncArgClassProtoGen, se
			}
		}
	}
	name, ok := astGetName(e)
	if ok {
		return PkgFuncArgClassPkgGen, name
	}
	return PkgFuncArgClassUnknown, ""
}

func PackageToProtobuf(pkg *packages.Package, descr PkgDescription, w io.StringWriter, outdir string) error {
	//There may exist a package to do this, but it looks simple enough to do it from scratch
	w.WriteString(`syntax = "proto3";` + "\n")
	w.WriteString(`package ngolofuzz;` + "\n")
	w.WriteString(`option go_package = "./;` + outdir + `";` + "\n\n")

	for _, m := range descr.Functions {
		w.WriteString(`message ` + m.Recv + m.Name + `Args {` + "\n")
		idx := 1
		for a := range m.Args {
			switch m.Args[a].Proto {
			case PkgFuncArgClassProto:
				w.WriteString(fmt.Sprintf("  %s %s = %d;\n", m.Args[a].FieldType, m.Args[a].Name, idx))
				idx = idx + 1
			case PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("  %s %s = %d;\n", ProtoGenerated[m.Args[a].FieldType], m.Args[a].Name, idx))
				idx = idx + 1
			}
		}
		w.WriteString("}\n")
	}
	w.WriteString("\n")

	w.WriteString(`message NgoloFuzzOne {` + "\n")
	w.WriteString(`  oneof item {` + "\n")
	for m := range descr.Functions {
		w.WriteString(fmt.Sprintf("    %s%sArgs %s%s = %d;\n", descr.Functions[m].Recv, descr.Functions[m].Name, descr.Functions[m].Recv, descr.Functions[m].Name, m+1))
	}
	w.WriteString("  }\n}\n")

	w.WriteString(`message NgoloFuzzList { repeated NgoloFuzzOne list = 1; }`)

	return nil
}

const fuzzTarget1 = `
//go:build gofuzz

package %s

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"time"

	"google.golang.org/protobuf/proto"

`

const fuzzTarget2 = `
)

type FuzzingConn struct {
	buf []byte
	offset int
}

func (c *FuzzingConn) Read(b []byte) (n int, err error) {
	if c.offset >= len(c.buf) {
		return 0, io.EOF
	}
	if len(b) < len(c.buf) + c.offset {
		copy(b, c.buf[c.offset:])
		c.offset += len(b)
		return len(b), nil
	}
	copy(b, c.buf[c.offset:])
	r := len(c.buf) - c.offset
	c.offset = len(c.buf)
	return r, nil
}

func (c *FuzzingConn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (c *FuzzingConn) Close() error {
	c.offset = len(c.buf)
	return nil
}

type FuzzingAddr struct {}

func (c *FuzzingAddr) Network() string {
	return "fuzz_addr_net"
}

func (c *FuzzingAddr) String() string {
	return "fuzz_addr_string"
}

func (c *FuzzingConn) LocalAddr() net.Addr {
	return &FuzzingAddr{}
}

func (c *FuzzingConn) RemoteAddr() net.Addr {
	return &FuzzingAddr{}
}

func (c *FuzzingConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *FuzzingConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *FuzzingConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func CreateFuzzingConn(a []byte) *FuzzingConn {
	r := &FuzzingConn{}
	r.buf = a
	return r
}

func ConvertIntArray(a []int64) []int {
	r := make([]int, len(a))
	for i := range a {
		r[i] = int(a[i])
	}
	return r
}

func GetRune(s string) rune {
	for _,c := range s {
		return c
	}
	return '\x00'
}

func FuzzNG_valid(data []byte) int {
	gen := &NgoloFuzzList{}
	err := proto.Unmarshal(data, gen)
	if err != nil {
		panic("Failed to unmarshal LPM generated variables")
	}
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case string:
			//do nothing
			default:
				fmt.Printf(string(debug.Stack()))
				panic(r)
			}
		}
	}()
	return FuzzNG_List(gen)
}

// we are unsure the input is a valid protobuf
func FuzzNG_unsure(data []byte) int {
	gen := &NgoloFuzzList{}
	err := proto.Unmarshal(data, gen)
	if err != nil {
		return 0
	}
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case string:
			//do nothing
			default:
				fmt.Printf(string(debug.Stack()))
				panic(r)
			}
		}
	}()
	return FuzzNG_List(gen)
}

var initialized bool

func FuzzNG_List(gen *NgoloFuzzList) int {
	if !initialized {
		repro := os.Getenv("FUZZ_NG_REPRODUCER")
		if len(repro) > 0 {
			f, err := os.Create(repro)
			if err != nil {
				log.Fatalf("Failed to open %s : %s", repro, err)
			} else {
				PrintNG_List(gen, f)
			}
		}
		initialized = true
	}
`

func PackageToFuzzTarget(pkg *packages.Package, descr PkgDescription, w io.StringWriter, outdir string) error {
	//maybe we should create AST and generate go from there
	w.WriteString(fmt.Sprintf(fuzzTarget1, outdir))
	// import other package needed from args such as strings
	toimport := make(map[string]bool)
	toimport[pkg.ID] = true
	toimport["net"] = true
	for _, m := range descr.Functions {
		for a := range m.Args {
			switch m.Args[a].Proto {
			case PkgFuncArgClassProtoGen:
				pkgGenNames := strings.Split(ProtoGenerators[m.Args[a].FieldType], ".")
				if len(pkgGenNames) == 2 {
					toimport[pkgGenNames[0]] = true
				}
			}
		}
	}
	for k, _ := range toimport {
		w.WriteString("\t\"" + k + "\"\n")
	}
	w.WriteString(fuzzTarget2)

	pkgSplit := strings.Split(pkg.ID, "/")
	pkgImportName := pkgSplit[len(pkgSplit)-1]

	for _, r := range descr.Types {
		w.WriteString(fmt.Sprintf("\tvar %sResults []*%s.%s\n", r.Name, pkgImportName, r.Name))
		w.WriteString(fmt.Sprintf("\t%sResultsIndex := 0\n", r.Name))
	}
	w.WriteString("\tfor l := range gen.List {\n")
	w.WriteString("\t\tswitch a := gen.List[l].Item.(type) {\n")

	for _, m := range descr.Functions {
		w.WriteString(fmt.Sprintf("\t\tcase *NgoloFuzzOne_%s%s:\n", m.Recv, m.Name))
		//prepare args
		for a := range m.Args {
			switch m.Args[a].Proto {
			case PkgFuncArgClassPkgGen:
				w.WriteString(fmt.Sprintf("\t\t\tif len(%sResults) == 0 {\n", m.Args[a].FieldType))
				w.WriteString("\t\t\t\tcontinue\n\t\t\t}\n")
				w.WriteString(fmt.Sprintf("\t\t\targ%d := %s%sResults[%sResultsIndex]\n", a, m.Args[a].Prefix, m.Args[a].FieldType, m.Args[a].FieldType))
				w.WriteString(fmt.Sprintf("\t\t\t%sResultsIndex = (%sResultsIndex + 1) %% len(%sResults)\n", m.Args[a].FieldType, m.Args[a].FieldType, m.Args[a].FieldType))
			case PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("\t\t\targ%d := ", a))
				w.WriteString(fmt.Sprintf("%s(a.%s%s.%s)\n", ProtoGenerators[m.Args[a].FieldType], m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			}
		}
		//call
		w.WriteString("\t\t\t")
		useReturn := false
		for a := range m.Returns {
			if m.Returns[a].Used {
				useReturn = true
				break
			}
		}
		if useReturn {
			comma := false
			for a := range m.Returns {
				if comma {
					w.WriteString(", ")
				} else {
					comma = true
				}
				if m.Returns[a].Used {
					w.WriteString(fmt.Sprintf("r%d", a))
				} else {
					w.WriteString("_")
				}
			}
			w.WriteString(" := ")
		}
		if len(m.Recv) > 0 {
			w.WriteString("arg0.")
		} else {
			w.WriteString(fmt.Sprintf("%s.", pkgImportName))
		}
		w.WriteString(fmt.Sprintf("%s(", m.Name))
		comma := false
		for a := range m.Args {
			if len(m.Recv) > 0 && a == 0 {
				continue
			}
			if comma {
				w.WriteString(", ")
			} else {
				comma = true
			}
			switch m.Args[a].Proto {
			case PkgFuncArgClassProto:
				w.WriteString(fmt.Sprintf("a.%s%s.%s", m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassPkgGen, PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("arg%d", a))
			}
		}
		w.WriteString(")\n")
		if useReturn {
			for a := range m.Returns {
				if m.Returns[a].Used {
					if m.Returns[a].Prefix == "" && m.Returns[a].Suffix == "" {
						w.WriteString(fmt.Sprintf("\t\t\tif r%d != nil{\n\t", a))
					}
					w.WriteString(fmt.Sprintf("\t\t\t%sResults = append(%sResults, %sr%d%s)\n", m.Returns[a].FieldType, m.Returns[a].FieldType, m.Returns[a].Prefix, a, m.Returns[a].Suffix))
					if m.Returns[a].Prefix == "" && m.Returns[a].Suffix == "" {
						w.WriteString(fmt.Sprintf("\t\t\t}\n"))
					}
				}
			}
		}
	}
	w.WriteString("\t\t}\n\t}\n\treturn 1\n}\n\n")

	w.WriteString("func PrintNG_List(gen *NgoloFuzzList, w io.StringWriter) {\n")
	for _, r := range descr.Types {
		w.WriteString(fmt.Sprintf("\t%sNb := 0\n", r.Name))
		w.WriteString(fmt.Sprintf("\t%sResultsIndex := 0\n", r.Name))
	}
	w.WriteString("\tfor l := range gen.List {\n")
	w.WriteString("\t\tswitch a := gen.List[l].Item.(type) {\n")
	for _, m := range descr.Functions {
		w.WriteString(fmt.Sprintf("\t\tcase *NgoloFuzzOne_%s%s:\n", m.Recv, m.Name))
		//prepare args
		for a := range m.Args {
			switch m.Args[a].Proto {
			case PkgFuncArgClassPkgGen:
				w.WriteString(fmt.Sprintf("\t\t\tif %sNb == 0 {\n", m.Args[a].FieldType))
				w.WriteString("\t\t\t\tcontinue\n\t\t\t}\n")
			}
		}
		//call
		useReturn := false
		for a := range m.Returns {
			if m.Returns[a].Used {
				useReturn = true
				break
			}
		}
		formatArgs := make([]string, 0, 16)
		w.WriteString("\t\t\tw.WriteString(fmt.Sprintf(\"")
		if useReturn {
			comma := false
			for a := range m.Returns {
				if comma {
					w.WriteString(", ")
				} else {
					comma = true
				}
				if m.Returns[a].Used {
					w.WriteString(fmt.Sprintf("%s%%d", m.Returns[a].FieldType))
					formatArgs = append(formatArgs, fmt.Sprintf("%sNb", m.Returns[a].FieldType))
				} else {
					w.WriteString("_")
				}
			}
			w.WriteString(" := ")
		}
		if len(m.Recv) > 0 {
			w.WriteString(fmt.Sprintf("%s%%d.", m.Args[0].FieldType))
			formatArgs = append(formatArgs, fmt.Sprintf("%sNb-1", m.Args[0].FieldType))
		} else {
			w.WriteString(fmt.Sprintf("%s.", pkgImportName))
		}
		w.WriteString(fmt.Sprintf("%s(", m.Name))

		comma := false
		for a := range m.Args {
			if len(m.Recv) > 0 && a == 0 {
				continue
			}
			if comma {
				w.WriteString(", ")
			} else {
				comma = true
			}
			switch m.Args[a].Proto {
			case PkgFuncArgClassProto:
				w.WriteString(fmt.Sprintf("%%#+v"))
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s.%s", m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("%s(%%#+v)", ProtoGenerators[m.Args[a].FieldType]))
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s.%s", m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassPkgGen:
				w.WriteString(fmt.Sprintf("%s%%d", m.Args[a].FieldType))
				formatArgs = append(formatArgs, fmt.Sprintf("%sNb", m.Args[a].FieldType))
			}
		}
		w.WriteString(`)\n"`)
		for _, f := range formatArgs {
			w.WriteString(", ")
			w.WriteString(f)
		}
		w.WriteString("))\n")

		//save
		if useReturn {
			for a := range m.Returns {
				if m.Returns[a].Used {
					w.WriteString(fmt.Sprintf("\t\t\t%sNb = %sNb + 1\n", m.Returns[a].FieldType, m.Returns[a].FieldType))
				}
			}
		}
		for a := range m.Args {
			if m.Args[a].Proto == PkgFuncArgClassPkgGen {
				w.WriteString(fmt.Sprintf("\t\t\t%sResultsIndex = (%sResultsIndex + 1) %% %sNb\n", m.Args[a].FieldType, m.Args[a].FieldType, m.Args[a].FieldType))
			}
		}
	}
	w.WriteString("\t\t}\n\t}\n}\n")

	return nil
}

func PackageToFuzzer(pkgname string, outdir string, exclude string) error {
	pkg, err := PackageFromName(pkgname)
	if err != nil {
		log.Printf("Failed loading package : %s", err)
		return err
	}
	if len(pkg.GoFiles) == 0 {
		log.Printf("No files in package")
		return fmt.Errorf("No files in package")
	}
	log.Printf("Found package in %s", filepath.Dir(pkg.GoFiles[0]))

	ngdir := outdir
	err = os.MkdirAll(ngdir, 0777)
	if err != nil {
		log.Printf("Failed creating dir %s : %s", ngdir, err)
		return err
	}

	descr, err := PackageToProtobufMessagesDescription(pkg, exclude)
	if err != nil {
		return err
	}

	ngProtoFilename := filepath.Join(ngdir, "ngolofuzz.proto")
	f, err := os.Create(ngProtoFilename)
	if err != nil {
		log.Printf("Failed creating file : %s", err)
		return err
	}
	err = PackageToProtobuf(pkg, descr, f, outdir)
	if err != nil {
		return err
	}
	f.Close()

	ngProtoFilename = filepath.Join(ngdir, "fuzz_ng.go")
	f, err = os.Create(ngProtoFilename)
	if err != nil {
		log.Printf("Failed creating file : %s", err)
		return err
	}
	err = PackageToFuzzTarget(pkg, descr, f, outdir)
	if err != nil {
		return err
	}
	f.Close()

	return nil
}

func PackageFromName(pkgname string) (*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedFiles | packages.NeedSyntax}
	pkgs, err := packages.Load(cfg, pkgname)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("Unexpectedly got %d packages", len(pkgs))
	}
	return pkgs[0], nil
}

const FNG_TYPE_RESULT = 1
const FNG_TYPE_ARG = 2

func astGetName(a ast.Expr) (string, bool) {
	switch e := a.(type) {
	case *ast.StarExpr:
		return astGetName(e.X)
	case *ast.Ident:
		return e.Name, true
	case *ast.ArrayType:
		return astGetName(e.Elt)
		//default:
		//panic(fmt.Sprintf("lol %#+v", e))
	case *ast.SelectorExpr:
		switch i2 := e.X.(type) {
		case *ast.Ident:
			se := fmt.Sprintf("%s.%s", i2.Name, e.Sel.Name)
			return se, true
		}
	case *ast.MapType:
		return "mapkv", true
	}
	return "", false
}

func funcToUse(name string, excludes []string) bool {
	//there may be a better test for exported functions
	if unicode.IsUpper(rune(name[0])) {
		for e := range excludes {
			if strings.Contains(name, excludes[e]) {
				return false
			}
		}
		return true
	}
	return false
}

func PackageToProtobufMessagesDescription(pkg *packages.Package, exclude string) (PkgDescription, error) {
	r := PkgDescription{}

	excludes := strings.Split(exclude, ",")
	if len(exclude) == 0 {
		excludes = excludes[:0]
	}
	typesMap := make(map[string]uint8)
	//first loop to find exported types
	for s := range pkg.Syntax {
		for d := range pkg.Syntax[s].Decls {
			switch f := pkg.Syntax[s].Decls[d].(type) {
			case *ast.GenDecl:
				for l := range f.Specs {
					switch t := f.Specs[l].(type) {
					case *ast.TypeSpec:
						if unicode.IsUpper(rune(t.Name.Name[0])) {
							typesMap[t.Name.Name] = 0
						}
					}
				}
			}
		}
	}

	//second loop to check if they are both read and used
	for s := range pkg.Syntax {
		for d := range pkg.Syntax[s].Decls {
			switch f := pkg.Syntax[s].Decls[d].(type) {
			case *ast.FuncDecl:
				if funcToUse(f.Name.Name, excludes) {
					if f.Type.Results != nil {
						for l := range f.Type.Results.List {
							name, ok := astGetName(f.Type.Results.List[l].Type)
							if ok && len(name) > 0 {
								v, ok := typesMap[name]
								if ok {
									typesMap[name] = v | FNG_TYPE_RESULT
								}
							}
						}
					}
					for l := range f.Type.Params.List {
						name, ok := astGetName(f.Type.Params.List[l].Type)
						if ok && len(name) > 0 {
							v, ok := typesMap[name]
							if ok {
								typesMap[name] = v | FNG_TYPE_ARG
							}
						}
					}
					if f.Recv != nil {
						for l := range f.Recv.List {
							name, ok := astGetName(f.Recv.List[l].Type)
							if ok && len(name) > 0 {
								v, ok := typesMap[name]
								if ok {
									typesMap[name] = v | FNG_TYPE_ARG
								}
							}
						}
					}
				}
			}
		}
	}

	r.Types = make([]PkgType, 0, len(typesMap))
	for k, v := range typesMap {
		if v == (FNG_TYPE_RESULT | FNG_TYPE_ARG) {
			pt := PkgType{}
			pt.Name = k
			r.Types = append(r.Types, pt)
		} else if v == FNG_TYPE_ARG {
			//TODO implement a dummy function to build this based on the exported fields ?
			// or field of an other produced field
			log.Printf("Type %s is used as argument but not produced\n", k)
			//return r, fmt.Errorf("Type %s is used as argument but not produced", k)
		}
	}

	// new loop for functions
	r.Functions = make([]PkgFunction, 0, 16)
	for s := range pkg.Syntax {
		for d := range pkg.Syntax[s].Decls {
			switch f := pkg.Syntax[s].Decls[d].(type) {
			case *ast.FuncDecl:
				if funcToUse(f.Name.Name, excludes) {
					pfpm := PkgFunction{}
					pfpm.Name = f.Name.Name
					if f.Recv != nil {
						if len(f.Recv.List) != 1 {
							return r, fmt.Errorf("Function %s has unhandled recv %#+v", f.Name.Name, f.Recv.List)
						}
						name, ok := astGetName(f.Recv.List[0].Type)
						if ok && len(name) > 0 {
							if !unicode.IsUpper(rune(name[0])) {
								continue
							}
							v, ok := typesMap[name]
							if !ok || v != (FNG_TYPE_RESULT|FNG_TYPE_ARG) {
								log.Printf("Function %s has unproduced recv %s", f.Name.Name, name)
								continue
							}
							pfpm.Recv = name + "ngdot"
							for n := range f.Recv.List[0].Names {
								papi := PkgFuncArg{}
								papi.Name = f.Recv.List[0].Names[n].Name
								papi.FieldType = name
								papi.Proto = PkgFuncArgClassPkgGen
								pfpm.Args = append(pfpm.Args, papi)
							}
						}
					}
					donotadd := false
					for l := range f.Type.Params.List {
						class, name := GolangArgumentClassName(f.Type.Params.List[l].Type)
						if class == PkgFuncArgClassUnknown {
							log.Printf("Unhandled argument %#+v for %s%s", f.Type.Params.List[l].Type, pfpm.Recv, f.Name.Name)
							return r, fmt.Errorf("Unknown argument %#+v for %s", f.Type.Params.List[l].Type, f.Name.Name)
						} else if class == PkgFuncArgClassUnhandled {
							log.Printf("Unhandled argument %#+v for %s%s", f.Type.Params.List[l].Type, pfpm.Recv, f.Name.Name)
							donotadd = true
							continue
						} else {
							prefix := ""
							if class == PkgFuncArgClassPkgGen {
								v, ok := typesMap[name]
								if !ok || v != (FNG_TYPE_RESULT|FNG_TYPE_ARG) {
									log.Printf("Function %s has unproduced argument %s", f.Name.Name, name)
									donotadd = true
									continue
								}
								if _, ok := f.Type.Params.List[l].Type.(*ast.Ident); ok {
									prefix = "*"
								}
							}
							for n := range f.Type.Params.List[l].Names {
								papi := PkgFuncArg{}
								papi.Name = f.Type.Params.List[l].Names[n].Name
								papi.FieldType = name
								papi.Proto = class
								papi.Prefix = prefix
								pfpm.Args = append(pfpm.Args, papi)
							}
						}
					}
					if donotadd {
						continue
					}
					if f.Type.Results != nil {
						for l := range f.Type.Results.List {
							name, ok := astGetName(f.Type.Results.List[l].Type)
							if !ok {
								return r, fmt.Errorf("Unhandled result %#+v for %s", f.Type.Results.List[l].Type, f.Name.Name)
							}
							v, ok := typesMap[name]
							pfr := PkgFuncResult{}
							switch f.Type.Results.List[l].Type.(type) {
							case *ast.Ident:
								pfr.Prefix = "&"
							case *ast.ArrayType:
								pfr.Suffix = "..."
							}
							pfr.FieldType = name
							if ok && v == (FNG_TYPE_RESULT|FNG_TYPE_ARG) {
								pfr.Used = true
							}
							pfpm.Returns = append(pfpm.Returns, pfr)
						}
					}
					r.Functions = append(r.Functions, pfpm)
				}
			}
		}
	}
	return r, nil
}
