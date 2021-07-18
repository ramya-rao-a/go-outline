package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"

	"golang.org/x/tools/go/buildutil"
)

type Declaration struct {
	Label        string        `json:"label"`
	Type         string        `json:"type"`
	ReceiverType string        `json:"receiverType,omitempty"`
	Start        token.Pos     `json:"start"`
	End          token.Pos     `json:"end"`
	Children     []Declaration `json:"children,omitempty"`
}

var (
	file        = flag.String("f", "", "the path to the file to outline")
	importsOnly = flag.Bool("imports-only", false, "parse imports only")
	modified    = flag.Bool("modified", false, "read an archive of the modified file from standard input")
)

func getReceiverType(fset *token.FileSet, decl *ast.FuncDecl) (string, error) {
	if decl.Recv == nil {
		return "", nil
	}

	buf := &bytes.Buffer{}
	if err := format.Node(buf, fset, decl.Recv.List[0].Type); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func reportError(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
}

func getAst() (*ast.File, *token.FileSet, error) {
	flag.Parse()
	fset := token.NewFileSet()
	parserMode := parser.ParseComments
	if *importsOnly {
		parserMode = parser.ImportsOnly
	}

	var fileAst *ast.File
	var err error

	if *modified {
		archive, err := buildutil.ParseOverlayArchive(os.Stdin)
		if err != nil {
			err = fmt.Errorf("failed to parse -modified archive: %v", err)
			return nil, nil, err
		}
		fc, ok := archive[*file]
		if !ok {
			err = fmt.Errorf("couldn't find %s in archive", *file)
			return nil, nil, err
		}
		fileAst, err = parser.ParseFile(fset, *file, fc, parserMode)
	} else {
		fileAst, err = parser.ParseFile(fset, *file, nil, parserMode)
	}

	if err != nil {
		err = fmt.Errorf("could not parse file %s", err.Error())
		return nil, nil, err
	}
	return fileAst, fset, nil
}

func getDeclarations(fileAst *ast.File, fset *token.FileSet) ([]*Declaration, error) {
	declarations := []Declaration{}

	for _, decl := range fileAst.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			receiverType, err := getReceiverType(fset, decl)
			if err != nil {
				err = fmt.Errorf("failed to parse receiver type: %v", err)
				return nil, err
			}
			declarations = append(declarations, Declaration{
				decl.Name.String(),
				"function",
				receiverType,
				decl.Pos(),
				decl.End(),
				[]Declaration{},
			})
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.ImportSpec:
					declarations = append(declarations, Declaration{
						spec.Path.Value,
						"import",
						"",
						spec.Pos(),
						spec.End(),
						[]Declaration{},
					})
				case *ast.TypeSpec:
					//TODO: Members if it's a struct or interface type?
					declarations = append(declarations, Declaration{
						spec.Name.String(),
						"type",
						"",
						spec.Pos(),
						spec.End(),
						[]Declaration{},
					})
				case *ast.ValueSpec:
					for _, id := range spec.Names {
						varOrConst := "variable"
						if decl.Tok == token.CONST {
							varOrConst = "constant"
						}
						declarations = append(declarations, Declaration{
							id.Name,
							varOrConst,
							"",
							id.Pos(),
							id.End(),
							[]Declaration{},
						})
					}
				default:
					err := fmt.Errorf("unknown token type: %s", decl.Tok)
					return nil, err
				}
			}
		default:
			err := fmt.Errorf("unknown declaration @ %v", decl.Pos())
			return nil, err
		}
	}

	pkg := []*Declaration{{
		fileAst.Name.String(),
		"package",
		"",
		fileAst.Pos(),
		fileAst.End(),
		declarations,
	}}
	return pkg, nil
}

func main() {
	fileAst, fset, err := getAst()
	if err != nil {
		reportError(err)
		return
	}

	pkg, err := getDeclarations(fileAst, fset)
	if err != nil {
		reportError(err)
		return
	}

	str, _ := json.Marshal(pkg)
	fmt.Println(string(str))
}
