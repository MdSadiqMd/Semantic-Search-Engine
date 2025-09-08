package ast

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/google/uuid"
)

type GoParser struct {
	fileSet *token.FileSet
}

func NewGoParser() *GoParser {
	return &GoParser{
		fileSet: token.NewFileSet(),
	}
}

func (p *GoParser) GetSupportedExtensions() []string {
	return []string{".go"}
}

func (p *GoParser) GetLanguage() string {
	return "go"
}

func (p *GoParser) ParseFile(ctx context.Context, filePath string) ([]models.CodeElement, []models.Relationship, error) {
	content, err := ReadFileContent(filePath)
	if err != nil {
		return nil, nil, err
	}

	file, err := parser.ParseFile(p.fileSet, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse Go file: %w", err)
	}

	var elements []models.CodeElement
	var relationships []models.Relationship

	// Extract package information
	packageName := file.Name.Name

	// Walk through all declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			element, rels := p.parseFuncDecl(d, filePath, packageName, content)
			if element != nil {
				elements = append(elements, *element)
				relationships = append(relationships, rels...)
			}
		case *ast.GenDecl:
			els, rels := p.parseGenDecl(d, filePath, packageName, content)
			elements = append(elements, els...)
			relationships = append(relationships, rels...)
		}
	}

	return elements, relationships, nil
}

func (p *GoParser) parseFuncDecl(funcDecl *ast.FuncDecl, filePath, packageName, content string) (*models.CodeElement, []models.Relationship) {
	if funcDecl == nil || funcDecl.Name == nil {
		return nil, nil
	}

	pos := p.fileSet.Position(funcDecl.Pos())
	end := p.fileSet.Position(funcDecl.End())

	elementType := models.Function
	if funcDecl.Recv != nil {
		elementType = models.Method
	}

	// Extract function signature
	signature := p.extractFuncSignature(funcDecl)

	// Extract doc comments
	var docComment string
	if funcDecl.Doc != nil {
		docComment = ExtractDocComment(p.extractComments(funcDecl.Doc))
	}

	// Extract function body
	code := GetLineRange(content, pos.Line, end.Line)

	element := &models.CodeElement{
		ID:         uuid.New().String(),
		Name:       funcDecl.Name.Name,
		Type:       elementType,
		FilePath:   filePath,
		StartLine:  pos.Line,
		EndLine:    end.Line,
		Package:    packageName,
		Signature:  signature,
		DocComment: docComment,
		Code:       code,
		Metadata: map[string]interface{}{
			"is_exported":  funcDecl.Name.IsExported(),
			"has_receiver": funcDecl.Recv != nil,
			"param_count":  len(funcDecl.Type.Params.List),
			"return_count": p.getReturnCount(funcDecl.Type),
		},
	}

	// Extract relationships
	var relationships []models.Relationship

	// Find function calls within this function
	ast.Inspect(funcDecl, func(n ast.Node) bool {
		if callExpr, ok := n.(*ast.CallExpr); ok {
			if ident, ok := callExpr.Fun.(*ast.Ident); ok {
				// Direct function call
				relationships = append(relationships, models.Relationship{
					ID:     uuid.New().String(),
					FromID: element.ID,
					ToID:   "",
					Type:   models.Calls,
					Properties: map[string]interface{}{
						"function_name": ident.Name,
						"call_type":     "direct",
					},
				})
			} else if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
				// Method call or package function call
				relationships = append(relationships, models.Relationship{
					ID:     uuid.New().String(),
					FromID: element.ID,
					ToID:   "",
					Type:   models.Calls,
					Properties: map[string]interface{}{
						"function_name": selectorExpr.Sel.Name,
						"call_type":     "method",
					},
				})
			}
		}
		return true
	})

	return element, relationships
}

func (p *GoParser) parseGenDecl(genDecl *ast.GenDecl, filePath, packageName, content string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	for _, spec := range genDecl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			element, rels := p.parseTypeSpec(s, genDecl, filePath, packageName, content)
			if element != nil {
				elements = append(elements, *element)
				relationships = append(relationships, rels...)
			}
		case *ast.ValueSpec:
			els, rels := p.parseValueSpec(s, genDecl, filePath, packageName, content)
			elements = append(elements, els...)
			relationships = append(relationships, rels...)
		case *ast.ImportSpec:
			element := p.parseImportSpec(s, genDecl, filePath, packageName, content)
			if element != nil {
				elements = append(elements, *element)
			}
		}
	}

	return elements, relationships
}

