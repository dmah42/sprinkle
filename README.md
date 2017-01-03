# Flock
Run jobs on distributed machines easily. No master negotiation or consensus in
sight: All parts take an `addr` command line argument that refers to a UDP
multicast address on which workers can be discovered.

## Modules
### Sheep
The worker. Responsible for doing work. Exposes a gRPC service definition
defined in `proto/sheep.proto`.

### Shepherd
User-facing command line for running work on sheep.

### UI
Simple HTTP server to monitor the flock.

## Example command line
Start a worker
```
$ ./bin/sheep --logtostderr --addr="225.0.0.1:9999"
```

Run a command
```
$ ./bin/shepherd --cmd="echo hello" --logtostderr --addr=225.0.0.1:9999
...
I0103 10:13:41.607985   14308 shepherd.go:224] hello
```

## TODO
* TTL on discovery requests.
* code cleanup to move discovery into a common module
* bazel build
* unit tests
