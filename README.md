[![PkgGoDev](https://pkg.go.dev/badge/github.com/krhubert/backoff)](https://pkg.go.dev/github.com/krhubert/backoff)

# Backoff policy

This package provides a backoff policy implementation for functions that need to be retried.

## Usage

```go
package main

import (
    "fmt"
    "time"

    "github.com/krhubert/backoff"
)

func main() {
    // Create a new contant interval backoff policy with
    // a maximum of 5 retries
    bo := backoff.WithMaxRetries(
        backoff.NewIntervalBackoff(time.Second),
        5,
    )

    // Retry a function that returns an error
    if err := backoff.Retry(bo, func() error {
        return nil
    }); err != nil {
        fmt.Println("Failed after 5 retries")
    }

    // Create a new exponential backoff policy with 
    // a maximum interval of 1 seconds
    bo = backoff.WithMaxInterval(
        backoff.NewExponentialBackoff(),
        time.Second,
    )

    backoff.Retry(bo, func() error {
        return nil
    })
}
```
