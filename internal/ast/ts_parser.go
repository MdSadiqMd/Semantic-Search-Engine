package ast

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/google/uuid"
)

type TypeScriptParser struct{}

func NewTypeScriptParser() *TypeScriptParser {
	return &TypeScriptParser{}
}

func (p *TypeScriptParser) GetSupportedExtensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx"}
}

func (p *TypeScriptParser) GetLanguage() string {
	return "typescript"
}

func (p *TypeScriptParser) ParseFile(ctx context.Context, filePath string) ([]models.CodeElement, []models.Relationship, error) {
	content, err := ReadFileContent(filePath)
	if err != nil {
		return nil, nil, err
	}

	var elements []models.CodeElement
	var relationships []models.Relationship

	// Parse imports
	imports := p.parseImports(content, filePath)
	elements = append(elements, imports...)

	// Parse functions
	functions, funcRels := p.parseFunctions(content, filePath)
	elements = append(elements, functions...)
	relationships = append(relationships, funcRels...)

	// Parse classes
	classes, classRels := p.parseClasses(content, filePath)
	elements = append(elements, classes...)
	relationships = append(relationships, classRels...)

	// Parse interfaces
	interfaces := p.parseInterfaces(content, filePath)
	elements = append(elements, interfaces...)

	// Parse type aliases
	types := p.parseTypes(content, filePath)
	elements = append(elements, types...)

	// Parse variables and constants
	variables := p.parseVariables(content, filePath)
	elements = append(elements, variables...)

	return elements, relationships, nil
}

func (p *TypeScriptParser) parseImports(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Match import statements
	importRegex := regexp.MustCompile(`(?m)^import\s+(.*?)\s+from\s+['"]([^'"]+)['"];?`)
	matches := importRegex.FindAllStringSubmatch(content, -1)

	strings.Split(content, "\n")

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		fullMatch := match[0]
		importedItems := match[1]
		importPath := match[2]

		// Find line number
		lineNum := p.findLineNumber(content, fullMatch)

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      importPath,
			Type:      models.Import,
			FilePath:  filePath,
			StartLine: lineNum,
			EndLine:   lineNum,
			Package:   p.getPackageFromFile(filePath),
			Signature: fullMatch,
			Code:      fullMatch,
			Metadata: map[string]interface{}{
				"import_path":    importPath,
				"imported_items": importedItems,
				"import_type":    "es6",
			},
		}

		elements = append(elements, element)
	}

	// Match require statements
	requireRegex := regexp.MustCompile(`(?m)(?:const|let|var)\s+([^=]+)=\s*require\(['"]([^'"]+)['"]\)`)
	requireMatches := requireRegex.FindAllStringSubmatch(content, -1)

	for _, match := range requireMatches {
		if len(match) < 3 {
			continue
		}

		fullMatch := match[0]
		variableName := strings.TrimSpace(match[1])
		importPath := match[2]
		lineNum := p.findLineNumber(content, fullMatch)

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      importPath,
			Type:      models.Import,
			FilePath:  filePath,
			StartLine: lineNum,
			EndLine:   lineNum,
			Package:   p.getPackageFromFile(filePath),
			Signature: fullMatch,
			Code:      fullMatch,
			Metadata: map[string]interface{}{
				"import_path":   importPath,
				"variable_name": variableName,
				"import_type":   "require",
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *TypeScriptParser) parseFunctions(content, filePath string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Function declarations
	funcRegex := regexp.MustCompile(`(?ms)(?:^|\n)((?:export\s+)?(?:async\s+)?function\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\([^)]*\)(?:\s*:\s*[^{]+)?\s*\{)`)
	matches := funcRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		signatureStart := match[2]
		signatureEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		signature := content[signatureStart:signatureEnd]
		functionName := content[nameStart:nameEnd]

		startLine := p.findLineNumberByIndex(content, signatureStart)

		// Find the end of the function by matching braces
		endIndex := p.findFunctionEnd(content, match[3])
		endLine := p.findLineNumberByIndex(content, endIndex)

		code := content[signatureStart:endIndex]

		docComment := p.extractDocComment(content, signatureStart)

		element := models.CodeElement{
			ID:         uuid.New().String(),
			Name:       functionName,
			Type:       models.Function,
			FilePath:   filePath,
			StartLine:  startLine,
			EndLine:    endLine,
			Package:    p.getPackageFromFile(filePath),
			Signature:  signature,
			DocComment: docComment,
			Code:       code,
			Metadata: map[string]interface{}{
				"is_async":   strings.Contains(signature, "async"),
				"is_export":  strings.Contains(signature, "export"),
				"parameters": p.extractParameters(signature),
			},
		}

		elements = append(elements, element)

		callRels := p.findFunctionCalls(code, element.ID)
		relationships = append(relationships, callRels...)
	}

	// Arrow functions
	arrowRegex := regexp.MustCompile(`(?m)(?:const|let|var)\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[:=]\s*(?:\([^)]*\)|[^=])\s*=>\s*[{(]`)
	arrowRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		matchStart := match[0]
		matchEnd := match[1]
		nameStart := match[2]
		nameEnd := match[3]

		functionName := content[nameStart:nameEnd]
		startLine := p.findLineNumberByIndex(content, matchStart)

		endIndex := p.findArrowFunctionEnd(content, matchEnd)
		endLine := p.findLineNumberByIndex(content, endIndex)

		signature := content[matchStart:matchEnd]
		code := content[matchStart:endIndex]

		docComment := p.extractDocComment(content, matchStart)

		element := models.CodeElement{
			ID:         uuid.New().String(),
			Name:       functionName,
			Type:       models.Function,
			FilePath:   filePath,
			StartLine:  startLine,
			EndLine:    endLine,
			Package:    p.getPackageFromFile(filePath),
			Signature:  signature,
			DocComment: docComment,
			Code:       code,
			Metadata: map[string]interface{}{
				"is_arrow_function": true,
				"parameters":        p.extractParameters(signature),
			},
		}

		elements = append(elements, element)

		callRels := p.findFunctionCalls(code, element.ID)
		relationships = append(relationships, callRels...)
	}

	return elements, relationships
}

