# listener ![build](https://github.com/int128/listener/workflows/build/badge.svg) [![GoDoc](https://godoc.org/github.com/int128/listener?status.svg)](https://godoc.org/github.com/int128/listener)

This is a Go package to allocate a net.Listener from address candidates.

```sh
go get github.com/int128/listener
```

## Examples

### Allocate a free port

To allocate a `net.Listener` at a free port on localhost:

```go
package main

import (
	"fmt"

	"github.com/int128/listener"
)

func main() {
	l, err := listener.New(nil)
	if err != nil {
		panic(err)
	}
	defer l.Close()

	fmt.Printf("Open %s", l.URL)
}
```

### Allocate a port from candidates

To allocate a `net.Listener` at a port 18000 or 28000 on localhost:

```go
package main

import (
	"fmt"

	"github.com/int128/listener"
)

func main() {
	l, err := listener.New([]string{"127.0.0.1:18000", "127.0.0.1:28000"})
	if err != nil {
		panic(err)
	}
	defer l.Close()

	fmt.Printf("Open %s", l.URL)
}
```

If port 18000 is already in use, it will allocate port 28000.


## Contributions

This is an open source software.
Free free to open issues and pull requests.
