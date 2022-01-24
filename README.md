# kube-relay

Small CLI tool to proxy tcp connections from local to resources in k8s network (e.g. databases or resource in other namespaces that you cannot access directly or via `kubectl port-forward`). It will spawn a pod with `socat` to use as relay and connect to that pod via kubernetes' port-forwarding facility.

```
                   ┌───────────────────────────────┐
                   │                               │
                   │  kubernetes cluster network   │
                   │                               │
   ┌───────────┐   │  ┌─────────┐     ┌─────────┐  │
   │           │   │  │         │     │         │  │
   │           │   │  │ relay   │     │ cluster │  │
   │ localhost ├───┼─►│         ├────►│         │  │
   │           │   │  │ pod     │     │ service │  │
   │           │   │  │         │     │         │  │
   └─────┬─────┘   │  └─────────┘     └─────────┘  │
                   │                        ▲      │
         │         └────────────────────────┼──────┘
                                             
         └ ── ── ── ── ── ── ── ── ── ── ── ┘
```

## Build

Tested with Go v1.17 on Linux and MacOS.

```bash
go mod tidy
go build
```

## Run

```bash
./kube-relay -ch some-service.my-namespace
Created pod "kube-relay"
Pod "kube-relay" is running
Forwarding from 127.0.0.1:1999 -> 9000
Forwarding from [::1]:1999 -> 9000
```

Test in another shell

```bash
curl localhost:1999/health
{"status":"OK"}
```