func (p *TypeScriptParser) parseClasses(content, filePath string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Class declarations
	classRegex := regexp.MustCompile(`(?ms)(?:^|\n)((?:export\s+)?(?:abstract\s+)?class\s+([a-zA-Z_$][a-zA-Z0-9_$]*)(?:\s+extends\s+[a-zA-Z_$][a-zA-Z0-9_$]*)?(?:\s+implements\s+[^{]+)?\s*\{)`)
	matches := classRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		signatureStart := match[2]
		signatureEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		signature := content[signatureStart:signatureEnd]
		className := content[nameStart:nameEnd]

		startLine := p.findLineNumberByIndex(content, signatureStart)

		// Find the end of the class by matching braces
		endIndex := p.findClassEnd(content, match[3])
		endLine := p.findLineNumberByIndex(content, endIndex)

		code := content[signatureStart:endIndex]

		// Extract doc comment
		docComment := p.extractDocComment(content, signatureStart)

		element := models.CodeElement{
			ID:         uuid.New().String(),
			Name:       className,
			Type:       models.Struct, // Using Struct type for classes
			FilePath:   filePath,
			StartLine:  startLine,
			EndLine:    endLine,
			Package:    p.getPackageFromFile(filePath),
			Signature:  signature,
			DocComment: docComment,
			Code:       code,
			Metadata: map[string]interface{}{
				"is_class":    true,
				"is_abstract": strings.Contains(signature, "abstract"),
				"is_export":   strings.Contains(signature, "export"),
				"extends":     p.extractExtends(signature),
				"implements":  p.extractImplements(signature),
			},
		}

		elements = append(elements, element)

		// Parse methods within the class
		methods, methodRels := p.parseClassMethods(code, element.ID, filePath, startLine)
		elements = append(elements, methods...)
		relationships = append(relationships, methodRels...)

		// Parse properties within the class
		properties := p.parseClassProperties(code, element.ID, filePath, startLine)
		elements = append(elements, properties...)
	}

	return elements, relationships
}

