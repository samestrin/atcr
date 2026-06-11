# github.com/stretchr/testify

**Version:** v1.11.1 (August 27, 2025)
**Registry:** [pkg.go.dev/github.com/stretchr/testify](https://pkg.go.dev/github.com/stretchr/testify)
**Official Docs:** [pkg.go.dev/github.com/stretchr/testify](https://pkg.go.dev/github.com/stretchr/testify)
**Tier:** Important
**Last Updated:** 2026-06-10

---

## Overview

Testify is a Go package providing tools for testifying that your code behaves as intended. It offers easy assertions, mocking, and testing suite interfaces. The package is widely used in the Go ecosystem for writing clear, expressive tests with helpful failure messages.

## Quick Start

### Installation

```bash
go get github.com/stretchr/testify
```

To update to the latest version:
```bash
go get -u github.com/stretchr/testify
```

### Basic Usage

Testify provides four main sub-packages:

- **assert**: Non-fatal assertions that return boolean values
- **require**: Fatal assertions that terminate tests immediately on failure
- **mock**: Mock object framework for setting expectations and verifying interactions
- **suite**: Test suite struct with setup/teardown lifecycle hooks

## Key APIs

### assert Package

Non-fatal assertions that return `bool` and print friendly failure descriptions.

**Key Functions:**
- `Equal(t TestingT, expected, actual interface{}, msgAndArgs ...interface{}) bool`
- `NotEqual(t TestingT, expected, actual interface{}, msgAndArgs ...interface{}) bool`
- `Nil(t TestingT, object interface{}, msgAndArgs ...interface{}) bool`
- `NotNil(t TestingT, object interface{}, msgAndArgs ...interface{}) bool`
- `True(t TestingT, value bool, msgAndArgs ...interface{}) bool`
- `NoError(t TestingT, err error, msgAndArgs ...interface{}) bool`

**Usage Example:**

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestSomething(t *testing.T) {
    // Option 1: Direct function calls
    assert.Equal(t, 123, 123, "they should be equal")
    assert.Nil(t, someObject)

    // Option 2: Create an assert object (recommended for multiple assertions)
    assert := assert.New(t)
    
    assert.Equal(123, 123, "they should be equal")
    assert.NotEqual(123, 456, "they should not be equal")
    
    if assert.NotNil(someObject) {
        // Safe to use someObject here since it's not nil
        assert.Equal("ExpectedValue", someObject.Value)
    }
}
```

Full API documentation: [assert package](https://pkg.go.dev/github.com/stretchr/testify/assert)

### require Package

Same functions as `assert`, but terminates the test immediately on failure by calling `t.FailNow()`. Use for critical preconditions where subsequent test logic depends on the assertion passing.

**Important:** Must be called from the goroutine running the test function to avoid race conditions.

**Usage Example:**

```go
import (
    "testing"
    "github.com/stretchr/testify/require"
)

func TestSomethingCritical(t *testing.T) {
    require := require.New(t)
    
    // If this fails, the test stops immediately
    require.NotNil(object, "object must exist to continue")
    require.Equal("value", object.Field) 
}
```

Full API documentation: [require package](https://pkg.go.dev/github.com/stretchr/testify/require)

### mock Package

Create mock objects that implement interfaces, set expectations, and verify interactions.

**Key Methods:**
- `On(methodName string, args ...interface{}) *Call` - Set expectation for a method call
- `Return(values ...interface{}) *Call` - Specify return values for the expectation
- `AssertExpectations(t TestingT) bool` - Verify all expected calls were made
- `Called(args ...interface{}) Arguments` - Call from within mocked method implementation

**Usage Example:**

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/mock"
)

// 1. Define the mocked object
type MyMockedObject struct {
    mock.Mock
}

// 2. Implement the method being mocked
func (m *MyMockedObject) DoSomething(number int) (bool, error) {
    args := m.Called(number)
    return args.Bool(0), args.Error(1)
}

// 3. Write the test
func TestSomething(t *testing.T) {
    testObj := new(MyMockedObject)

    // Set expectations
    testObj.On("DoSomething", 123).Return(true, nil)
    
    // Use placeholder for dynamic/unpredictable arguments
    // testObj.On("DoSomething", mock.Anything).Return(true, nil)

    // Call the code under test
    targetFuncThatDoesSomethingWithObj(testObj)

    // Verify expectations were met
    testObj.AssertExpectations(t)
}
```

Full API documentation: [mock package](https://pkg.go.dev/github.com/stretchr/testify/mock)

### suite Package

Structure tests using structs with setup/teardown lifecycle hooks. Provides object-oriented test organization.

**Key Features:**
- Embed `suite.Suite` in your struct
- Implement `SetupTest()` and `TearDownTest()` for per-test lifecycle hooks
- Methods starting with `Test` are executed as tests
- Access assertion methods directly via `suite.Assert()` or `suite.Equal()`, etc.

**Important:** The suite package does not support parallel tests.

**Usage Example:**

```go
package yours

import (
    "testing"
    "github.com/stretchr/testify/suite"
)

// Define the suite struct
type ExampleTestSuite struct {
    suite.Suite
    VariableThatShouldStartAtFive int
}

// SetupTest runs before each test
func (suite *ExampleTestSuite) SetupTest() {
    suite.VariableThatShouldStartAtFive = 5
}

// A test method
func (suite *ExampleTestSuite) TestExample() {
    // Use built-in assertion methods
    suite.Equal(5, suite.VariableThatShouldStartAtFive)
}

// Required: Run the suite via a normal test function
func TestExampleTestSuite(t *testing.T) {
    suite.Run(t, new(ExampleTestSuite))
}
```

Full API documentation: [suite package](https://pkg.go.dev/github.com/stretchr/testify/suite)

---

## Integration Notes (atcr)

- Use `assert` for most test assertions in atcr unit tests
- Use `require` for critical preconditions (e.g., file existence, successful initialization) where test logic cannot continue on failure
- Use `mock` for LLM provider mocks in fan-out engine tests (per the base system's test pattern)
- Use `httptest.NewServer` with mock handlers to simulate OpenAI-compatible providers
- Table-driven tests with `t.Run` subtests benefit from assert's clear failure messages
- The suite package is optional; atcr tests can use standard `testing.T` with assert/require

---
**Source:** Extracted from official sources on 2026-06-10.
