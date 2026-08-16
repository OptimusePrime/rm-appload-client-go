# AppLoad Backend Client for Go

[AppLoad](https://github.com/asivery/rm-appload/) is a Xovi extension for reMarkable's (rM) native UI xochitl, which allows installing additional apps on rM, it supports all rM models (except perhaps rM 1).
Native apps for AppLoad are written such that the UI is written in QML and the backend can be written in any language and communicate with the UI via AppLoad's protocol using Unix sockets.

AppLoad provides an example client compliant with its protocol in Rust. This is my backend client for Go.
It mostly follows the same structure as the Rust client, but I made a few changes to make it more Go-idiomatic.

# Quick Start

First add the library to your project.

```sh
$ go get github.com/OptimusePrime/rm-appload-client-go
```

Then copy and paste this code:

```go
package main

import (
	"fmt"
	
	appload "github.com/OptimusePrime/rm-appload-client-go"
)

const HelloUIMessageType = 10
const HelloBackendMessageType = 11

func main() {
	// first, create a backend (there will always be one backend per app)
	backend := appload.NewAppLoadBackend()
	
	// here, you register all message handlers for your backend
	// that is, handlers for all message types your UI may send
	// message type numbers (the first argument) are arbitrary numbers to identify messages on UI and backend
	// content of the message must be a string (this includes JSON and other text-based formats)
	backend.RegisterMessageHandler(HelloUIMessageType, func (contents string, sender MessageSender) {
		fmt.Printf("UI says: %s\n", contents)
		sender.SendMessage(HelloBackendMessageType, []byte("Hi there!"))
    })
	
	// run the backend, possibly with additional options (see below)
	backend.Run()
}
```

# Additional Run Options 

## `WithCleanup` option

This allows running code (a cleanup function) immediately before the backend exits.

```go
package main

import (
	"os"

	appload "github.com/OptimusePrime/rm-appload-client-go"
)

func main() {
	backend := appload.NewAppLoadBackend()

	// ...
	// register message handlers here

	backend.Run(appload.WithCleanup(func() {
		// cleanup code
		// e.g. delete some temp files
		os.Remove("/tmp/user-log.txt")
	}))
}
```

## `WithSetup` option

Setup makes it possible to run some initial setup code. It executes after a connection with the Unix socket has been established, but before it starts listening for messages.

```go
package main

import (
	"encoding/json"

	appload "github.com/OptimusePrime/rm-appload-client-go"
)

const GreetingMessageType = 1

type GreetingMessage struct {
	content string
	font    string
}

func main() {
	backend := appload.NewAppLoadBackend()

	// ...
	// register message handlers here

	// for example, you could send an initial message to the UI, e.g. a greeting message for the user
	backend.Run(appload.WithSetup(func(backend *appload.AppLoadBackend, sender appload.MessageSender) error {
		greet := GreetingMessage{
			content: "Welcome, dear {{USER}}!",
			font:    "Inter",
		}

		data, _ := json.Marshal(greet)

		return sender.SendMessage(GreetingMessageType, data)
	}))
}
```

---

For both of these, be aware that, by default, AppLoad does not shut down the backend of the app after the user exits it and leaves the backend operating in the background.
This can be changed (i.e. force the backend to shut down each time the user exits the app) using this QML code (see the [AppLoad GitHub page](https://github.com/asivery/rm-appload/#applications-format) for more details).
```qmllang
signal close
function unloading() {
    ...
}
```

# The Protocol

When AppLoad is starting an app, it starts the app's UI in QML using its QML engine, and then executes the backend `entry` executable.
The first and only argument provided to the executable is a `SOCK_SEQPACKET` Unix socket (path) for communication. 

AppLoad then expects the backend to immediately connect to the socket. After which, messages can be exchanged.

## Message exchange

Both the backend and the UI can send and receive messages, the process is the same.

1. The sender first sends a message header: a contiguous array of 8 bytes.
The first four bytes is a 32-bit unsigned integer representing a message type - this is an arbitrary number set by the developer allowing handling of the message based on its type. The latter half of the array is another 32-bit unsigned integer representing the length of the message about to be sent, up to a maximum `10485760` bytes (10 MiB).
2. The receiver is constantly listening for new message headers. Once a header is received, the receiver prepares to receive the message of the specified length.
3. The sender now sends the message of the length specified in the header. This must be a string because AppLoad on the UI side will always convert the received data into a string.
This constraint usually will not represent an issue, since it still allows an exchange of data via JSON, XML, etc. In all cases dealing with data sent or received, this library uses `[]byte` instead of `string` to allow easy use with various (e.g., JSON) `Marshal` and `Unmarshal` functions.
4. The receiver reads the data and handles the message.

