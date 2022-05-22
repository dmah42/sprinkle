# Swarm
Run jobs on distributed machines easily. No master negotiation or consensus in
sight: All parts take an `addr` command line argument that refers to a UDP
multicast address on which workers can be discovered.

## Commands
### Worker
 Responsible for doing work. Exposes a gRPC service definition defined in
api/proto/swarm.proto.

### Swarm
User-facing command line for running work on a swarm.

### UI
Simple HTTP server to monitor the swarm.

## Example command lines
Start the UI
```
$ ./bin/ui --logtostderr --addr="225.0.0.1:9999"
```

Start a worker
```
$ ./bin/worker --logtostderr --addr="225.0.0.1:9999"
```

Run a command
```
$ ./bin/swarm --cmd="echo hello" --logtostderr --addr=225.0.0.1:9999
...
I0103 10:13:41.607985   14308 swarm.go:224] hello
```

## TODO
* take a reference to a command and use groupcache
* TTL on discovery requests.
* unit tests
