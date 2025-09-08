package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/ast"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPythonParser_GetSupportedExtensions(t *testing.T) {
	parser := ast.NewPythonParser()
	extensions := parser.GetSupportedExtensions()

	assert.Len(t, extensions, 1)
	assert.Contains(t, extensions, ".py")
}

func TestPythonParser_GetLanguage(t *testing.T) {
	parser := ast.NewPythonParser()
	language := parser.GetLanguage()

	assert.Equal(t, "python", language)
}

func TestPythonParser_ParseFile_SimpleImports(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "imports.py")

	content := `import os
import sys
from typing import List, Dict
from collections import defaultdict, Counter
import json as js
from datetime import datetime, timedelta`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	elements, _, err := parser.ParseFile(ctx, testFile)
	require.NoError(t, err)

	// Count imports
	importCount := 0
	for _, element := range elements {
		if element.Type == models.Import {
			importCount++
			assert.Equal(t, testFile, element.FilePath)
			assert.NotEmpty(t, element.ID)
			assert.NotEmpty(t, element.Name)
			assert.Greater(t, element.StartLine, 0)
		}
	}

	assert.Equal(t, 6, importCount, "Should parse 6 import statements")
}

func TestPythonParser_ParseFile_SimpleFunctions(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "functions.py")

	content := `def simple_func():
    return "hello"

def func_with_params(x: int, y: str = "default") -> str:
    """Function with type hints."""
    return f"{x}_{y}"

def func_no_types(param):
    return param

def func_return_type() -> int:
    return 42`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	elements, relationships, err := parser.ParseFile(ctx, testFile)
	require.NoError(t, err)

	// Count functions
	functionCount := 0
	var functions []models.CodeElement
	for _, element := range elements {
		if element.Type == models.Function {
			functionCount++
			functions = append(functions, element)
			assert.Equal(t, testFile, element.FilePath)
			assert.NotEmpty(t, element.ID)
			assert.NotEmpty(t, element.Name)
			assert.Greater(t, element.StartLine, 0)
			assert.GreaterOrEqual(t, element.EndLine, element.StartLine)
		}
	}

	assert.Equal(t, 4, functionCount, "Should parse 4 functions")

	// Check function names
	functionNames := make([]string, len(functions))
	for i, fn := range functions {
		functionNames[i] = fn.Name
	}
	assert.Contains(t, functionNames, "simple_func")
	assert.Contains(t, functionNames, "func_with_params")
	assert.Contains(t, functionNames, "func_no_types")
	assert.Contains(t, functionNames, "func_return_type")

	// Check for some docstrings
	hasDocstring := false
	for _, fn := range functions {
		if fn.DocComment != "" {
			hasDocstring = true
			break
		}
	}
	assert.True(t, hasDocstring, "Should have at least one function with docstring")

	// Check relationships (function calls)
	if len(relationships) > 0 {
		for _, rel := range relationships {
			if rel.Type == models.Calls {
				assert.NotEmpty(t, rel.FromID)
			}
		}
	}
}

func TestPythonParser_ParseFile_SimpleClasses(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "classes.py")

	content := `class SimpleClass:
    def method(self):
        return "hello"

class Child(Parent):
    """Class with inheritance."""

    class_var = "value"

    def __init__(self, name):
        self.name = name

    def method(self):
        return self.name

class MultipleInheritance(BaseA, BaseB):
    def method_a(self):
        pass

    def method_b(self):
        pass`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	elements, relationships, err := parser.ParseFile(ctx, testFile)
	require.NoError(t, err)

	// Count different types of elements
	classCount := 0
	methodCount := 0
	variableCount := 0

	var classes []models.CodeElement
	for _, element := range elements {
		switch element.Type {
		case models.Struct: // Classes are represented as Struct
			classCount++
			classes = append(classes, element)
		case models.Method:
			methodCount++
		case models.Variable:
			variableCount++
		}
	}

	assert.Equal(t, 3, classCount, "Should parse 3 classes")
	assert.Greater(t, methodCount, 0, "Should parse methods")
	assert.Greater(t, variableCount, 0, "Should parse class variables")

	// Check class names
	classNames := make([]string, len(classes))
	for i, cls := range classes {
		classNames[i] = cls.Name
	}
	assert.Contains(t, classNames, "SimpleClass")
	assert.Contains(t, classNames, "Child")
	assert.Contains(t, classNames, "MultipleInheritance")

	// Check HAS_METHOD relationships
	hasMethodCount := 0
	for _, rel := range relationships {
		if rel.Type == models.HasMethod {
			hasMethodCount++
		}
	}
	assert.Greater(t, hasMethodCount, 0, "Should have HAS_METHOD relationships")
}

