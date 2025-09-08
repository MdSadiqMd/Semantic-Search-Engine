package ast

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/google/uuid"
)

type PythonParser struct{}

func NewPythonParser() *PythonParser {
	return &PythonParser{}
}

func (p *PythonParser) GetSupportedExtensions() []string {
	return []string{".py"}
}

func (p *PythonParser) GetLanguage() string {
	return "python"
}

func (p *PythonParser) ParseFile(ctx context.Context, filePath string) ([]models.CodeElement, []models.Relationship, error) {
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

	// Parse variables and constants
	variables := p.parseVariables(content, filePath)
	elements = append(elements, variables...)

	return elements, relationships, nil
}

func (p *PythonParser) parseImports(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Match import statements
	importRegex := regexp.MustCompile(`(?m)^(?:from\s+([^\s]+)\s+)?import\s+([^\n]+)`)
	matches := importRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}

		fullMatch := match[0]
		fromModule := match[1]
		importedItems := match[2]

		lineNum := p.findLineNumber(content, fullMatch)

		var importPath string
		if fromModule != "" {
			importPath = fromModule
		} else {
			items := strings.Split(importedItems, ",")
			if len(items) > 0 {
				importPath = strings.TrimSpace(items[0])
			}
		}

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
				"from_module":    fromModule,
				"import_type":    "python",
			},
		}
		elements = append(elements, element)
	}

	return elements
}

func (p *PythonParser) parseFunctions(content, filePath string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Function definitions
	funcRegex := regexp.MustCompile(`(?m)^(\s*)def\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\([^)]*\)(?:\s*->\s*[^:]+)?:`)
	matches := funcRegex.FindAllStringSubmatchIndex(content, -1)

	lines := strings.Split(content, "\n")

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		indentStart := match[2]
		indentEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		indent := content[indentStart:indentEnd]
		functionName := content[nameStart:nameEnd]
		startLine := p.findLineNumberByIndex(content, match[0])

		// Find function signature
		signatureEnd := p.findSignatureEnd(content, match[1])
		signature := strings.TrimSpace(content[match[0]:signatureEnd])

		// Find end of function based on indentation
		endLine := p.findFunctionEndByIndentation(lines, startLine-1, len(indent))

		// Extract function body
		if endLine > startLine {
			code := strings.Join(lines[startLine-1:endLine], "\n")

			// Extract docstring
			docComment := p.extractDocstring(lines, startLine-1)

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
					"indent_level": len(indent),
					"is_method":    len(indent) > 0,
					"parameters":   p.extractParameters(signature),
					"return_type":  p.extractReturnType(signature),
				},
			}
			elements = append(elements, element)

			// Find function calls within this function
			callRels := p.findFunctionCalls(code, element.ID)
			relationships = append(relationships, callRels...)
		}
	}

	return elements, relationships
}

func (p *PythonParser) parseClasses(content, filePath string) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Class definitions
	classRegex := regexp.MustCompile(`(?m)^(\s*)class\s+([a-zA-Z_][a-zA-Z0-9_]*)(?:\([^)]*\))?:`)
	matches := classRegex.FindAllStringSubmatchIndex(content, -1)

	lines := strings.Split(content, "\n")

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		indentStart := match[2]
		indentEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		indent := content[indentStart:indentEnd]
		className := content[nameStart:nameEnd]
		startLine := p.findLineNumberByIndex(content, match[0])

		// Find class signature
		signatureEnd := p.findSignatureEnd(content, match[1])
		signature := strings.TrimSpace(content[match[0]:signatureEnd])

		// Find end of class based on indentation
		endLine := p.findFunctionEndByIndentation(lines, startLine-1, len(indent))

		if endLine > startLine {
			code := strings.Join(lines[startLine-1:endLine], "\n")

			// Extract docstring
			docComment := p.extractDocstring(lines, startLine-1)

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
					"is_class":     true,
					"indent_level": len(indent),
					"base_classes": p.extractBaseClasses(signature),
				},
			}

			elements = append(elements, element)

			// Parse methods within the class
			methods, methodRels := p.parseClassMethods(code, element.ID, filePath, startLine, len(indent))
			elements = append(elements, methods...)
			relationships = append(relationships, methodRels...)

			// Parse class variables
			classVars := p.parseClassVariables(code, element.ID, filePath, startLine, len(indent))
			elements = append(elements, classVars...)
		}
	}

	return elements, relationships
}

