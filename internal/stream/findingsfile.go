package stream

import "os"

// readFindingsFile reads a selected findings file. A var so tests can act
// between selection and the read.
var readFindingsFile = os.ReadFile
