package pkgtofuzzinput

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

type PkgFuncArgClass uint8

const (
	PkgFuncArgClassProto     PkgFuncArgClass = 0
	PkgFuncArgClassUnhandled PkgFuncArgClass = 1
	PkgFuncArgClassPkgGen    PkgFuncArgClass = 2
	PkgFuncArgClassProtoGen  PkgFuncArgClass = 3
	PkgFuncArgClassPkgConst  PkgFuncArgClass = 4
	PkgFuncArgClassUnknown   PkgFuncArgClass = 5
	PkgFuncArgClassPkgGenA   PkgFuncArgClass = 6
	PkgFuncArgClassPkgStruct PkgFuncArgClass = 7
)

type PkgFuncArg struct {
	Name      string
	FieldType string
	Proto     PkgFuncArgClass
	Prefix    string
	Suffix    string
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
	Suffix  string
	Args    []PkgFuncArg
	Returns []PkgFuncResult
	SrcDst  uint8
}

type PkgType struct {
	Name   string
	Values []string
	Args   []PkgFuncArg
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
	"bufio.Reader":  "CreateBufioReader",
	"big.Int":       "CreateBigInt",
	"net.Conn":      "CreateFuzzingConn",
	"int":           "int",
	"rune":          "GetRune",
	"byte":          "byte",
	"uint8":         "uint8",
	"uint16":        "uint16",
	"[]int":         "ConvertIntArray",
	"[]uint16":      "ConvertUint16Array",
}

var ProtoGenerated = map[string]string{
	"io.RuneReader": "string",
	"io.ReaderAt":   "bytes",
	"io.Reader":     "bytes",
	"io.Writer":     "bytes",
	"bufio.Reader":  "bytes",
	"big.Int":       "bytes",
	"net.Conn":      "bytes",
	"int":           "int64",
	"rune":          "string",
	"byte":          "uint32",
	"uint8":         "uint32",
	"uint16":        "uint32",
	"[]int":         "repeated int64",
	"[]uint16":      "repeated int64",
}

