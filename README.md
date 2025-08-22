[![PkgGoDev](https://pkg.go.dev/badge/github.com/krhubert/backoff)](https://pkg.go.dev/github.com/krhubert/backoff)

# Backoff

This package provides a retry mechanism for functions.

The API is very minimalistic, yet gives options for customization.

## Usage

```go
package main

import (
    "fmt"
    "time"

    "github.com/krhubert/backoff"
)

func fn() (string, error) {
    return "out", nil
}

func main() {
    out, err := backoff.Retry2(context.Background(), fn)
    if err != nil {
        fmt.Fprint(os.Stderr, "Failed", err)
        os.Exit(1)
    }
    fmt.Println(out)
}
```
