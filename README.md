# Sprinkle
Run jobs on distributed machines easily. No master negotiation or consensus in
sight: All parts take an `addr` command line argument that refers to a UDP
multicast address on which workers can be discovered.

## Commands
### Worker
 Responsible for doing work. Exposes a gRPC service definition defined in
api/proto/sprinkle.proto.

### Run
User-facing command line for running work on the most appropriate worker.

### UI
Simple HTTP server to monitor the workers and running jobs.

## Example command lines
Start the UI
```
$ ./bin/ui --logtostderr"
```

Start a worker
```
$ ./bin/worker --logtostderr"
```

Run a command
```
$ ./bin/run --cmd="sleep 10 && echo hello" --logtostderr 
...
hello
```

## TODO
* take a reference to a command and use groupcache
* TTL on discovery requests.
* unit tests