func TestPythonParser_ParseFile_Variables(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "variables.py")

	content := `# Global variables and constants
config = {"key": "value"}
debug_mode = True
name = "test"

# Constants (uppercase)
MAX_SIZE = 1000
DEBUG_MODE = True
PI = 3.14159

# Type annotated variables
typed_name: str = "test"
count: int = 42`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	elements, _, err := parser.ParseFile(ctx, testFile)
	require.NoError(t, err)

	// Count variables and constants
	variableCount := 0
	constantCount := 0

	for _, element := range elements {
		if element.Type == models.Variable {
			variableCount++
		} else if element.Type == models.Constant {
			constantCount++
		}
	}

	assert.Greater(t, variableCount, 0, "Should parse variables")
	assert.Greater(t, constantCount, 0, "Should parse constants")

	totalVars := variableCount + constantCount
	assert.GreaterOrEqual(t, totalVars, 8, "Should parse at least 8 variables/constants")
}

func TestPythonParser_ParseFile_Integration(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "integration.py")

	content := `"""Test module for Python parser."""
import os
from typing import List, Dict, Optional

# Global constants and variables
MAX_SIZE = 100
config = {"test": True}
debug_mode: bool = False

class TestClass:
    """Test class docstring."""

    class_var = "shared"
    _private_var = "hidden"

    def __init__(self, name: str):
        """Initialize test class."""
        self.name = name

    def process_data(self, data: List[str]) -> Dict[str, int]:
        """Process input data."""
        result = {}
        for item in data:
            result[item] = len(item)
        return result

    def _private_method(self):
        """Private method."""
        return self._private_var

    @staticmethod
    def static_method() -> str:
        """Static method."""
        return "static"

class ChildClass(TestClass):
    """Child class inheriting from TestClass."""

    def __init__(self, name: str, value: int):
        super().__init__(name)
        self.value = value

    def get_value(self) -> int:
        """Get the value."""
        return self.value

def global_function(x: int, y: Optional[str] = None) -> str:
    """Global function example."""
    processor = TestClass("test")
    result = processor.process_data(["hello", "world"])
    return str(x) if y is None else f"{x}_{y}"

def another_function():
    """Another function without type hints."""
    value = global_function(42, "test")
    return value.upper()

# More variables
result_cache = {}
TIMEOUT = 30`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	elements, relationships, err := parser.ParseFile(ctx, testFile)
	require.NoError(t, err)

	// Verify we got expected elements
	assert.NotEmpty(t, elements)
	assert.NotEmpty(t, relationships)

	// Count different types
	counts := make(map[any]int)
	for _, element := range elements {
		counts[element.Type]++
	}

	assert.Greater(t, counts[models.Import], 0, "Should have imports")
	assert.Greater(t, counts[models.Function], 0, "Should have functions")
	assert.Greater(t, counts[models.Struct], 0, "Should have classes")
	assert.Greater(t, counts[models.Method], 0, "Should have methods")
	assert.Greater(t, counts[models.Variable], 0, "Should have variables")
	assert.Greater(t, counts[models.Constant], 0, "Should have constants")

	// Verify specific counts
	assert.GreaterOrEqual(t, counts[models.Import], 3, "Should have at least 3 imports")
	assert.GreaterOrEqual(t, counts[models.Function], 2, "Should have at least 2 functions")
	assert.GreaterOrEqual(t, counts[models.Struct], 2, "Should have at least 2 classes")
	assert.GreaterOrEqual(t, counts[models.Method], 4, "Should have at least 4 methods")

	// Verify relationships exist
	relationshipTypes := make(map[models.RelationType]int)
	for _, rel := range relationships {
		relationshipTypes[rel.Type]++
	}

	assert.Greater(t, relationshipTypes[models.HasMethod], 0, "Should have HAS_METHOD relationships")
	assert.Greater(t, relationshipTypes[models.Calls], 0, "Should have CALLS relationships")

	// Check that elements have proper metadata
	for _, element := range elements {
		assert.NotEmpty(t, element.ID, "Element should have ID")
		assert.NotEmpty(t, element.Name, "Element should have name")
		assert.Greater(t, element.StartLine, 0, "Element should have valid start line")
		assert.GreaterOrEqual(t, element.EndLine, element.StartLine, "End line should be >= start line")

		if element.Type == models.Method || element.Type == models.Function {
			// Functions and methods should have signatures
			assert.NotEmpty(t, element.Signature, "Functions/methods should have signatures")
		}
	}

	// Check relationships have proper structure
	for _, rel := range relationships {
		assert.NotEmpty(t, rel.ID, "Relationship should have ID")
		assert.NotEmpty(t, rel.FromID, "Relationship should have FromID")

		if rel.Type == models.HasMethod {
			assert.NotEmpty(t, rel.ToID, "HAS_METHOD relationships should have ToID")
		}
	}
}

