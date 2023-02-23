# Smokeping Slave in Go

## Why?

Just don't bother dealing with perl packages, `fping`, `tcpping` or whatever
dependencies on weird environments.

Now, with single binary you are able to set up a smokeping slave node on whatever
devices Go supports.

## Scope

This implementation only supports `FPing` anf `TCPing` probes. Should be enough
for most.

## How to compile

```
go build cmd/smokeping
```

Alternatively, `release/build.py` compiles for multiple platforms.

## How to use

Simply run with whatever method you prefer. See `smokeping --help` for flags.