func (p *GoParser) parseTypeSpec(typeSpec *ast.TypeSpec, genDecl *ast.GenDecl, filePath, packageName, content string) (*models.CodeElement, []models.Relationship) {
	if typeSpec == nil || typeSpec.Name == nil {
		return nil, nil
	}

	pos := p.fileSet.Position(typeSpec.Pos())
	end := p.fileSet.Position(typeSpec.End())

	var elementType models.ElementType
	var signature string
	var relationships []models.Relationship

	switch t := typeSpec.Type.(type) {
	case *ast.StructType:
		elementType = models.Struct
		signature = p.extractStructSignature(typeSpec.Name.Name, t)

		// Create relationships for struct fields
		if t.Fields != nil {
			for _, field := range t.Fields.List {
				if field.Type != nil {
					// Create HAS_FIELD relationship
					relationships = append(relationships, models.Relationship{
						ID:     uuid.New().String(),
						FromID: "",
						ToID:   "",
						Type:   models.HasField,
						Properties: map[string]interface{}{
							"field_name": p.getFieldName(field),
							"field_type": p.exprToString(field.Type),
						},
					})
				}
			}
		}
	case *ast.InterfaceType:
		elementType = models.Interface
		signature = p.extractInterfaceSignature(typeSpec.Name.Name, t)

		// Create relationships for interface methods
		if t.Methods != nil {
			for _, method := range t.Methods.List {
				if method.Type != nil {
					relationships = append(relationships, models.Relationship{
						ID:     uuid.New().String(),
						FromID: "",
						ToID:   "",
						Type:   models.HasMethod,
						Properties: map[string]interface{}{
							"method_name": p.getFieldName(method),
							"method_type": p.exprToString(method.Type),
						},
					})
				}
			}
		}
	default:
		elementType = models.Struct 
		signature = fmt.Sprintf("type %s %s", typeSpec.Name.Name, p.exprToString(typeSpec.Type))
	}

	// Extract doc comments
	var docComment string
	if genDecl.Doc != nil {
		docComment = ExtractDocComment(p.extractComments(genDecl.Doc))
	}

	code := GetLineRange(content, pos.Line, end.Line)

	element := &models.CodeElement{
		ID:         uuid.New().String(),
		Name:       typeSpec.Name.Name,
		Type:       elementType,
		FilePath:   filePath,
		StartLine:  pos.Line,
		EndLine:    end.Line,
		Package:    packageName,
		Signature:  signature,
		DocComment: docComment,
		Code:       code,
		Metadata: map[string]interface{}{
			"is_exported": typeSpec.Name.IsExported(),
			"type_name":   p.exprToString(typeSpec.Type),
		},
	}

	for i := range relationships {
		relationships[i].FromID = element.ID
	}

	return element, relationships
}

func (p *GoParser) parseValueSpec(valueSpec *ast.ValueSpec, genDecl *ast.GenDecl, filePath, packageName, content string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	for _, name := range valueSpec.Names {
		if name == nil {
			continue
		}

		pos := p.fileSet.Position(name.Pos())
		end := p.fileSet.Position(valueSpec.End())

		elementType := models.Variable
		if genDecl.Tok == token.CONST {
			elementType = models.Constant
		}

		// Extract type and value
		signature := p.extractValueSignature(name.Name, valueSpec, genDecl.Tok)

		// Extract doc comments
		var docComment string
		if genDecl.Doc != nil {
			docComment = ExtractDocComment(p.extractComments(genDecl.Doc))
		}

		code := GetLineRange(content, pos.Line, end.Line)

		element := models.CodeElement{
			ID:         uuid.New().String(),
			Name:       name.Name,
			Type:       elementType,
			FilePath:   filePath,
			StartLine:  pos.Line,
			EndLine:    end.Line,
			Package:    packageName,
			Signature:  signature,
			DocComment: docComment,
			Code:       code,
			Metadata: map[string]interface{}{
				"is_exported": name.IsExported(),
				"is_const":    genDecl.Tok == token.CONST,
			},
		}

		elements = append(elements, element)
	}

	return elements, relationships
}