func TestPythonParser_ParseFile_ErrorCases(t *testing.T) {
	parser := ast.NewPythonParser()
	ctx := context.Background()

	t.Run("file not found", func(t *testing.T) {
		_, _, err := parser.ParseFile(ctx, "nonexistent.py")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read file")
	})

	t.Run("empty file", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "empty.py")

		err := os.WriteFile(testFile, []byte(""), 0644)
		require.NoError(t, err)

		elements, relationships, err := parser.ParseFile(ctx, testFile)
		require.NoError(t, err)

		assert.Empty(t, elements)
		assert.Empty(t, relationships)
	})

	t.Run("malformed python", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "malformed.py")

		// Python with syntax issues that the parser should handle gracefully
		content := `# Malformed Python that might confuse regex parsing
def incomplete_function(
def another_func():
    pass

class InvalidClass
    pass

def valid_function():
    return "this should still work"`

		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		elements, _, err := parser.ParseFile(ctx, testFile)
		require.NoError(t, err)

		// Should still parse some valid elements
		functionCount := 0
		for _, element := range elements {
			if element.Type == models.Function {
				functionCount++
			}
		}

		// Should find at least the valid functions
		assert.GreaterOrEqual(t, functionCount, 1, "Should parse at least one valid function")
	})
}

func TestPythonParser_ParseFile_EdgeCases(t *testing.T) {
	parser := ast.NewPythonParser()
	ctx := context.Background()

	t.Run("decorators and async", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "decorators.py")

		content := `@decorator
def decorated_function():
    pass

@staticmethod
def static_method():
    pass

async def async_function():
    await something()

@property
def property_method(self):
    return self._value

@classmethod
def class_method(cls):
    return cls()`

		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		elements, _, err := parser.ParseFile(ctx, testFile)
		require.NoError(t, err)

		functionCount := 0
		functionNames := []string{}
		for _, element := range elements {
			if element.Type == models.Function {
				functionCount++
				functionNames = append(functionNames, element.Name)
			}
		}

		assert.GreaterOrEqual(t, functionCount, 4, "Should parse decorated and async functions")
		assert.Contains(t, functionNames, "decorated_function")
		assert.Contains(t, functionNames, "async_function")
	})

	t.Run("nested structures", func(t *testing.T) {
		tempDir := t.TempDir()
		testFile := filepath.Join(tempDir, "nested.py")

		content := `class OuterClass:
    def outer_method(self):
        class InnerClass:
            def inner_method(self):
                def nested_function():
                    return "nested"
                return nested_function()
        return InnerClass()

def outer_function():
    def inner_function():
        return "inner"
    return inner_function()`

		err := os.WriteFile(testFile, []byte(content), 0644)
		require.NoError(t, err)

		elements, _, err := parser.ParseFile(ctx, testFile)
		require.NoError(t, err)

		classCount := 0
		functionCount := 0
		methodCount := 0

		for _, element := range elements {
			switch element.Type {
			case models.Struct:
				classCount++
			case models.Function:
				functionCount++
			case models.Method:
				methodCount++
			}
		}

		// Should parse nested structures
		assert.Greater(t, classCount, 0, "Should parse classes")
		assert.Greater(t, functionCount, 0, "Should parse functions")
		assert.Greater(t, methodCount, 0, "Should parse methods")
	})
}

// Benchmark tests using public interface
func BenchmarkPythonParser_ParseFile_Small(b *testing.B) {
	tempDir := b.TempDir()
	testFile := filepath.Join(tempDir, "benchmark_small.py")

	content := `import os
from typing import List

def simple_function(x: int) -> str:
    return str(x)

class SimpleClass:
    def method(self):
        return "result"`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(b, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := parser.ParseFile(ctx, testFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPythonParser_ParseFile_Medium(b *testing.B) {
	tempDir := b.TempDir()
	testFile := filepath.Join(tempDir, "benchmark_medium.py")

	// Generate medium-sized Python file
	content := `"""Medium-sized Python file for benchmarking."""
import os
import sys
from typing import List, Dict, Optional
from collections import defaultdict, Counter

# Constants
MAX_SIZE = 1000
DEBUG_MODE = True

class BaseClass:
    """Base class docstring."""

    def __init__(self, name: str):
        self.name = name

    def process(self, data: List[str]) -> Dict[str, int]:
        result = {}
        for item in data:
            result[item] = len(item)
        return result

class DerivedClass(BaseClass):
    def __init__(self, name: str, value: int):
        super().__init__(name)
        self.value = value

    def get_value(self) -> int:
        return self.value

def global_func1(x: int) -> str:
    return str(x)

def global_func2(data: List[str]) -> int:
    return len(data)

def global_func3() -> Dict[str, str]:
    return {"key": "value"}`

	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(b, err)

	parser := ast.NewPythonParser()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := parser.ParseFile(ctx, testFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}