func (p *PythonParser) parseClassMethods(classCode, classID, filePath string, classStartLine, classIndent int) ([]models.CodeElement, []models.Relationship) {
	var elements []models.CodeElement
	var relationships []models.Relationship

	// Method patterns within class
	methodRegex := regexp.MustCompile(`(?m)^(\s+)def\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\([^)]*\)(?:\s*->\s*[^:]+)?:`)
	matches := methodRegex.FindAllStringSubmatchIndex(classCode, -1)

	classLines := strings.Split(classCode, "\n")

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		indentStart := match[2]
		indentEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		indent := classCode[indentStart:indentEnd]
		methodName := classCode[nameStart:nameEnd]

		// Skip if not properly indented within class
		if len(indent) <= classIndent {
			continue
		}

		relativeStartLine := p.findLineNumberByIndex(classCode, match[0])
		absoluteStartLine := classStartLine + relativeStartLine - 1

		// Find method signature
		signatureEnd := p.findSignatureEnd(classCode, match[1])
		signature := strings.TrimSpace(classCode[match[0]:signatureEnd])

		// Find end of method
		relativeEndLine := p.findFunctionEndByIndentation(classLines, relativeStartLine-1, len(indent))
		absoluteEndLine := classStartLine + relativeEndLine - 1

		if relativeEndLine > relativeStartLine {
			methodCode := strings.Join(classLines[relativeStartLine-1:relativeEndLine], "\n")

			// Extract docstring
			docComment := p.extractDocstring(classLines, relativeStartLine-1)

			element := models.CodeElement{
				ID:         uuid.New().String(),
				Name:       methodName,
				Type:       models.Method,
				FilePath:   filePath,
				StartLine:  absoluteStartLine,
				EndLine:    absoluteEndLine,
				Package:    p.getPackageFromFile(filePath),
				Signature:  signature,
				DocComment: docComment,
				Code:       methodCode,
				Metadata: map[string]interface{}{
					"class_id":     classID,
					"indent_level": len(indent),
					"is_static":    !strings.Contains(signature, "self"),
					"is_private":   strings.HasPrefix(methodName, "_"),
					"parameters":   p.extractParameters(signature),
					"return_type":  p.extractReturnType(signature),
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
			callRels := p.findFunctionCalls(methodCode, element.ID)
			relationships = append(relationships, callRels...)
		}
	}

	return elements, relationships
}

func (p *PythonParser) parseClassVariables(classCode, classID, filePath string, classStartLine, classIndent int) []models.CodeElement {
	var elements []models.CodeElement

	// Class variable patterns
	varRegex := regexp.MustCompile(`(?m)^(\s+)([a-zA-Z_][a-zA-Z0-9_]*)\s*[:]?\s*=\s*[^\n]+`)
	matches := varRegex.FindAllStringSubmatchIndex(classCode, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		indentStart := match[2]
		indentEnd := match[3]
		nameStart := match[4]
		nameEnd := match[5]

		indent := classCode[indentStart:indentEnd]
		varName := classCode[nameStart:nameEnd]

		// Skip if not at class level or is in a method
		if len(indent) != classIndent+4 { // Assuming 4-space indentation
			continue
		}

		relativeLineNum := p.findLineNumberByIndex(classCode, match[0])
		absoluteLineNum := classStartLine + relativeLineNum - 1

		fullDeclaration := strings.TrimSpace(classCode[match[0]:match[1]])

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      varName,
			Type:      models.Variable,
			FilePath:  filePath,
			StartLine: absoluteLineNum,
			EndLine:   absoluteLineNum,
			Package:   p.getPackageFromFile(filePath),
			Signature: fullDeclaration,
			Code:      fullDeclaration,
			Metadata: map[string]interface{}{
				"class_id":     classID,
				"is_private":   strings.HasPrefix(varName, "_"),
				"is_class_var": true,
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *PythonParser) parseVariables(content, filePath string) []models.CodeElement {
	var elements []models.CodeElement

	// Global variable declarations
	varRegex := regexp.MustCompile(`(?m)^([a-zA-Z_][a-zA-Z0-9_]*)\s*[:]?\s*=\s*[^\n]+`)
	matches := varRegex.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		nameStart := match[2]
		nameEnd := match[3]

		varName := content[nameStart:nameEnd]
		lineNum := p.findLineNumberByIndex(content, match[0])

		fullDeclaration := strings.TrimSpace(content[match[0]:match[1]])

		// Skip if it looks like it's inside a function or class
		line := strings.Split(content, "\n")[lineNum-1]
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		elementType := models.Variable
		if strings.ToUpper(varName) == varName && len(varName) > 1 {
			elementType = models.Constant
		}

		element := models.CodeElement{
			ID:        uuid.New().String(),
			Name:      varName,
			Type:      elementType,
			FilePath:  filePath,
			StartLine: lineNum,
			EndLine:   lineNum,
			Package:   p.getPackageFromFile(filePath),
			Signature: fullDeclaration,
			Code:      fullDeclaration,
			Metadata: map[string]interface{}{
				"is_global":  true,
				"is_private": strings.HasPrefix(varName, "_"),
				"var_type":   p.extractVariableType(fullDeclaration),
			},
		}

		elements = append(elements, element)
	}

	return elements
}

func (p *PythonParser) findLineNumber(content, searchText string) int {
	beforeText := content[:strings.Index(content, searchText)]
	return strings.Count(beforeText, "\n") + 1
}

func (p *PythonParser) findLineNumberByIndex(content string, index int) int {
	if index < 0 || index >= len(content) {
		return 1
	}
	return strings.Count(content[:index], "\n") + 1
}

func (p *PythonParser) findSignatureEnd(content string, startIndex int) int {
	for i := startIndex; i < len(content); i++ {
		if content[i] == ':' {
			return i + 1
		}
	}
	return len(content)
}

func (p *PythonParser) findFunctionEndByIndentation(lines []string, startLine, indent int) int {
	for i := startLine + 1; i < len(lines); i++ {
		line := lines[i]

		// Empty lines don't count
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Count leading whitespace
		lineIndent := 0
		for _, char := range line {
			if char == ' ' {
				lineIndent++
			} else if char == '\t' {
				lineIndent += 4 // Treat tab as 4 spaces
			} else {
				break
			}
		}

		// If we find a line with same or less indentation, that's the end
		if lineIndent <= indent {
			return i
		}
	}

	return len(lines)
}

func (p *PythonParser) extractDocstring(lines []string, startLine int) string {
	for i := startLine + 1; i < len(lines) && i < startLine+5; i++ {
		line := strings.TrimSpace(lines[i])

		// Check for triple-quoted docstring
		if strings.HasPrefix(line, `"""`) || strings.HasPrefix(line, `'''`) {
			quote := line[:3]
			docLines := []string{}

			// Single line docstring
			if len(line) > 6 && strings.HasSuffix(line, quote) {
				return strings.Trim(line[3:len(line)-3], " \t")
			}

			// Multi-line docstring
			docLines = append(docLines, strings.TrimSpace(line[3:]))

			for j := i + 1; j < len(lines); j++ {
				docLine := lines[j]
				if strings.Contains(docLine, quote) {
					// End of docstring
					endPart := docLine[:strings.Index(docLine, quote)]
					if strings.TrimSpace(endPart) != "" {
						docLines = append(docLines, strings.TrimSpace(endPart))
					}
					break
				}
				docLines = append(docLines, strings.TrimSpace(docLine))
			}

			return strings.Join(docLines, "\n")
		}
	}

	return ""
}

func (p *PythonParser) extractParameters(signature string) []string {
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

func (p *PythonParser) extractReturnType(signature string) string {
	arrowIndex := strings.Index(signature, "->")
	if arrowIndex == -1 {
		return ""
	}

	returnPart := signature[arrowIndex+2:]
	colonIndex := strings.Index(returnPart, ":")
	if colonIndex != -1 {
		returnPart = returnPart[:colonIndex]
	}

	return strings.TrimSpace(returnPart)
}

func (p *PythonParser) extractBaseClasses(signature string) []string {
	parenStart := strings.Index(signature, "(")
	parenEnd := strings.LastIndex(signature, ")")

	if parenStart == -1 || parenEnd == -1 || parenEnd <= parenStart {
		return []string{}
	}

	baseString := signature[parenStart+1 : parenEnd]
	if strings.TrimSpace(baseString) == "" {
		return []string{}
	}

	bases := strings.Split(baseString, ",")
	result := make([]string, len(bases))
	for i, base := range bases {
		result[i] = strings.TrimSpace(base)
	}

	return result
}

func (p *PythonParser) extractVariableType(declaration string) string {
	colonIndex := strings.Index(declaration, ":")
	equalIndex := strings.Index(declaration, "=")

	if colonIndex != -1 && (equalIndex == -1 || colonIndex < equalIndex) {
		typeEnd := equalIndex
		if typeEnd == -1 {
			typeEnd = len(declaration)
		}
		return strings.TrimSpace(declaration[colonIndex+1 : typeEnd])
	}

	return ""
}

func (p *PythonParser) findFunctionCalls(code, callerID string) []models.Relationship {
	var relationships []models.Relationship

	// Simple pattern to find function calls
	callRegex := regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	matches := callRegex.FindAllStringSubmatch(code, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			functionName := match[1]
			// Skip common keywords and built-ins
			if functionName == "if" || functionName == "for" || functionName == "while" ||
				functionName == "print" || functionName == "len" || functionName == "range" ||
				functionName == "str" || functionName == "int" || functionName == "float" {
				continue
			}

			relationships = append(relationships, models.Relationship{
				ID:     uuid.New().String(),
				FromID: callerID,
				ToID:   "",
				Type:   models.Calls,
				Properties: map[string]interface{}{
					"function_name": functionName,
					"language":      "python",
				},
			})
		}
	}

	return relationships
}

func (p *PythonParser) getPackageFromFile(filePath string) string {
	parts := strings.Split(filePath, string(filepath.Separator))
	if len(parts) > 1 {
		dirs := parts[:len(parts)-1]
		return strings.Join(dirs, ".")
	}
	return "main"
}
