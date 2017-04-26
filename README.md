# sync/netmutex - Go client to lock server

Low-level high-performance Golang client library for [Taooka lock server](https://taooka.org/) .

[![GoDoc](https://godoc.org/github.com/MichaelMonashev/sync/netmutex?status.svg)](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)
[![Travis CI](https://travis-ci.org/MichaelMonashev/sync.svg)](https://travis-ci.org/MichaelMonashev/sync)
[![AppVeyor](https://ci.appveyor.com/api/projects/status/eit2o9qvcocqyhqd?svg=true)](https://ci.appveyor.com/project/MichaelMonashev/sync)
[![Codeship](https://codeship.com/projects/a1e6d740-389e-0134-23ba-1e19d127eddf/status?branch=master)](https://codeship.com/projects/165999)
[![Coverage](https://coveralls.io/repos/github/MichaelMonashev/sync/badge.svg?branch=master)](https://coveralls.io/github/MichaelMonashev/sync?branch=master)
[![Codecov](https://codecov.io/gh/MichaelMonashev/sync/branch/master/graph/badge.svg)](https://codecov.io/gh/MichaelMonashev/sync)

## Installation

### Using *go get*

```
$ go get github.com/MichaelMonashev/sync/netmutex
```

Its source will be in:

```
$GOPATH/src/github.com/MichaelMonashev/sync/netmutex
```

## Documentation

See: [godoc.org/github.com/MichaelMonashev/sync/netmutex](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)

or run:

```
$ godoc github.com/MichaelMonashev/sync/netmutex
```

## Performance

200000+ locks per second on 8-core Linux box.

## Example

Steps:

 - connect to lock server
 - lock key
 - execute critical section
 - unlock key
 - close connection

See full example in [sync/netmutex godoc](https://godoc.org/github.com/MichaelMonashev/sync/netmutex#ex-package)