func (p *TypeScriptParser) parseClassMethods(classCode, classID, filePath string, classStartLine int) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Method patterns
	methodRegex := regexp.MustCompile(`(?ms)((?:public|private|protected|static)?\s*(?:async\s+)?([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\([^)]*\)(?:\s*:\s*[^{]+)?\s*\{)`)
	matches := methodRegex.FindAllStringSubmatchIndex(classCode, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		signatureStart := match[2]
		signatureEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		signature := classCode[signatureStart:signatureEnd]
		methodName := classCode[nameStart:nameEnd]

		// Skip if it's a constructor or getter/setter pattern
		if methodName == "constructor" || strings.Contains(signature, "get ") || strings.Contains(signature, "set ") {
			continue
		}

		startLine := classStartLine + p.findLineNumberByIndex(classCode, signatureStart) - 1

		endIndex := p.findFunctionEnd(classCode, match[3])
		endLine := classStartLine + p.findLineNumberByIndex(classCode, endIndex) - 1

		code := classCode[signatureStart:endIndex]

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      methodName,
			Type:      models.Method,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			Package:   p.getPackageFromFile(filePath),
			Signature: signature,
			Code:      code,
			Metadata: map[string]interface{}{
				"class_id":   classID,
				"visibility": p.extractVisibility(signature),
				"is_static":  strings.Contains(signature, "static"),
				"is_async":   strings.Contains(signature, "async"),
				"parameters": p.extractParameters(signature),
			},
		}

		elements = append(elements, element)

		// Create HAS_METHOD relationship
		relationships = append(relationships, models.Relationship{
			ID:     uuid.New().String(),
			FromID: classID,
			ToID:   element.ID,
			Type:   models.HasMethod,
			Properties: map[string]interface{}{
				"method_name": methodName,
			},
		})

		// Find function calls within this method
		callRels := p.findFunctionCalls(code, element.ID)
		relationships = append(relationships, callRels...)
	}

	return elements, relationships
}