func (p *GoParser) parseImportSpec(importSpec *ast.ImportSpec, genDecl *ast.GenDecl, filePath, packageName, content string) *models.CodeElement {
	if importSpec == nil || importSpec.Path == nil {
		return nil
	}

	pos := p.fileSet.Position(importSpec.Pos())
	end := p.fileSet.Position(importSpec.End())

	importPath := strings.Trim(importSpec.Path.Value, "\"")
	var alias string
	if importSpec.Name != nil {
		alias = importSpec.Name.Name
	}

	signature := fmt.Sprintf("import %s", importSpec.Path.Value)
	if alias != "" {
		signature = fmt.Sprintf("import %s %s", alias, importSpec.Path.Value)
	}

	code := GetLineRange(content, pos.Line, end.Line)

	return &models.CodeElement{
		ID:        uuid.New().String(),
		Name:      importPath,
		Type:      models.Import,
		FilePath:  filePath,
		StartLine: pos.Line,
		EndLine:   end.Line,
		Package:   packageName,
		Signature: signature,
		Code:      code,
		Metadata: map[string]interface{}{
			"import_path": importPath,
			"alias":       alias,
		},
	}
}

func (p *GoParser) extractComments(commentGroup *ast.CommentGroup) []string {
	if commentGroup == nil {
		return nil
	}

	var comments []string
	for _, comment := range commentGroup.List {
		comments = append(comments, comment.Text)
	}
	return comments
}

func (p *GoParser) extractFuncSignature(funcDecl *ast.FuncDecl) string {
	var parts []string

	parts = append(parts, "func")

	// Add receiver
	if funcDecl.Recv != nil {
		receiver := p.formatFieldList(funcDecl.Recv)
		parts = append(parts, fmt.Sprintf("(%s)", receiver))
	}

	// Add function name
	parts = append(parts, funcDecl.Name.Name)

	// Add parameters
	params := p.formatFieldList(funcDecl.Type.Params)
	parts = append(parts, fmt.Sprintf("(%s)", params))

	// Add return types
	if funcDecl.Type.Results != nil {
		results := p.formatFieldList(funcDecl.Type.Results)
		if len(funcDecl.Type.Results.List) == 1 && len(funcDecl.Type.Results.List[0].Names) == 0 {
			parts = append(parts, results)
		} else {
			parts = append(parts, fmt.Sprintf("(%s)", results))
		}
	}

	return strings.Join(parts, " ")
}

func (p *GoParser) extractStructSignature(name string, structType *ast.StructType) string {
	if structType.Fields == nil || len(structType.Fields.List) == 0 {
		return fmt.Sprintf("type %s struct{}", name)
	}

	return fmt.Sprintf("type %s struct { ... }", name)
}

func (p *GoParser) extractInterfaceSignature(name string, interfaceType *ast.InterfaceType) string {
	if interfaceType.Methods == nil || len(interfaceType.Methods.List) == 0 {
		return fmt.Sprintf("type %s interface{}", name)
	}

	return fmt.Sprintf("type %s interface { ... }", name)
}

func (p *GoParser) extractValueSignature(name string, valueSpec *ast.ValueSpec, tok token.Token) string {
	keyword := "var"
	if tok == token.CONST {
		keyword = "const"
	}

	signature := fmt.Sprintf("%s %s", keyword, name)

	if valueSpec.Type != nil {
		signature += " " + p.exprToString(valueSpec.Type)
	}

	if len(valueSpec.Values) > 0 {
		signature += " = ..."
	}

	return signature
}

func (p *GoParser) formatFieldList(fieldList *ast.FieldList) string {
	if fieldList == nil || len(fieldList.List) == 0 {
		return ""
	}

	var parts []string
	for _, field := range fieldList.List {
		fieldStr := p.exprToString(field.Type)
		if len(field.Names) > 0 {
			names := make([]string, len(field.Names))
			for i, name := range field.Names {
				names[i] = name.Name
			}
			fieldStr = strings.Join(names, ", ") + " " + fieldStr
		}
		parts = append(parts, fieldStr)
	}

	return strings.Join(parts, ", ")
}

func (p *GoParser) exprToString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + p.exprToString(e.X)
	case *ast.SelectorExpr:
		return p.exprToString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + p.exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + p.exprToString(e.Key) + "]" + p.exprToString(e.Value)
	case *ast.ChanType:
		return "chan " + p.exprToString(e.Value)
	case *ast.FuncType:
		return "func(...)"
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	default:
		return "unknown"
	}
}

func (p *GoParser) getReturnCount(funcType *ast.FuncType) int {
	if funcType.Results == nil {
		return 0
	}
	return len(funcType.Results.List)
}

func (p *GoParser) getFieldName(field *ast.Field) string {
	if len(field.Names) == 0 {
		return ""
	}
	return field.Names[0].Name
}