func GolangArgumentClassName(e ast.Expr) (PkgFuncArgClass, string) {
	// this is likely incomplete
	switch i := e.(type) {
	case *ast.Ident:
		switch i.Name {
		case "uint32", "int32", "string", "bool", "int64", "uint64":
			return PkgFuncArgClassProto, i.Name
		case "float32":
			return PkgFuncArgClassProto, "float"
		case "float64":
			return PkgFuncArgClassProto, "double"
		case "int", "rune", "byte", "uint8", "uint16":
			return PkgFuncArgClassProtoGen, i.Name
		case "any":
			return PkgFuncArgClassProto, "NgoloFuzzAny"
		}
	case *ast.FuncType:
		return PkgFuncArgClassUnhandled, ""
	case *ast.Ellipsis:
		return PkgFuncArgClassUnhandled, ""
	case *ast.InterfaceType:
		if len(i.Methods.List) == 0 { // any
			return PkgFuncArgClassProto, "NgoloFuzzAny"
		}
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
		case *ast.ArrayType:
			switch i3 := i2.Elt.(type) {
			case *ast.Ident:
				switch i3.Name {
				case "byte":
					if i.Len == nil && i2.Len == nil {
						return PkgFuncArgClassProto, "repeated bytes"
					}
				}
			}
		case *ast.Ident:
			switch i2.Name {
			case "byte", "uint8":
				if i.Len == nil {
					// no fixed size arrays in protobuf...
					return PkgFuncArgClassProto, "bytes"
				}
			case "uint16":
				return PkgFuncArgClassProtoGen, "[]uint16"
			case "int":
				return PkgFuncArgClassProtoGen, "[]int"
			case "float64":
				return PkgFuncArgClassProto, "repeated float64"
			case "string":
				return PkgFuncArgClassProto, "repeated string"
			default:
				return PkgFuncArgClassPkgGenA, i2.Name
			}
		}
	case *ast.StarExpr:
		switch i2 := i.X.(type) {
		case *ast.SelectorExpr:
			switch i3 := i2.X.(type) {
			case *ast.Ident:
				se := fmt.Sprintf("%s.%s", i3.Name, i2.Sel.Name)
				switch se {
				case "big.Int", "bufio.Reader":
					return PkgFuncArgClassProtoGen, se
				}
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

	for _, r := range descr.Types {
		if len(r.Values) > 0 {
			w.WriteString(`enum ` + r.Name + `Enum {` + "\n")
			for v := range r.Values {
				w.WriteString(fmt.Sprintf("  %s = %d;\n", r.Values[v], v))
			}
			w.WriteString("}\n\n")
		} else if len(r.Args) > 0 {
			idx := 1
			w.WriteString(`message ` + r.Name + `Struct {` + "\n")
			for a := range r.Args {
				switch r.Args[a].Proto {
				case PkgFuncArgClassPkgConst:
					w.WriteString(fmt.Sprintf("  %sEnum %s = %d;\n", r.Args[a].FieldType, r.Args[a].Name, idx))
					idx = idx + 1
				case PkgFuncArgClassProto:
					w.WriteString(fmt.Sprintf("  %s %s = %d;\n", r.Args[a].FieldType, r.Args[a].Name, idx))
					idx = idx + 1
				case PkgFuncArgClassProtoGen:
					w.WriteString(fmt.Sprintf("  %s %s = %d;\n", ProtoGenerated[r.Args[a].FieldType], r.Args[a].Name, idx))
					idx = idx + 1
				}
			}
			w.WriteString("}\n\n")
		}
	}

	for _, m := range descr.Functions {
		w.WriteString(`message ` + m.Recv + m.Name + `Args {` + "\n")
		idx := 1
		for a := range m.Args {
			switch m.Args[a].Proto {
			case PkgFuncArgClassPkgConst:
				w.WriteString(fmt.Sprintf("  %sEnum %s = %d;\n", m.Args[a].FieldType, m.Args[a].Name, idx))
				idx = idx + 1
			case PkgFuncArgClassProto:
				w.WriteString(fmt.Sprintf("  %s %s = %d;\n", m.Args[a].FieldType, m.Args[a].Name, idx))
				idx = idx + 1
			case PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("  %s %s = %d;\n", ProtoGenerated[m.Args[a].FieldType], m.Args[a].Name, idx))
				idx = idx + 1
			case PkgFuncArgClassPkgStruct:
				w.WriteString(fmt.Sprintf("  %sStruct %s = %d;\n", m.Args[a].FieldType, m.Args[a].Name, idx))
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

	//TODO only add it if necessary
	w.WriteString(`message NgoloFuzzAny {` + "\n")
	w.WriteString(`  oneof item {` + "\n")
	w.WriteString("    double DoubleArgs = 1;\n")
	w.WriteString("    int64 Int64Args = 2;\n")
	w.WriteString("    bool BoolArgs = 3;\n")
	w.WriteString("    string StringArgs = 4;\n")
	w.WriteString("    bytes BytesArgs = 5;\n")
	w.WriteString("  }\n}\n")

	w.WriteString(`message NgoloFuzzList { repeated NgoloFuzzOne list = 1; }`)

	return nil
}

const fuzzTarget1 = `//go:build gofuzz

package %s

import (
	"google.golang.org/protobuf/proto"

`

const fuzzTarget2 = `)

type FuzzingConn struct {
	buf    []byte
	offset int
}

func (c *FuzzingConn) Read(b []byte) (n int, err error) {
	if c.offset >= len(c.buf) {
		return 0, io.EOF
	}
	if len(b) < len(c.buf)+c.offset {
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

type FuzzingAddr struct{}

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

//TODO only add these functions if needed
func CreateBigInt(a []byte) *big.Int {
	r := new(big.Int)
	r.SetBytes(a)
	return r
}

func CreateBufioReader(a []byte) *bufio.Reader {
	return bufio.NewReader(bytes.NewBuffer(a))
}

func ConvertIntArray(a []int64) []int {
	r := make([]int, len(a))
	for i := range a {
		r[i] = int(a[i])
	}
	return r
}

func ConvertUint16Array(a []int64) []uint16 {
	r := make([]uint16, len(a))
	for i := range a {
		r[i] = uint16(a[i])
	}
	return r
}

func GetRune(s string) rune {
	for _, c := range s {
		return c
	}
	return '\x00'
}
`

const fuzzTarget3 = `func FuzzNG_valid(data []byte) int {
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
				panic(r)
			}
		}
	}()
	runtime.GC()
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
				panic(r)
			}
		}
	}()
	runtime.GC()
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

// fix camel case for rare functions not having it like rsa.DecryptPKCS1v15

func CamelUpper(s string) string {
	return s[0:1] + strings.ToUpper(s[1:2]) + s[2:3]
}

var badCamel = regexp.MustCompile(`([0-9])([a-z])([0-9])`)

func CamelCase(s string) string {
	return badCamel.ReplaceAllStringFunc(s, CamelUpper)
}

func TitleCase(s string) string {
	if len(s) > 0 && s[0] == '_' {
		return "X" + strings.Title(s[1:])
	}
	return strings.Title(s)
}

func PackageToFuzzTarget(pkg *packages.Package, descr PkgDescription, w io.StringWriter, outdir string, limits string) error {

	// maybe args parsing should be done earlier...
	limitsList := strings.Split(limits, ",")
	if len(limits) == 0 {
		limitsList = limitsList[:0]
	}
	limitsMap := make(map[string]bool, len(limitsList))
	for k := range limitsList {
		limitsMap[limitsList[k]] = true
	}

	//maybe we should create AST and generate go from there
	w.WriteString(fmt.Sprintf(fuzzTarget1, outdir))
	// import other package needed from args such as strings
	toimport := make(map[string]bool)
	toimport[pkg.ID] = true
	toimport["fmt"] = true
	toimport["bufio"] = true
	toimport["bytes"] = true
	toimport["io"] = true
	toimport["log"] = true
	toimport["net"] = true
	toimport["os"] = true
	toimport["time"] = true
	toimport["runtime"] = true
	toimport["math/big"] = true
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
	keys := make([]string, 0, len(toimport))
	for k := range toimport {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		w.WriteString("\t\"" + k + "\"\n")
	}
	w.WriteString(fuzzTarget2)

	pkgSplit := strings.Split(pkg.ID, "/")
	pkgImportName := pkgSplit[len(pkgSplit)-1]

	// write functions returning type with constants
	for _, r := range descr.Types {
		if len(r.Values) > 0 {
			w.WriteString("\nfunc " + r.Name + "NewFromFuzz(p " + r.Name + "Enum) " + pkgImportName + "." + r.Name + "{\n")
			if len(r.Values) > 1 {
				w.WriteString("\tswitch p {\n")
				for i := 0; i < len(r.Values)-1; i++ {
					w.WriteString(fmt.Sprintf("\t\tcase %d:\n", i+1))
					w.WriteString("\t\t\treturn " + pkgImportName + "." + r.Values[i+1] + "\n")
				}
				w.WriteString("\t}\n")
			}
			w.WriteString("\treturn " + pkgImportName + "." + r.Values[0] + "\n")
			w.WriteString("}\n\n")
			w.WriteString("\nfunc Convert" + r.Name + "NewFromFuzz(a []" + r.Name + "Enum) []" + pkgImportName + "." + r.Name + "{\n")
			w.WriteString("\tr := make([]" + pkgImportName + "." + r.Name + ", len(a))\n")
			w.WriteString("\tfor i := range a {\n")
			w.WriteString("\t\tr[i] = " + r.Name + "NewFromFuzz(a[i])\n")
			w.WriteString("\t}\n")
			w.WriteString("\treturn r\n")
			w.WriteString("}\n\n")
		} else if len(r.Args) > 0 {
			w.WriteString("\nfunc " + r.Name + "NewFromFuzz(p *" + r.Name + "Struct) *" + pkgImportName + "." + r.Name + "{\n")
			w.WriteString("\treturn &" + pkgImportName + "." + r.Name + "{\n")
			for i := range r.Args {
				w.WriteString("\t\t" + r.Args[i].Name + ": ")
				switch r.Args[i].Proto {
				case PkgFuncArgClassPkgConst:
					if strings.HasPrefix(r.Args[i].FieldType, "repeated ") {
						w.WriteString(fmt.Sprintf("Convert%s(p.%s)", r.Args[i].FieldType[len("repeated "):]+"NewFromFuzz", r.Args[i].Name))
					} else {
						w.WriteString(fmt.Sprintf("%s(p.%s)", r.Args[i].FieldType+"NewFromFuzz", r.Args[i].Name))
					}
				case PkgFuncArgClassProto:
					w.WriteString(fmt.Sprintf("p.%s%s", r.Args[i].Name, r.Args[i].Suffix))
				case PkgFuncArgClassProtoGen:
					w.WriteString(fmt.Sprintf("%s(p.%s)", ProtoGenerators[r.Args[i].FieldType], r.Args[i].Name))
				}
				w.WriteString(",\n")
			}
			w.WriteString("\t}\n")
			w.WriteString("}\n\n")
		}
	}
	w.WriteString(fuzzTarget3)

	for _, r := range descr.Types {
		if len(r.Values) == 0 && len(r.Args) == 0 {
			w.WriteString(fmt.Sprintf("\tvar %sResults []*%s.%s\n", r.Name, pkgImportName, r.Name))
			w.WriteString(fmt.Sprintf("\t%sResultsIndex := 0\n", r.Name))
		}
	}
	w.WriteString("\tfor l := range gen.List {\n")
	w.WriteString("\t\tswitch a := gen.List[l].Item.(type) {\n")

	for _, m := range descr.Functions {
		w.WriteString(fmt.Sprintf("\t\tcase *NgoloFuzzOne_%s%s%s:\n", m.Recv, CamelCase(m.Name), m.Suffix))
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
				w.WriteString(fmt.Sprintf("%s(a.%s%s%s.%s)\n", ProtoGenerators[m.Args[a].FieldType], m.Recv, CamelCase(m.Name), m.Suffix, TitleCase(m.Args[a].Name)))
			case PkgFuncArgClassPkgConst, PkgFuncArgClassPkgStruct:
				w.WriteString(fmt.Sprintf("\t\t\targ%d := ", a))
				w.WriteString(fmt.Sprintf("%s(a.%s%s.%s)\n", m.Args[a].FieldType+"NewFromFuzz", m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassProto:
				// The parameter 2 could be improved
				if m.Args[a].Name == "dst" && m.Args[a].FieldType == "bytes" && m.SrcDst == FNG_DSTSRC_DST|FNG_DSTSRC_SRC {
					w.WriteString(fmt.Sprintf("\t\t\ta.%s%s%s.Dst = make([]byte, 2*len(a.%s%s%s.Src))\n", m.Recv, m.Name, m.Suffix, m.Recv, m.Name, m.Suffix))
				}
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
				w.WriteString(fmt.Sprintf("a.%s%s%s.%s", m.Recv, CamelCase(m.Name), m.Suffix, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassPkgGen, PkgFuncArgClassProtoGen, PkgFuncArgClassPkgConst, PkgFuncArgClassPkgStruct:
				w.WriteString(fmt.Sprintf("arg%d", a))
			}
			// check if this parameter must be limited like rand.Prime.bits
			_, ok := limitsMap[fmt.Sprintf("%s%s.%s", m.Recv, m.Name, m.Args[a].Name)]
			if ok {
				// constant is good enough for now
				w.WriteString(" % 0x10001")
			}
		}
		w.WriteString(")\n")
		if useReturn {
			for a := range m.Returns {
				if m.Returns[a].Used {
					if m.Returns[a].Prefix == "" && m.Returns[a].Suffix == "" {
						w.WriteString(fmt.Sprintf("\t\t\tif r%d != nil{\n\t", a))
					}
					if m.Returns[a].FieldType == "error" {
						w.WriteString("\t\t\treturn 0\n")
					} else {
						w.WriteString(fmt.Sprintf("\t\t\t%sResults = append(%sResults, %sr%d%s)\n", m.Returns[a].FieldType, m.Returns[a].FieldType, m.Returns[a].Prefix, a, m.Returns[a].Suffix))
					}
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
		if len(r.Values) == 0 && len(r.Args) == 0 {
			w.WriteString(fmt.Sprintf("\t%sNb := 0\n", r.Name))
			w.WriteString(fmt.Sprintf("\t%sResultsIndex := 0\n", r.Name))
		}
	}
	w.WriteString("\tfor l := range gen.List {\n")
	w.WriteString("\t\tswitch a := gen.List[l].Item.(type) {\n")
	for _, m := range descr.Functions {
		w.WriteString(fmt.Sprintf("\t\tcase *NgoloFuzzOne_%s%s%s:\n", m.Recv, CamelCase(m.Name), m.Suffix))
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
			if m.Returns[a].Used && m.Returns[a].FieldType != "error" {
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
				if m.Returns[a].Used && m.Returns[a].FieldType != "error" {
					w.WriteString(fmt.Sprintf("%s%%d", m.Returns[a].FieldType))
					formatArgs = append(formatArgs, fmt.Sprintf("%sNb", m.Returns[a].FieldType))
				} else {
					w.WriteString("_")
				}
			}
			w.WriteString(" := ")
		}
		if len(m.Recv) > 0 {
			if m.Args[0].Proto == PkgFuncArgClassPkgConst || m.Args[0].Proto == PkgFuncArgClassPkgStruct {
				w.WriteString(fmt.Sprintf("%s(%%#+v).", m.Args[0].FieldType+"NewFromFuzz"))
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s.%s", m.Recv, m.Name, strings.Title(m.Args[0].Name)))
			} else {
				w.WriteString(fmt.Sprintf("%s%%d.", m.Args[0].FieldType))
				formatArgs = append(formatArgs, fmt.Sprintf("%sNb-1", m.Args[0].FieldType))
			}
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
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s%s.%s", m.Recv, CamelCase(m.Name), m.Suffix, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassProtoGen:
				w.WriteString(fmt.Sprintf("%s(%%#+v)", ProtoGenerators[m.Args[a].FieldType]))
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s%s.%s", m.Recv, CamelCase(m.Name), m.Suffix, TitleCase(m.Args[a].Name)))
			case PkgFuncArgClassPkgConst, PkgFuncArgClassPkgStruct:
				w.WriteString(fmt.Sprintf("%s(%%#+v)", m.Args[a].FieldType+"NewFromFuzz"))
				formatArgs = append(formatArgs, fmt.Sprintf("a.%s%s.%s", m.Recv, m.Name, strings.Title(m.Args[a].Name)))
			case PkgFuncArgClassPkgGen:
				w.WriteString(fmt.Sprintf("%s%%d", m.Args[a].FieldType))
				formatArgs = append(formatArgs, fmt.Sprintf("%sNb", m.Args[a].FieldType))
			}
			_, ok := limitsMap[fmt.Sprintf("%s%s.%s", m.Recv, m.Name, m.Args[a].Name)]
			if ok {
				w.WriteString(" %% 0x10001")
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
				if m.Returns[a].Used && m.Returns[a].FieldType != "error" {
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

func PackageToFuzzer(pkgname string, outdir string, exclude string, limits string) error {
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
	err = PackageToFuzzTarget(pkg, descr, f, outdir, limits)
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

// Flags
const FNG_TYPE_RESULT uint8 = 1
const FNG_TYPE_ARG uint8 = 2

// has some defined constants
const FNG_TYPE_CONST uint8 = 4

// struct with exported fields (which can be built without a function)
const FNG_TYPE_STRUCTEXP uint8 = 8

const FNG_DSTSRC_DST = 1
const FNG_DSTSRC_SRC = 2

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
	case *ast.InterfaceType:
		return "intf", true
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

func pkgTypeConsts(pkg *packages.Package, k string) (bool, []string) {
	found := false
	var values []string
	for s := range pkg.Syntax {
		for d := range pkg.Syntax[s].Decls {
			switch f := pkg.Syntax[s].Decls[d].(type) {
			case *ast.GenDecl:
				if f.Tok == token.CONST {
					for v := range f.Specs {
						val, ok := f.Specs[v].(*ast.ValueSpec)
						if ok {
							if found {
								// take all the list of constants iota-like
								if unicode.IsUpper(rune(val.Names[0].Name[0])) {
									values = append(values, val.Names[0].Name)
								}
							} else {
								constyp, ok := val.Type.(*ast.Ident)
								if ok {
									if constyp.Name == k {
										if unicode.IsUpper(rune(val.Names[0].Name[0])) {
											values = append(values, val.Names[0].Name)
										}
										found = true
									}
								}
							}
						}
					}
					if found {
						return found, values
					}
				}
			}
		}
	}
	return found, values
}

func exportedStructArgs(pkg *packages.Package, sname string, typesMap map[string]uint8) []PkgFuncArg {
	var r []PkgFuncArg
	for s := range pkg.Syntax {
		for d := range pkg.Syntax[s].Decls {
			switch f := pkg.Syntax[s].Decls[d].(type) {
			case *ast.GenDecl:
				for l := range f.Specs {
					switch t := f.Specs[l].(type) {
					case *ast.TypeSpec:
						if t.Name.Name == sname {
							switch u := t.Type.(type) {
							case *ast.StructType:
								for f := range u.Fields.List {
									for n := range u.Fields.List[f].Names {
										if unicode.IsUpper(rune(u.Fields.List[f].Names[n].Name[0])) {
											class, name := GolangArgumentClassName(u.Fields.List[f].Type)
											if class == PkgFuncArgClassUnknown || class == PkgFuncArgClassUnhandled {
												log.Printf("Unhandled field %#+v for struct %s", u.Fields.List[f].Type, sname)
												continue
											}
											sa := PkgFuncArg{}
											sa.Name = u.Fields.List[f].Names[n].Name
											sa.FieldType = name
											if class == PkgFuncArgClassPkgGen || class == PkgFuncArgClassPkgGenA {
												v, ok := typesMap[name]
												if ok && (v&FNG_TYPE_CONST) != 0 {
													// we will produce one of the constants exported based on an int32/enum-like
													if class == PkgFuncArgClassPkgGenA {
														sa.FieldType = "repeated " + sa.FieldType
													}
													class = PkgFuncArgClassPkgConst
												}
											}
											sa.Proto = class
											if sa.Name == "String" {
												sa.Suffix = "_"
											}
											if class != PkgFuncArgClassPkgGen && class != PkgFuncArgClassPkgGenA {
												r = append(r, sa)
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return r
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
							initVal := uint8(0)
							switch u := t.Type.(type) {
							case *ast.StructType:
								nbu := 0
								nbl := 0
								for f := range u.Fields.List {
									for n := range u.Fields.List[f].Names {
										if unicode.IsUpper(rune(u.Fields.List[f].Names[n].Name[0])) {
											nbu++
										} else {
											nbl++
										}
									}
								}
								if nbu > nbl {
									initVal = FNG_TYPE_STRUCTEXP
								}
							}
							typesMap[t.Name.Name] = initVal
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
					if f.Recv != nil {
						if len(f.Recv.List) == 1 {
							name, ok := astGetName(f.Recv.List[0].Type)
							if ok && len(name) > 0 {
								if !unicode.IsUpper(rune(name[0])) {
									continue
								}
							}
						}
					}
					if f.Type.Results != nil {
						for l := range f.Type.Results.List {
							name, ok := astGetName(f.Type.Results.List[l].Type)
							if ok && len(name) > 0 {
								_, isarray := f.Type.Results.List[l].Type.(*ast.ArrayType)
								if !isarray {
									v, ok := typesMap[name]
									if ok {
										typesMap[name] = v | FNG_TYPE_RESULT
									}
								} else {
									log.Printf("Array result for %s is not handled\n", name)
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
	var structToDo []string
	for k, v := range typesMap {
		if (v & (FNG_TYPE_RESULT | FNG_TYPE_ARG)) == (FNG_TYPE_RESULT | FNG_TYPE_ARG) {
			pt := PkgType{}
			pt.Name = k
			r.Types = append(r.Types, pt)
		} else if (v & FNG_TYPE_ARG) != 0 {
			hasconst, values := pkgTypeConsts(pkg, k)
			if hasconst {
				// There is no producer, but we have some exported const values that we can use
				typesMap[k] = v | FNG_TYPE_CONST
				pt := PkgType{}
				pt.Name = k
				pt.Values = values
				r.Types = append(r.Types, pt)
			} else if (v & FNG_TYPE_STRUCTEXP) != 0 {
				structToDo = append(structToDo, k)
			} else {
				//TODO type is exported field of an other produced return ?
				log.Printf("Type %s is used as argument but not produced\n", k)
				//return r, fmt.Errorf("Type %s is used as argument but not produced", k)
			}
		}
	}
	for _, k := range structToDo {
		pt := PkgType{}
		pt.Name = k
		pt.Args = exportedStructArgs(pkg, k, typesMap)
		if len(pt.Args) == 0 {
			typesMap[k] = typesMap[k] & (uint8(^FNG_TYPE_STRUCTEXP))
		} else {
			r.Types = append(r.Types, pt)
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
					switch pfpm.Name {
					case "Marshal", "Unmarshal":
						pfpm.Suffix = "_"
					}
					if f.Recv != nil {
						if len(f.Recv.List) != 1 {
							return r, fmt.Errorf("Function %s has unhandled recv %#+v", f.Name.Name, f.Recv.List)
						}
						name, ok := astGetName(f.Recv.List[0].Type)
						if ok && len(name) > 0 {
							if !unicode.IsUpper(rune(name[0])) {
								continue
							}
							class := PkgFuncArgClassPkgGen
							v, ok := typesMap[name]
							if ok && v == (FNG_TYPE_CONST|FNG_TYPE_ARG) {
								// we will produce one of the constants exported based on an int32/enum-like
								class = PkgFuncArgClassPkgConst
							} else if (v&FNG_TYPE_STRUCTEXP) != 0 && (v&FNG_TYPE_RESULT) == 0 {
								class = PkgFuncArgClassPkgStruct
							} else if !ok || (v&FNG_TYPE_RESULT) == 0 {
								log.Printf("Function %s has unproduced recv %s", f.Name.Name, name)
								continue
							}
							pfpm.Recv = name + "Ngdot"
							for n := range f.Recv.List[0].Names {
								papi := PkgFuncArg{}
								papi.Name = f.Recv.List[0].Names[n].Name
								papi.FieldType = name
								papi.Proto = class
								pfpm.Args = append(pfpm.Args, papi)
							}
						} else {
							log.Printf("Function %s has unhandled recv %#+v", f.Name.Name, f.Recv.List[0])
							continue
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
								if ok && v == (FNG_TYPE_CONST|FNG_TYPE_ARG) {
									// we will produce one of the constants exported based on an int32/enum-like
									class = PkgFuncArgClassPkgConst
								} else if !ok || (v&FNG_TYPE_RESULT) == 0 {
									log.Printf("Function %s has unproduced argument %s", f.Name.Name, name)
									donotadd = true
									continue
								}
								if _, ok := f.Type.Params.List[l].Type.(*ast.Ident); ok {
									prefix = "*"
								}
							} else if class == PkgFuncArgClassPkgGenA {
								log.Printf("Function %s has unproduced array argument %s", f.Name.Name, name)
								donotadd = true
								continue
							}
							for n := range f.Type.Params.List[l].Names {
								papi := PkgFuncArg{}
								papi.Name = f.Type.Params.List[l].Names[n].Name
								papi.FieldType = name
								papi.Proto = class
								papi.Prefix = prefix
								if papi.FieldType == "bytes" {
									// special handling for functions such as hex.Encode(dst, src []byte)
									// where dst is write only (no read) and size is assumed to be big enough
									if papi.Name == "dst" {
										pfpm.SrcDst = pfpm.SrcDst | FNG_DSTSRC_DST
									} else if papi.Name == "src" {
										pfpm.SrcDst = pfpm.SrcDst | FNG_DSTSRC_SRC
									}
								}
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
								if name != "error" {
									pfr.Prefix = "&"
								}
							case *ast.ArrayType:
								pfr.Suffix = "..."
							}
							pfr.FieldType = name
							if ok && v == (FNG_TYPE_RESULT|FNG_TYPE_ARG) || pfr.FieldType == "error" {
								pfr.Used = true
							}
							if len(f.Type.Results.List[l].Names) > 1 {
								for _ = range f.Type.Results.List[l].Names {
									pfpm.Returns = append(pfpm.Returns, pfr)
								}
							} else {
								pfpm.Returns = append(pfpm.Returns, pfr)
							}
						}
					}
					r.Functions = append(r.Functions, pfpm)
				}
			}
		}
	}
	return r, nil
}