func (p *TypeScriptParser) parseClassProperties(classCode, classID, filePath string, classStartLine int) []models.CodeElement {
	var elements []models.CodeElement

	// Property patterns
	propRegex := regexp.MustCompile(`(?m)^\s*((?:public|private|protected|static|readonly)?\s*([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[?:]?\s*[^=\n]*(?:=\s*[^;\n]+)?[;\n])`)
	matches := propRegex.FindAllStringSubmatchIndex(classCode, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		fullStart := match[0]
		fullEnd := match[1]
		nameStart := match[4]
		nameEnd := match[5]

		propertyName := classCode[nameStart:nameEnd]
		fullProperty := classCode[fullStart:fullEnd]

		startLine := classStartLine + p.findLineNumberByIndex(classCode, fullStart) - 1

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      propertyName,
			Type:      models.Variable,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   startLine,
			Package:   p.getPackageFromFile(filePath),
			Signature: strings.TrimSpace(fullProperty),
			Code:      strings.TrimSpace(fullProperty),
			Metadata: map[string]interface{}{
				"class_id":    classID,
				"visibility":  p.extractVisibility(fullProperty),
				"is_static":   strings.Contains(fullProperty, "static"),
				"is_readonly": strings.Contains(fullProperty, "readonly"),
				"is_property": true,
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *TypeScriptParser) parseInterfaces(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Interface declarations
	interfaceRegex := regexp.MustCompile(`(?ms)(?:^|\n)((?:export\s+)?interface\s+([a-zA-Z_$][a-zA-Z0-9_$]*)(?:\s+extends\s+[^{]+)?\s*\{)`)
	matches := interfaceRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		signatureStart := match[2]
		signatureEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		signature := content[signatureStart:signatureEnd]
		interfaceName := content[nameStart:nameEnd]

		startLine := p.findLineNumberByIndex(content, signatureStart)

		endIndex := p.findInterfaceEnd(content, match[3])
		endLine := p.findLineNumberByIndex(content, endIndex)

		code := content[signatureStart:endIndex]
		docComment := p.extractDocComment(content, signatureStart)

		element := models.CodeElement{
			ID:         uuid.New().String(),
			Name:       interfaceName,
			Type:       models.Interface,
			FilePath:   filePath,
			StartLine:  startLine,
			EndLine:    endLine,
			Package:    p.getPackageFromFile(filePath),
			Signature:  signature,
			DocComment: docComment,
			Code:       code,
			Metadata: map[string]interface{}{
				"is_export": strings.Contains(signature, "export"),
				"extends":   p.extractExtends(signature),
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *TypeScriptParser) parseTypes(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Type alias declarations
	typeRegex := regexp.MustCompile(`(?m)(?:^|\n)((?:export\s+)?type\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*=\s*[^;\n]+[;\n]?)`)
	matches := typeRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		fullStart := match[2]
		fullEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		fullDeclaration := content[fullStart:fullEnd]
		typeName := content[nameStart:nameEnd]
		startLine := p.findLineNumberByIndex(content, fullStart)

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      typeName,
			Type:      models.Struct, // Using Struct for type aliases
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   startLine,
			Package:   p.getPackageFromFile(filePath),
			Signature: strings.TrimSpace(fullDeclaration),
			Code:      strings.TrimSpace(fullDeclaration),
			Metadata: map[string]interface{}{
				"is_type_alias": true,
				"is_export":     strings.Contains(fullDeclaration, "export"),
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *TypeScriptParser) parseVariables(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Variable declarations (const, let, var)
	varRegex := regexp.MustCompile(`(?m)(?:^|\n)((?:export\s+)?(?:const|let|var)\s+([a-zA-Z_$][a-zA-Z0-9_$]*)\s*[^=\n]*(?:=\s*[^;\n]+)?[;\n]?)`)
	matches := varRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		fullStart := match[2]
		fullEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		fullDeclaration := content[fullStart:fullEnd]
		variableName := content[nameStart:nameEnd]
		startLine := p.findLineNumberByIndex(content, fullStart)

		elementType := models.Variable
		if strings.Contains(fullDeclaration, "const") {
			elementType = models.Constant
		}

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      variableName,
			Type:      elementType,
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   startLine,
			Package:   p.getPackageFromFile(filePath),
			Signature: strings.TrimSpace(fullDeclaration),
			Code:      strings.TrimSpace(fullDeclaration),
			Metadata: map[string]interface{}{
				"is_export": strings.Contains(fullDeclaration, "export"),
				"var_type":  p.extractVarType(fullDeclaration),
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *TypeScriptParser) findLineNumber(content, searchText string) int {
	beforeText := content[:strings.Index(content, searchText)]
	return strings.Count(beforeText, "\n") + 1
}

func (p *TypeScriptParser) findLineNumberByIndex(content string, index int) int {
	if index < 0 || index >= len(content) {
		return 1
	}
	return strings.Count(content[:index], "\n") + 1
}

func (p *TypeScriptParser) findFunctionEnd(content string, startIndex int) int {
	braceCount := 0
	for i := startIndex; i < len(content); i++ {
		if content[i] == '{' {
			braceCount++
		} else if content[i] == '}' {
			braceCount--
			if braceCount == 0 {
				return i + 1
			}
		}
	}
	return len(content)
}

func (p *TypeScriptParser) findArrowFunctionEnd(content string, startIndex int) int {
	// Simple heuristic: find next function declaration or end of file
	nextFuncIndex := strings.Index(content[startIndex:], "\nfunction")
	nextClassIndex := strings.Index(content[startIndex:], "\nclass")
	nextConstIndex := strings.Index(content[startIndex:], "\nconst")

	minIndex := len(content)
	if nextFuncIndex != -1 && nextFuncIndex < minIndex {
		minIndex = startIndex + nextFuncIndex
	}
	if nextClassIndex != -1 && nextClassIndex < minIndex {
		minIndex = startIndex + nextClassIndex
	}
	if nextConstIndex != -1 && nextConstIndex < minIndex {
		minIndex = startIndex + nextConstIndex
	}

	return minIndex
}

func (p *TypeScriptParser) findClassEnd(content string, startIndex int) int {
	return p.findFunctionEnd(content, startIndex)
}

func (p *TypeScriptParser) findInterfaceEnd(content string, startIndex int) int {
	return p.findFunctionEnd(content, startIndex)
}

func (p *TypeScriptParser) extractDocComment(content string, position int) string {
	beforePosition := content[:position]
	lines := strings.Split(beforePosition, "\n")

	var docLines []string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "*/") {
			continue
		}
		if strings.HasPrefix(line, "*") || strings.HasPrefix(line, "//") {
			cleaned := strings.TrimPrefix(line, "*")
			cleaned = strings.TrimPrefix(cleaned, "//")
			cleaned = strings.TrimSpace(cleaned)
			if cleaned != "" {
				docLines = append([]string{cleaned}, docLines...)
			}
		} else if strings.HasPrefix(line, "/**") {
			break
		} else if line != "" {
			break
		}
	}

	return strings.Join(docLines, "\n")
}

func (p *TypeScriptParser) extractParameters(signature string) []string {
	parenStart := strings.Index(signature, "(")
	parenEnd := strings.LastIndex(signature, ")")

	if parenStart == -1 || parenEnd == -1 || parenEnd <= parenStart {
		return []string{}
	}

	paramString := signature[parenStart+1 : parenEnd]
	if strings.TrimSpace(paramString) == "" {
		return []string{}
	}

	params := strings.Split(paramString, ",")
	result := make([]string, len(params))
	for i, param := range params {
		result[i] = strings.TrimSpace(param)
	}

	return result
}

func (p *TypeScriptParser) extractVisibility(text string) string {
	if strings.Contains(text, "private") {
		return "private"
	}
	if strings.Contains(text, "protected") {
		return "protected"
	}
	return "public"
}

func (p *TypeScriptParser) extractExtends(signature string) string {
	extendsIndex := strings.Index(signature, "extends")
	if extendsIndex == -1 {
		return ""
	}

	afterExtends := signature[extendsIndex+7:]
	implementsIndex := strings.Index(afterExtends, "implements")
	braceIndex := strings.Index(afterExtends, "{")

	endIndex := len(afterExtends)
	if implementsIndex != -1 && implementsIndex < endIndex {
		endIndex = implementsIndex
	}
	if braceIndex != -1 && braceIndex < endIndex {
		endIndex = braceIndex
	}

	return strings.TrimSpace(afterExtends[:endIndex])
}

func (p *TypeScriptParser) extractImplements(signature string) string {
	implementsIndex := strings.Index(signature, "implements")
	if implementsIndex == -1 {
		return ""
	}

	afterImplements := signature[implementsIndex+10:]
	braceIndex := strings.Index(afterImplements, "{")

	if braceIndex == -1 {
		return strings.TrimSpace(afterImplements)
	}

	return strings.TrimSpace(afterImplements[:braceIndex])
}

func (p *TypeScriptParser) extractVarType(declaration string) string {
	if strings.Contains(declaration, "const") {
		return "const"
	}
	if strings.Contains(declaration, "let") {
		return "let"
	}
	return "var"
}

func (p *TypeScriptParser) findFunctionCalls(code, callerID string) []models.Relationship {
	var relationships []models.Relationship

	// Simple pattern to find function calls
	callRegex := regexp.MustCompile(`([a-zA-Z_$][a-zA-Z0-9_$]*)\s*\(`)
	matches := callRegex.FindAllStringSubmatch(code, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			functionName := match[1]
			// Skip common keywords and constructors
			if functionName == "if" || functionName == "for" || functionName == "while" ||
				functionName == "switch" || functionName == "catch" || functionName == "new" {
				continue
			}

			relationships = append(relationships, models.Relationship{
				ID:     uuid.New().String(),
				FromID: callerID,
				ToID:   "",
				Type:   models.Calls,
				Properties: map[string]interface{}{
					"function_name": functionName,
					"language":      "typescript",
				},
			})
		}
	}

	return relationships
}

func (p *TypeScriptParser) getPackageFromFile(filePath string) string {
	parts := strings.Split(filePath, string(filepath.Separator))
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], "/")
	}
	return "main"
}
