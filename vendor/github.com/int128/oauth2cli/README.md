# oauth2cli ![test](https://github.com/int128/oauth2cli/workflows/test/badge.svg) [![GoDoc](https://godoc.org/github.com/int128/oauth2cli?status.svg)](https://godoc.org/github.com/int128/oauth2cli)

This is a Go package for OAuth 2.0 authorization in a command line interface (CLI) tool.
You can create a CLI tool with the simple authorization flow for better UX.

Take a look at the screencast of [the example application](example/).

<img src="https://user-images.githubusercontent.com/321266/87224372-c2a53c00-c3bf-11ea-8419-74380a9e681e.gif" width="572" height="391">


## Purpose

When we create a CLI tool which accesses an API with OAuth, it needs the complicated flow such as copy/paste of a URL and code, as follows:

1. User runs the command.
1. Command shows the URL for authorization.
1. User opens the browser, logs in to the server and approves the authorization.
1. Server shows an authorization code.
1. User copies the code and pastes into the command.
1. Command accesses the API with the token.

You can make it simple by using oauth2cli as follows:

1. User runs the command.
1. Command opens the browser.
1. User logs in to the server and approves the authorization.
1. Command gets a token and access the API with the token.


## How it works

oauth2cli starts the local server and initiates the flow of [OAuth 2.0 Authorization Code Grant](https://tools.ietf.org/html/rfc6749#section-4.1).

Take a look at the sequence diagram:

![diagram](docs/diagram.svg)


## Contributions

This is an open source software licensed under Apache 2.0.
Feel free to open issues and pull requests.
